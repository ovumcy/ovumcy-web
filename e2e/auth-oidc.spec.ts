import { expect, test, type Page } from '@playwright/test';
import {
  DEFAULT_STRONG_PASSWORD,
  completeOnboardingIfPresent,
  confirmRecoveryCode,
  continueFromRecoveryCode,
  expectDedicatedRecoveryPage,
  expectInlineRegisterRecoveryStep,
  expectNoSensitiveAuthParams,
  loginViaUI,
  registerOwnerViaUI,
  readRecoveryCode,
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
    await expect(page.locator('[data-settings-recovery-code-unavailable]')).toBeVisible();
    await expect(page.locator('form[action="/api/settings/regenerate-recovery-code"]')).toHaveCount(0);

    const localPassword = 'LocalStrongPass2';
    await page.locator('#settings-new-password').fill(localPassword);
    await page.locator('#settings-confirm-password').fill(localPassword);
    await page.locator('[data-settings-local-password-form] button[type="submit"]').click();

    await expectDedicatedRecoveryPage(page);
    await readRecoveryCode(page);
    await confirmRecoveryCode(page);

    await expect(page).toHaveURL(/\/settings(?:\?.*)?$/);
  }

  await expect(page.locator('[data-settings-local-password-form]')).toHaveCount(0);
  await expect(page.locator('form[action="/api/settings/regenerate-recovery-code"]')).toHaveCount(1);
  await expect(page.locator('form[action="/api/settings/clear-data"]')).toHaveCount(1);
  await expect(page.locator('form[hx-delete="/api/settings/delete-account"]')).toHaveCount(1);
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

  test('hybrid mode keeps local auth visible and signs in via verified email match', async ({
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
      await expect(page).toHaveURL(/\/register$/);
      await expect(page.locator('#register-client-status .status-error, [data-auth-server-error]')).toBeVisible();
      await loginViaUI(page, credentials);
      await completeOnboardingIfPresent(page);
      await expect(page).toHaveURL(/\/dashboard(?:\?.*)?$/);
    }

    await page.locator('.nav-logout-form button[type="submit"]').click();
    await expect(page.locator('#confirm-modal')).toBeVisible();
    await page.locator('#confirm-modal-accept').click();
    await expect(page).toHaveURL(/\/login(?:\?.*)?$/);
    expectNoSensitiveAuthParams(page.url());

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
