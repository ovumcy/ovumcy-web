import { expect, test } from '@playwright/test';
import {
  completeOnboardingIfPresent,
  cookieByName,
  createCredentials,
  expectNoSensitiveAuthParams,
  loginViaUI,
  logoutViaAPI,
  pathOf,
  readRecoveryCode,
  registerOwnerViaUI,
} from './support/auth-helpers';

test.describe('Auth: register, login, logout', () => {
  test('registers valid account and lands on recovery code page without PII in URL', async ({
    page,
    context,
  }) => {
    const creds = createCredentials('auth-register');

    await registerOwnerViaUI(page, creds);

    await expect(page).toHaveURL(/\/recovery-code$/);
    expectNoSensitiveAuthParams(page.url());
    await readRecoveryCode(page);

    const authCookie = await cookieByName(context, 'ovumcy_auth');
    const recoveryCookie = await cookieByName(context, 'ovumcy_recovery_code');

    expect(authCookie).toBeTruthy();
    expect(recoveryCookie).toBeTruthy();
  });

  test('register duplicate email shows error and keeps clean redirect URL', async ({ page }) => {
    const creds = createCredentials('auth-duplicate');

    await registerOwnerViaUI(page, creds);
    await expect(page).toHaveURL(/\/recovery-code$/);

    await logoutViaAPI(page);
    await registerOwnerViaUI(page, creds);

    await expect(page).toHaveURL(/\/register$/);
    expectNoSensitiveAuthParams(page.url());
    await expect(page.locator('.status-error')).toBeVisible();
    await expect(page.locator('#register-email')).toHaveValue(creds.email);
  });

  test('register mismatch password shows form error without leaking query params', async ({ page }) => {
    const creds = createCredentials('auth-mismatch');

    await registerOwnerViaUI(page, creds, 'DifferentPass2');

    await expect(page).toHaveURL(/\/register$/);
    expectNoSensitiveAuthParams(page.url());
    await expect(page.locator('#register-client-status .status-error')).toBeVisible();
    await expect(page.locator('#register-password')).toHaveValue(creds.password);
    await expect(page.locator('#register-confirm-password')).toHaveValue('DifferentPass2');
  });

  test('register weak password shows validation error without leaking query params', async ({ page }) => {
    const creds = createCredentials('auth-weak', 'weakpass');

    await registerOwnerViaUI(page, creds);

    await expect(page).toHaveURL(/\/register$/);
    expectNoSensitiveAuthParams(page.url());
    await expect(page.locator('.status-error')).toBeVisible();
  });

  test('register form rejects invalid email via browser validation', async ({ page }) => {
    await page.goto('/register');
    await expect(page).toHaveURL(/\/register(?:\?.*)?$/);

    await page.locator('#register-email').fill('not-an-email');
    await page.locator('#register-password').fill('StrongPass1');
    await page.locator('#register-confirm-password').fill('StrongPass1');

    const emailInput = page.locator('#register-email');
    const isValidBeforeSubmit = await emailInput.evaluate(
      (element) => (element as HTMLInputElement).checkValidity()
    );
    expect(isValidBeforeSubmit).toBe(false);

    await page.locator('form[action="/api/auth/register"] button[type="submit"]').click();
    await expect(page).toHaveURL(/\/register(?:\?.*)?$/);
  });

  test('login wrong password and unknown email return same generic error message', async ({ page }) => {
    const creds = createCredentials('auth-generic-login');

    await registerOwnerViaUI(page, creds);
    await expect(page).toHaveURL(/\/recovery-code$/);

    await logoutViaAPI(page);

    await page.goto('/login');
    await page.locator('#login-email').fill(creds.email);
    await page.locator('#login-password').fill('WrongPass1');
    await page.locator('form[action="/api/auth/login"] button[type="submit"]').click();

    await expect(page).toHaveURL(/\/login$/);
    expectNoSensitiveAuthParams(page.url());
    const wrongPasswordMessage = ((await page.locator('.status-error').textContent()) ?? '').trim();

    await page.goto('/login');
    await page.locator('#login-email').fill(createCredentials('auth-missing-email').email);
    await page.locator('#login-password').fill('WrongPass1');
    await page.locator('form[action="/api/auth/login"] button[type="submit"]').click();

    await expect(page).toHaveURL(/\/login$/);
    expectNoSensitiveAuthParams(page.url());
    const missingEmailMessage = ((await page.locator('.status-error').textContent()) ?? '').trim();

    expect(wrongPasswordMessage).toBeTruthy();
    expect(missingEmailMessage).toBe(wrongPasswordMessage);
  });

  test('remember me controls auth cookie persistence (session vs 30 days)', async ({ page, context }) => {
    const creds = createCredentials('auth-remember');

    await registerOwnerViaUI(page, creds);
    await expect(page).toHaveURL(/\/recovery-code$/);
    await page.locator('#recovery-code-saved').check();
    await page.locator('form[action] button[type="submit"]').click();
    await completeOnboardingIfPresent(page);

    await logoutViaAPI(page);
    await loginViaUI(page, creds, false);
    await expect(page).toHaveURL(/\/dashboard$/);

    const sessionCookie = await cookieByName(context, 'ovumcy_auth');
    expect(sessionCookie).toBeTruthy();
    expect(sessionCookie?.expires ?? 0).toBeLessThanOrEqual(0);

    await logoutViaAPI(page);
    await loginViaUI(page, creds, true);
    await expect(page).toHaveURL(/\/dashboard$/);

    const persistentCookie = await cookieByName(context, 'ovumcy_auth');
    expect(persistentCookie).toBeTruthy();
    expect(persistentCookie?.expires ?? 0).toBeGreaterThan(
      Math.floor(Date.now() / 1000) + 20 * 24 * 60 * 60
    );
  });

  test('password visibility toggles work on login, register and settings forms', async ({ page }) => {
    const assertToggle = async (inputSelector: string, toggleSelector: string) => {
      await expect(page.locator(inputSelector)).toHaveAttribute('type', 'password');
      await page.locator(toggleSelector).click();
      await expect(page.locator(inputSelector)).toHaveAttribute('type', 'text');
      await page.locator(toggleSelector).click();
      await expect(page.locator(inputSelector)).toHaveAttribute('type', 'password');
    };

    await page.goto('/login');
    await assertToggle('#login-password', '#login-password + [data-password-toggle]');

    await page.goto('/register');
    await assertToggle('#register-password', '#register-password + [data-password-toggle]');
    await assertToggle(
      '#register-confirm-password',
      '#register-confirm-password + [data-password-toggle]'
    );

    const creds = createCredentials('auth-toggle-settings');
    await registerOwnerViaUI(page, creds);
    await expect(page).toHaveURL(/\/recovery-code$/);
    await page.locator('#recovery-code-saved').check();
    await page.locator('form[action] button[type="submit"]').click();
    await completeOnboardingIfPresent(page);

    await page.goto('/settings');
    await expect(page).toHaveURL(/\/settings$/);
    await assertToggle(
      '#settings-current-password',
      '#settings-current-password + [data-password-toggle]'
    );
  });

  test('logout via UI redirects to login and blocks protected pages after back navigation', async ({
    page,
  }) => {
    const creds = createCredentials('auth-logout-ui');

    await registerOwnerViaUI(page, creds);
    await expect(page).toHaveURL(/\/recovery-code$/);
    await page.locator('#recovery-code-saved').check();
    await page.locator('form[action] button[type="submit"]').click();
    await completeOnboardingIfPresent(page);

    await page.locator('.nav-logout-form button[type="submit"]').click();
    await expect(page.locator('#confirm-modal')).toBeVisible();
    await page.locator('#confirm-modal-accept').click();

    await expect(page).toHaveURL(/\/login$/);

    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/login$/);

    await page.goBack();
    expect(pathOf(page.url())).toBe('/login');
  });

  test('authenticated user is redirected away from /login and /register', async ({ page }) => {
    const creds = createCredentials('auth-redirects');

    await registerOwnerViaUI(page, creds);
    await expect(page).toHaveURL(/\/recovery-code$/);

    await page.goto('/login');
    await expect(page).toHaveURL(/\/onboarding(?:\?.*)?$/);

    await page.goto('/register');
    await expect(page).toHaveURL(/\/onboarding(?:\?.*)?$/);

    await completeOnboardingIfPresent(page);

    await page.goto('/login');
    await expect(page).toHaveURL(/\/dashboard$/);

    await page.goto('/register');
    await expect(page).toHaveURL(/\/dashboard$/);
  });
});
