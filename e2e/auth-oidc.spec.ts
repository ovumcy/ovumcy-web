import { expect, test, type Page } from './support/fixtures';
import {
  DEFAULT_STRONG_PASSWORD,
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  cookieByName,
  expectInlineRegisterRecoveryStep,
  expectNoSensitiveAuthParams,
  loginViaUI,
  registerOwnerViaUI,
} from './support/auth-helpers';
import { localeText } from './support/locale-helpers';

const oidcEnabled = process.env.OIDC_ENABLED === 'true';
const localOIDCProvider = process.env.E2E_OIDC_PROVIDER === 'local';
const loginMode = process.env.OIDC_LOGIN_MODE ?? 'hybrid';
const logoutMode = process.env.OIDC_LOGOUT_MODE ?? 'local';
const autoProvisionEnabled = process.env.OIDC_AUTO_PROVISION === 'true';
const providerEmail = process.env.OIDC_TEST_PROVIDER_EMAIL ?? 'oidc-browser@example.com';
const providerIssuer = process.env.OIDC_ISSUER_URL ?? '';

// `requireLocalPasswordSetup` is what separates the two callers, and it is a
// parameter rather than a plain `if` because the difference is not incidental:
// the first caller is the test OF the setup flow and must redden when the form
// is missing, while the second signs in on an account the first already enrolled
// — the form is legitimately gone by then, and its own subject is the logout
// bridge. Left as a bare `if`, the setup test passed vacuously whenever the form
// failed to render at all.
async function signInViaOIDCOnlyAndEnableLocalPassword(
  page: Page,
  { requireLocalPasswordSetup }: { requireLocalPasswordSetup: boolean },
) {
  await page.goto('/login');
  await expect(page).toHaveURL(/\/login(?:\?.*)?$/);
  await expect(page.locator('#login-form')).toHaveCount(0);
  await expect(page.locator('[data-auth-signup-cta]')).toHaveCount(0);
  await expect(page.locator('a[href="/forgot-password"]')).toHaveCount(0);
  await expect(page.locator('[data-auth-sso-cta]')).toBeVisible();

  await page.locator('[data-auth-sso-cta]').click();
  await completeOnboardingIfPresent(page);
  await expect(page).toHaveURL(/\/dashboard(?:\?.*)?$/);

  await page.goto('/settings');
  await expect(page).toHaveURL(/\/settings(?:\?.*)?$/);

  const localPasswordForm = page.locator('[data-settings-local-password-form]');
  if (requireLocalPasswordSetup) {
    await expect(localPasswordForm).toBeVisible();
  }
  if (await localPasswordForm.isVisible().catch(() => false)) {
    // OIDC-only users must complete a step-up re-auth before a local password
    // is committed. Submitting the form posts to
    // /api/v1/users/current/password/step-up, which hands back a same-origin
    // interstitial that bounces to the provider's authorize endpoint with
    // prompt=login + max_age=0. Against the controllable local IdP the round
    // trip runs to completion — provider re-auth, /auth/oidc/callback, then the
    // freshly-issued recovery code — so wait for that terminal /recovery-code
    // page as the end-to-end signal.
    await expect(page.locator('[data-settings-recovery-code-unavailable]')).toBeVisible();
    await expect(page.locator('form[action="/api/v1/users/current/recovery-code"]')).toHaveCount(0);
    await expect(localPasswordForm).toHaveAttribute('action', '/api/v1/users/current/password/step-up');

    const localPassword = 'LocalStrongPass2';
    await page.locator('#settings-new-password').fill(localPassword);
    await page.locator('#settings-confirm-password').fill(localPassword);
    // Wait for the concrete terminal URL, not `(url) => !url.startsWith(page.url())`:
    // page.url() is re-read on every predicate call and tracks the live location,
    // so it always equals the URL under test and the predicate never fires.
    await Promise.all([
      page.waitForURL(/\/recovery-code(?:\?.*)?$/),
      page.locator('[data-settings-local-password-form] button[type="submit"]').click(),
    ]);
    expect(page.url()).not.toMatch(/\/settings(?:\?.*)?$/);
  }
}

