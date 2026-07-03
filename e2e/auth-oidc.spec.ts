import { expect, test, type Page } from '@playwright/test';
import {
  DEFAULT_STRONG_PASSWORD,
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  expectInlineRegisterRecoveryStep,
  expectNoSensitiveAuthParams,
  loginViaUI,
  registerOwnerViaUI,
} from './support/auth-helpers';

const oidcEnabled = process.env.OIDC_ENABLED === 'true';
const localOIDCProvider = process.env.E2E_OIDC_PROVIDER === 'local';
const loginMode = process.env.OIDC_LOGIN_MODE ?? 'hybrid';
const logoutMode = process.env.OIDC_LOGOUT_MODE ?? 'local';
const autoProvisionEnabled = process.env.OIDC_AUTO_PROVISION === 'true';
const providerEmail = process.env.OIDC_TEST_PROVIDER_EMAIL ?? 'oidc-browser@example.com';
const providerIssuer = process.env.OIDC_ISSUER_URL ?? '';

async function signInViaOIDCOnlyAndEnableLocalPassword(page: Page) {
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
  if (await localPasswordForm.isVisible().catch(() => false)) {
    // OIDC-only users now must complete a step-up re-auth before a local
    // password is committed. Submitting the form posts to
    // /api/v1/users/current/password/step-up, which redirects to the
    // provider's authorize endpoint with prompt=login + max_age=0. The full
    // round-trip back through /auth/oidc/callback into /recovery-code depends
    // on the test provider honoring those parameters and cannot be exercised
    // without a controllable IdP — assert the redirect to the provider as the
    // closest end-to-end signal.
    await expect(page.locator('[data-settings-recovery-code-unavailable]')).toBeVisible();
    await expect(page.locator('form[action="/api/v1/users/current/recovery-code"]')).toHaveCount(0);
    await expect(localPasswordForm).toHaveAttribute('action', '/api/v1/users/current/password/step-up');

    const localPassword = 'LocalStrongPass2';
    await page.locator('#settings-new-password').fill(localPassword);
    await page.locator('#settings-confirm-password').fill(localPassword);
    await Promise.all([
      page.waitForURL((url) => !url.toString().startsWith(page.url())),
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
    await expect(ssoCTA).toContainText('Sign in with SSO');

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

  test('hybrid mode links SSO to a pre-existing local account only after password confirmation', async ({
    page,
  }) => {
    test.skip(!localOIDCProvider || loginMode !== 'hybrid', 'Requires local OIDC provider in hybrid mode');

    const credentials = { email: providerEmail, password: DEFAULT_STRONG_PASSWORD };
    const wrongPassword = 'WrongStrongPass9';

    await page.goto('/login');
    await expect(page).toHaveURL(/\/login(?:\?.*)?$/);
    await expect(page.locator('#login-form')).toBeVisible();
    await expect(page.locator('[data-auth-signup-cta]')).toBeVisible();
    await expect(page.locator('a[href="/forgot-password"]')).toBeVisible();
    await expect(page.locator('[data-auth-sso-cta]')).toBeVisible();

    // Establish a local account whose email matches the one the provider
    // asserts. The (issuer, subject) the IdP returns has never been linked, so
    // the first SSO sign-in must NOT auto-link — it has to fall into the
    // password-confirmation gate (d1def85).
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
      // browser project registered it). Registration is enumeration-safe, so a
      // duplicate email lands on the neutral /login, not /register — sign in
      // with the existing account instead.
      await expect(page).toHaveURL(/\/login(?:\?.*)?$/);
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
    // but the identity is unlinked → land on the password-confirmation page,
    // never straight on the dashboard.
    await page.locator('[data-auth-sso-cta]').click();
    await expect(page).toHaveURL(/\/auth\/oidc\/link-confirm$/);
    expectNoSensitiveAuthParams(page.url());
    await expect(page.locator('#oidc-link-confirm-form')).toHaveAttribute(
      'action',
      '/auth/oidc/link-confirm',
    );
    await expect(page.locator('#oidc-link-confirm-password')).toBeVisible();
    await expect(page.locator('[data-link-confirm-email]')).toContainText(providerEmail);

    // Wrong password is rejected and keeps the user on the confirmation page
    // (the pending-link cookie survives so a retry stays within TTL), with the
    // error surfaced through flash state rather than a PII-bearing URL.
    await page.locator('#oidc-link-confirm-password').fill(wrongPassword);
    await page.locator('[data-link-confirm-submit]').click();
    await expect(page).toHaveURL(/\/auth\/oidc\/link-confirm$/);
    expectNoSensitiveAuthParams(page.url());
    await expect(page.locator('[data-auth-server-error]')).toBeVisible();
    await expect(page.locator('[data-link-confirm-email]')).toContainText(providerEmail);

    // Correct password completes the link and issues a session.
    await page.locator('#oidc-link-confirm-password').fill(credentials.password);
    await page.locator('[data-link-confirm-submit]').click();
    await completeOnboardingIfPresent(page);
    await expect(page).toHaveURL(/\/dashboard(?:\?.*)?$/);
    await expect(page.locator('[data-nav-account-actions]')).toBeVisible();

    // The identity is now linked: a second SSO sign-in authenticates straight
    // through without a second confirmation prompt.
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

    await signInViaOIDCOnlyAndEnableLocalPassword(page);
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

    await signInViaOIDCOnlyAndEnableLocalPassword(page);

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
