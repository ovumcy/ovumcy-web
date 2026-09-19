import { expect, test, type Page } from './support/fixtures';
import {
  DEFAULT_STRONG_PASSWORD,
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  expectInlineRegisterRecoveryStep,
  loginViaUI,
  registerOwnerViaUI,
} from './support/auth-helpers';
import { localeText } from './support/locale-helpers';

// The cross-site lane. Everything here is identical to the same-site OIDC lane
// except WHERE the mock IdP listens: E2E_OIDC_HOST puts it on a different
// loopback host, which the browser treats as a different site, so the
// form_post callback POST arrives cross-site and the OIDC transit cookies
// (ovumcy_oidc_auth, ovumcy_oidc_stepup) are exercised against their real
// SameSite constraint instead of a same-host, different-port one. A step-up
// that silently depends on same-site cookie delivery can only redden here.
const oidcEnabled = process.env.OIDC_ENABLED === 'true';
const localOIDCProvider = process.env.E2E_OIDC_PROVIDER === 'local';
const crossSiteLane = process.env.E2E_OIDC_CROSS_SITE === 'true';
const loginMode = process.env.OIDC_LOGIN_MODE ?? 'hybrid';
const autoProvisionEnabled = process.env.OIDC_AUTO_PROVISION === 'true';
const providerEmail = process.env.OIDC_TEST_PROVIDER_EMAIL ?? 'oidc-browser@example.com';
const providerIssuer = process.env.OIDC_ISSUER_URL ?? '';
const appBaseURL = process.env.PLAYWRIGHT_BASE_URL ?? '';

// Guard against the lane passing for the wrong reason. If the harness ever
// reverts to serving the IdP from the app's own host, every assertion below
// still passes — while proving nothing about cross-site delivery. Compare the
// hosts the two URLs actually carry rather than trusting the env flag alone.
function expectIssuerIsCrossSite(): void {
  expect(providerIssuer, 'OIDC_ISSUER_URL must be set in the cross-site lane').not.toBe('');
  expect(appBaseURL, 'PLAYWRIGHT_BASE_URL must be set in the cross-site lane').not.toBe('');
  expect(
    new URL(providerIssuer).hostname,
    'the cross-site lane requires the IdP on a different host than the app',
  ).not.toBe(new URL(appBaseURL).hostname);
}

async function signInViaSSO(page: Page): Promise<void> {
  await page.goto('/login');
  await expect(page.locator('[data-auth-sso-cta]')).toBeVisible();
  await page.locator('[data-auth-sso-cta]').click();
  await completeOnboardingIfPresent(page);
  await expect(page).toHaveURL(/\/dashboard(?:\?.*)?$/);
}

async function acceptConfirmModal(page: Page): Promise<void> {
  await expect(page.locator('#confirm-modal')).toBeVisible();
  await page.locator('#confirm-modal-accept').click();
}

test.describe('Auth: OIDC cross-site callback', () => {
  test.use({ ignoreHTTPSErrors: true });
  test.skip(!oidcEnabled || !localOIDCProvider || !crossSiteLane, 'Requires the cross-site local OIDC lane');

  test('hybrid: identity link step-up completes over a cross-site form_post callback', async ({
    page,
  }) => {
    test.skip(loginMode !== 'hybrid', 'Requires hybrid login mode');
    expectIssuerIsCrossSite();

    const credentials = { email: providerEmail, password: DEFAULT_STRONG_PASSWORD };

    await page.goto('/login');
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
    } else {
      // The shared e2e database already holds this email (a prior project or a
      // retry registered it). Signing in is the precondition either way.
      await loginViaUI(page, credentials);
      await completeOnboardingIfPresent(page);
    }
    await expect(page).toHaveURL(/\/dashboard(?:\?.*)?$/);

    await page.goto('/settings');
    const linkIdentityForm = page.locator('form[action="/api/v1/users/current/oidc/link/step-up"]');
    await expect(linkIdentityForm).toBeVisible();

    // The step-up leaves the app, re-authenticates at the cross-site IdP, and
    // returns as a POST from that other site. The sealed step-up cookie has to
    // survive that round trip for the callback to find a purpose at all; a
    // cookie the browser withholds lands on the generic refusal instead.
    await linkIdentityForm.locator('button[type="submit"]').click();
    await expect(
      page.locator(
        '[data-flash-key="settings.success.oidc_identity_linked"][data-flash-status="success"]',
      ),
    ).toBeVisible({ timeout: 20_000 });
    await expect(page).toHaveURL(/\/settings(?:\?.*)?$/);
  });

  test('oidc_only: clear-data, deletion and local-password step-ups complete cross-site', async ({
    page,
  }) => {
    test.skip(
      loginMode !== 'oidc_only' || !autoProvisionEnabled,
      'Requires oidc_only mode with auto-provision',
    );
    expectIssuerIsCrossSite();

    await signInViaSSO(page);
    await page.goto('/settings');

    // Clear data first: it is the erasure that leaves the account signed in, so
    // the deletion step-up below still has a session to spend. Both forms only
    // render for an account with no local password, which is exactly the
    // auto-provisioned OIDC-only owner this lane signs in as.
    const clearDataStepup = page.locator('form[action="/api/v1/users/current/data-wipe/step-up"]');
    await expect(clearDataStepup).toBeVisible();
    await clearDataStepup.locator('button[type="submit"]').click();
    await acceptConfirmModal(page);
    await expect(
      page.locator('[data-flash-status="success"]'),
    ).toBeVisible({ timeout: 20_000 });

    // Deletion next, BEFORE any local password exists: both erasure forms only
    // render for an account that has none, so enrolling a password first would
    // hide the deletion step-up behind its password-gated variant and the
    // fourth purpose would go uncovered.
    await page.goto('/settings');
    const deletionStepup = page.locator('form[action="/api/v1/users/current/deletion/step-up"]');
    await expect(deletionStepup).toBeVisible();
    await deletionStepup.locator('button[type="submit"]').click();
    await acceptConfirmModal(page);
    // A deleted account cannot stay signed in: the terminal signal is the login
    // page, reached from the cross-site callback's own completion.
    await expect(page).toHaveURL(/\/login(?:\?.*)?$/, { timeout: 20_000 });
    await expect(page.locator('[data-auth-sso-cta]')).toBeVisible();
    expect(localeText('en', 'auth.login_with_sso')).not.toBe('');

    // Local password creation: the same step-up primitive, a different purpose
    // in the sealed cookie, and the one whose payload carries a prepared hash.
    // Auto-provision mints the account again on this sign-in, so it starts with
    // no local password — exactly the state the setup form needs.
    await signInViaSSO(page);
    await page.goto('/settings');
    const localPasswordForm = page.locator('[data-settings-local-password-form]');
    await expect(localPasswordForm).toBeVisible();
    await expect(localPasswordForm).toHaveAttribute(
      'action',
      '/api/v1/users/current/password/step-up',
    );
    const localPassword = 'CrossSiteStrongPass2';
    await page.locator('#settings-new-password').fill(localPassword);
    await page.locator('#settings-confirm-password').fill(localPassword);
    await Promise.all([
      page.waitForURL(/\/recovery-code(?:\?.*)?$/, { timeout: 20_000 }),
      localPasswordForm.locator('button[type="submit"]').click(),
    ]);
  });
});