test.describe('Auth: OIDC login entry', () => {
  test.use({ ignoreHTTPSErrors: true });
  test.skip(!oidcEnabled, 'Requires OIDC_ENABLED=true');

  test('shows SSO CTA and falls back to login with safe error UX', async ({ page }) => {
    test.skip(localOIDCProvider, 'Focused on the unavailable-provider browser lane');

    await page.goto('/login');
    await expect(page).toHaveURL(/\/login(?:\?.*)?$/);

    if (loginMode === 'hybrid') {
      await expect(page.locator('#login-form')).toBeVisible();
    } else {
      await expect(page.locator('#login-form')).toHaveCount(0);
      await expect(page.locator('[data-auth-signup-cta]')).toHaveCount(0);
      await expect(page.locator('a[href="/forgot-password"]')).toHaveCount(0);
    }

    const ssoCTA = page.locator('[data-auth-sso-cta]');
    await expect(ssoCTA).toBeVisible();
    await expect(ssoCTA).toContainText(localeText('en', 'auth.login_with_sso'));

    await ssoCTA.click();

    await expect(page).toHaveURL(/\/login$/);
    expectNoSensitiveAuthParams(page.url());
    await expect(page.locator('[data-auth-server-error]')).toContainText(
      'SSO sign-in is currently unavailable.'
    );

    if (loginMode === 'hybrid') {
      await expect(page.locator('#login-form')).toBeVisible();
    } else {
      await expect(page.locator('#login-form')).toHaveCount(0);
    }
  });

  test('hybrid mode refuses a first-time SSO link at the callback and links only from the Settings step-up', async ({
    context,
    page,
  }) => {
    test.skip(!localOIDCProvider || loginMode !== 'hybrid', 'Requires local OIDC provider in hybrid mode');

    const credentials = { email: providerEmail, password: DEFAULT_STRONG_PASSWORD };

    await page.goto('/login');
    await expect(page).toHaveURL(/\/login(?:\?.*)?$/);
    await expect(page.locator('#login-form')).toBeVisible();
    await expect(page.locator('[data-auth-signup-cta]')).toBeVisible();
    await expect(page.locator('a[href="/forgot-password"]')).toBeVisible();
    await expect(page.locator('[data-auth-sso-cta]')).toBeVisible();

    // Establish a local account whose email matches the one the provider
    // asserts. The (issuer, subject) the IdP returns has never been linked, so
    // the first SSO sign-in must NOT auto-link on the asserted email — it has
    // to be refused (d1def85, then #701 closed the confirmation page too).
    await registerOwnerViaUI(page, credentials);
    const inlineRecovery = page.locator('[data-auth-inline-recovery]');
    const recoveryVisible = await expect(inlineRecovery)
      .toBeVisible({ timeout: 5_000 })
      .then(() => true)
      .catch(() => false);

    if (recoveryVisible) {
      await expectInlineRegisterRecoveryStep(page);
      await continueFromRecoveryCode(page);
      await completeOnboardingIfPresent(page);
      await expect(page).toHaveURL(/\/dashboard(?:\?.*)?$/);
    } else {
      // providerEmail already exists in the shared e2e DB (for example a prior
      // browser project registered it). The branch stays adaptive because this
      // lane is re-run against a database it does not own, but it may not be
      // entered on a timeout alone: a registration broken for any other reason
      // would silently skip the first half of the flow. Duplicate registration
      // runs the decoy pickup and lands on the neutral /login flash, so pinning
      // that landing proves the precondition this branch claims. The key is the
      // one every unusable pickup produces, so it stays enumeration-safe.
      await expect(page).toHaveURL(/\/login$/);
      await expect(
        page.locator('[data-auth-server-error][data-error-key="auth.error.post_register_signin"]')
      ).toBeVisible();
      await loginViaUI(page, credentials);
      await completeOnboardingIfPresent(page);
      await expect(page).toHaveURL(/\/dashboard(?:\?.*)?$/);
    }

    await page.locator('.nav-logout-form button[type="submit"]').click();
    await expect(page.locator('#confirm-modal')).toBeVisible();
    await page.locator('#confirm-modal-accept').click();
    await expect(page).toHaveURL(/\/login(?:\?.*)?$/);
    expectNoSensitiveAuthParams(page.url());

    // First SSO sign-in: the verified email matches the existing local account
    // but the (issuer, subject) has never been linked. The callback REFUSES —
    // linking is a permanent, password-change-weight binding and may only be
    // authorised by a factor verified now, which an unauthenticated page
    // cannot do (issue #701). So the browser lands back on /login carrying the
    // refusal flash, never on a password-confirmation page and never on the
    // dashboard.
    await page.locator('[data-auth-sso-cta]').click();
    await expect(
      page.locator(
        '[data-auth-server-error][data-error-key="auth.error.sso_link_confirmation_unavailable"]',
      ),
    ).toBeVisible();
    await expect(page.locator('[data-auth-server-error]')).toContainText(
      localeText('en', 'auth.error.sso_link_confirmation_unavailable'),
    );
    await expect(page).toHaveURL(/\/login$/);
    expectNoSensitiveAuthParams(page.url());
    await expect(page.locator('#login-form')).toBeVisible();
    // No session was issued, and the retained-but-unreachable link-confirm
    // route was not armed: without the pending-link cookie its page and its
    // POST both bounce straight back to /login.
    expect(await cookieByName(context, 'ovumcy_auth')).toBeFalsy();
    expect(await cookieByName(context, 'ovumcy_oidc_link_pending')).toBeFalsy();

    // The link is made from an authenticated session instead, so sign in with
    // the account's existing method first.
    await loginViaUI(page, credentials);
    await completeOnboardingIfPresent(page);
    await expect(page).toHaveURL(/\/dashboard(?:\?.*)?$/);

    await page.goto('/settings');
    await expect(page).toHaveURL(/\/settings(?:\?.*)?$/);

    const linkIdentityForm = page.locator(
      'form[action="/api/v1/users/current/oidc/link/step-up"]',
    );
    await expect(linkIdentityForm).toBeVisible();
    await expect(linkIdentityForm.locator('button[type="submit"]')).toContainText(
      localeText('en', 'settings.oidc_link.button'),
    );

    // Submitting the card posts to the step-up, which hands back a same-origin
    // interstitial (the settings CSP pins form-action to 'self') that bounces
    // to the provider's authorize endpoint with prompt=login + max_age, then
    // /auth/oidc/callback links the identity and 303s back to /settings. The
    // terminal signal is the success flash, not the URL: the flow starts and
    // ends on /settings, so a URL wait would be satisfied before it began.
    // Caveat on what this proves: the local IdP keeps no login session of its
    // own and ignores prompt=login, so the lane covers the round trip and the
    // persisted link — the app's freshness gate itself (reauthClaimsFresh over
    // auth_time/iat) is covered by unit tests against forged claims.
    await linkIdentityForm.locator('button[type="submit"]').click();
    await expect(
      page.locator(
        '[data-flash-key="settings.success.oidc_identity_linked"][data-flash-status="success"]',
      ),
    ).toBeVisible({ timeout: 20_000 });
    await expect(page.locator('[data-flash-status="success"]')).toContainText(
      localeText('en', 'settings.success.oidc_identity_linked'),
    );
    await expect(page).toHaveURL(/\/settings(?:\?.*)?$/);
    expectNoSensitiveAuthParams(page.url());

    // The identity is now linked: a second SSO sign-in authenticates straight
    // through without any confirmation prompt.
    await page.locator('.nav-logout-form button[type="submit"]').click();
    await expect(page.locator('#confirm-modal')).toBeVisible();
    await page.locator('#confirm-modal-accept').click();
    await expect(page).toHaveURL(/\/login(?:\?.*)?$/);

    await page.locator('[data-auth-sso-cta]').click();
    await completeOnboardingIfPresent(page);
    await expect(page).toHaveURL(/\/dashboard(?:\?.*)?$/);
    await expect(page.locator('[data-nav-account-actions]')).toBeVisible();
  });

  test('oidc_only auto-provision enables a local password', async ({ page }) => {
    test.skip(
      !localOIDCProvider || loginMode !== 'oidc_only' || !autoProvisionEnabled,
      'Requires local OIDC provider, oidc_only mode, and auto-provision',
    );

    await signInViaOIDCOnlyAndEnableLocalPassword(page, { requireLocalPasswordSetup: true });
  });

  test('oidc_only provider logout bridge works when provider logout is enabled', async ({
    page,
  }) => {
    test.skip(
      !localOIDCProvider ||
        loginMode !== 'oidc_only' ||
        !autoProvisionEnabled ||
        !['provider', 'auto'].includes(logoutMode),
      'Requires local OIDC provider, oidc_only mode, auto-provision, and provider/auto logout mode',
    );

    let providerLogoutSeen = false;
    page.on('request', (request) => {
      if (providerIssuer && request.url().startsWith(`${providerIssuer}/logout`)) {
        providerLogoutSeen = true;
      }
    });

    // The setup test above ran first and already enrolled this provider
    // account's local password, so the setup form is gone by now — this test's
    // own subject is the logout bridge, not the enrollment.
    await signInViaOIDCOnlyAndEnableLocalPassword(page, { requireLocalPasswordSetup: false });

    await page.locator('.nav-logout-form button[type="submit"]').click();
    await expect(page.locator('#confirm-modal')).toBeVisible();
    await page.locator('#confirm-modal-accept').click();
    await expect(page).toHaveURL(/\/login(?:\?.*)?$/);
    expectNoSensitiveAuthParams(page.url());
    await expect(page.locator('[data-auth-sso-cta]')).toBeVisible();
    await expect(page.locator('#login-form')).toHaveCount(0);
    await expect.poll(() => providerLogoutSeen).toBe(true);
  });
});
