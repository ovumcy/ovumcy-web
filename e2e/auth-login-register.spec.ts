import { expect, test } from '@playwright/test';
import {
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  cookieByName,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  expectNoSensitiveAuthParams,
  expectValueNotInWebStorage,
  loginViaUI,
  logoutViaAPI,
  pathOf,
  readRecoveryCode,
  registerOwnerViaUI,
  requestSubmitForm,
} from './support/auth-helpers';

test.describe('Auth: register, login, logout', () => {
  test('registers valid account and lands on inline recovery step without PII in URL', async ({
    page,
    context,
  }) => {
    const creds = createCredentials('auth-register');

    await registerOwnerViaUI(page, creds);

    await expectInlineRegisterRecoveryStep(page);
    expectNoSensitiveAuthParams(page.url());
    await readRecoveryCode(page);

    const authCookie = await cookieByName(context, 'ovumcy_auth');
    const recoveryCookie = await cookieByName(context, 'ovumcy_recovery_code');

    expect(authCookie).toBeTruthy();
    expect(recoveryCookie).toBeFalsy();
  });

  test('register duplicate email shows error and keeps clean redirect URL', async ({ page }) => {
    const creds = createCredentials('auth-duplicate');

    await registerOwnerViaUI(page, creds);
    await expectInlineRegisterRecoveryStep(page);

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
    let registerRequests = 0;

    page.on('request', (request) => {
      if (request.method() === 'POST' && request.url().includes('/api/auth/register')) {
        registerRequests += 1;
      }
    });

    await page.goto('/register');
    await expect(page).toHaveURL(/\/register(?:\?.*)?$/);

    const checklist = page.locator('[data-password-guidance]');
    await expect(checklist.locator('[data-password-rule-item="length"]')).toHaveAttribute(
      'data-met',
      'false'
    );

    await page.locator('#register-email').fill(creds.email);
    await page.locator('#register-password').fill(creds.password);
    await page.locator('#register-confirm-password').fill(creds.password);

    await expect(checklist.locator('[data-password-rule-item="length"]')).toHaveAttribute(
      'data-met',
      'true'
    );
    await expect(checklist.locator('[data-password-rule-item="lower"]')).toHaveAttribute(
      'data-met',
      'true'
    );
    await expect(checklist.locator('[data-password-rule-item="upper"]')).toHaveAttribute(
      'data-met',
      'false'
    );
    await expect(checklist.locator('[data-password-rule-item="digit"]')).toHaveAttribute(
      'data-met',
      'false'
    );

    await requestSubmitForm(page.locator('form[action="/api/auth/register"]'));

    await expect(page).toHaveURL(/\/register$/);
    expectNoSensitiveAuthParams(page.url());
    await expect(page.locator('#register-client-status .status-error')).toBeVisible();
    await expect(page.locator('#register-email')).toHaveValue(creds.email);
    await expect(page.locator('#register-password')).toHaveValue(creds.password);
    await expect(page.locator('#register-confirm-password')).toHaveValue(creds.password);
    expect(registerRequests).toBe(0);
  });

  test('register form rejects invalid email via browser validation', async ({ page }) => {
    await page.goto('/register');
    await expect(page).toHaveURL(/\/register(?:\?.*)?$/);
    await expect(page.locator('#register-form')).toHaveAttribute('novalidate', '');

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
    await expect(page.locator('#register-client-status .status-error')).toBeVisible();
  });

  test('register form rejects emoji email via client validation', async ({ page }) => {
    const consoleErrors: string[] = [];
    page.on('console', (message) => {
      if (message.type() === 'error') {
        consoleErrors.push(message.text());
      }
    });

    await page.goto('/register');
    await expect(page).toHaveURL(/\/register(?:\?.*)?$/);

    await page.locator('#register-email').fill('test😀@test.com');
    await page.locator('#register-password').fill('StrongPass1');
    await page.locator('#register-confirm-password').fill('StrongPass1');

    const emailInput = page.locator('#register-email');
    const isValidBeforeSubmit = await emailInput.evaluate(
      (element) => (element as HTMLInputElement).checkValidity()
    );
    expect(isValidBeforeSubmit).toBe(false);

    await page.locator('form[action="/api/auth/register"] button[type="submit"]').click();
    await expect(page).toHaveURL(/\/register(?:\?.*)?$/);
    await expect(page.locator('#register-client-status .status-error')).toContainText(
      /valid email address|корректный email|correo válido/i
    );
    expect(
      consoleErrors.some((text) => /Pattern attribute value .* is not a valid regular expression/i.test(text))
    ).toBe(false);
  });

  test('register empty submit validates in top-down order and places the error next to the active field', async ({
    page,
  }) => {
    await page.goto('/register');
    await expect(page).toHaveURL(/\/register(?:\?.*)?$/);

    const form = page.locator('form[action="/api/auth/register"]');
    const status = page.locator('#register-client-status');
    const email = page.locator('#register-email');
    const password = page.locator('#register-password');
    const confirm = page.locator('#register-confirm-password');
    const passwordField = page.locator('.password-field', { has: password });
    const confirmField = page.locator('.password-field', { has: confirm });

    await requestSubmitForm(form);
    await expect(status.locator('.status-error')).toBeVisible();
    await expect(email).toBeFocused();
    await expect(page.locator('#register-email + #register-client-status')).toHaveCount(1);

    const emailValue = `ordered-${Date.now()}@example.com`;
    await email.fill(emailValue);
    await requestSubmitForm(form);
    await expect(password).toBeFocused();
    expect(await passwordField.evaluate((node) => node.nextElementSibling?.id || '')).toBe(
      'register-client-status'
    );

    await password.fill('StrongPass1');
    await requestSubmitForm(form);
    await expect(confirm).toBeFocused();
    expect(await confirmField.evaluate((node) => node.nextElementSibling?.id || '')).toBe(
      'register-client-status'
    );
  });

  test('login form uses custom validation and clears invalid-credentials banner on input', async ({
    page,
  }) => {
    const creds = createCredentials('auth-login-banner-clear');

    await registerOwnerViaUI(page, creds);
    await expectInlineRegisterRecoveryStep(page);
    await logoutViaAPI(page);

    await page.goto('/login');
    await expect(page.locator('#login-form')).toHaveAttribute('novalidate', '');

    await page.locator('form[action="/api/auth/login"] button[type="submit"]').click();
    await expect(page.locator('#login-client-status .status-error')).toBeVisible();

    await page.locator('#login-email').fill(creds.email);
    await page.locator('#login-password').fill('WrongPass1');
    await page.locator('form[action="/api/auth/login"] button[type="submit"]').click();

    const serverError = page.locator('[data-auth-server-error]');
    await expect(serverError).toBeVisible();

    await page.locator('#login-password').fill('StillWrong2');
    await expect(serverError).toHaveCount(0);
  });

  test('login wrong password keeps email but does not restore password from browser storage', async ({
    page,
  }) => {
    const creds = createCredentials('auth-login-no-password-draft');
    const attemptedPassword = 'WrongPass1';

    await registerOwnerViaUI(page, creds);
    await expectInlineRegisterRecoveryStep(page);
    await logoutViaAPI(page);

    await page.goto('/login');
    await page.locator('#login-email').fill(creds.email);
    await page.locator('#login-password').fill(attemptedPassword);
    await page.locator('form[action="/api/auth/login"] button[type="submit"]').click();

    await expect(page).toHaveURL(/\/login$/);
    expectNoSensitiveAuthParams(page.url());
    await expect(page.locator('[data-auth-server-error]')).toBeVisible();
    await expect(page.locator('#login-email')).toHaveValue(creds.email);
    await expect(page.locator('#login-password')).toHaveValue('');
    await expect(page.locator('#login-password')).toBeFocused();
    await expectValueNotInWebStorage(page, attemptedPassword);
  });

  test('login wrong password and unknown email return same generic error message', async ({ page }) => {
    const creds = createCredentials('auth-generic-login');

    await registerOwnerViaUI(page, creds);
    await expectInlineRegisterRecoveryStep(page);

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
    await expectInlineRegisterRecoveryStep(page);
    await continueFromRecoveryCode(page);
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
      await expect(page.locator(`${toggleSelector} svg.password-toggle-svg`)).toBeVisible();
      await expect(page.locator(inputSelector)).toHaveAttribute('type', 'password');
      await page.locator(toggleSelector).click();
      await expect(page.locator(inputSelector)).toHaveAttribute('type', 'text');
      await expect(page.locator(`${toggleSelector} svg.password-toggle-svg`)).toBeVisible();
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
    await expectInlineRegisterRecoveryStep(page);
    await continueFromRecoveryCode(page);
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
    let logoutRequests = 0;

    page.on('request', (request) => {
      if (request.method() === 'POST' && request.url().includes('/logout')) {
        logoutRequests += 1;
      }
    });

    await registerOwnerViaUI(page, creds);
    await expectInlineRegisterRecoveryStep(page);
    await continueFromRecoveryCode(page);
    await completeOnboardingIfPresent(page);

    await page.locator('.nav-logout-form button[type="submit"]').click();
    await expect(page.locator('#confirm-modal')).toBeVisible();
    await page.locator('#confirm-modal-accept').click();

    await expect.poll(() => logoutRequests).toBe(1);
    await expect(page).toHaveURL(/\/login$/);

    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/login$/);

    await page.goBack();
    expect(pathOf(page.url())).toBe('/login');
  });

  test('authenticated user is redirected away from /login and /register', async ({ page }) => {
    const creds = createCredentials('auth-redirects');

    await registerOwnerViaUI(page, creds);
    await expectInlineRegisterRecoveryStep(page);

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

test.describe('Auth: registration mode closed', () => {
  test.skip(process.env.REGISTRATION_MODE !== 'closed', 'Requires REGISTRATION_MODE=closed');

  test('closed mode hides signup CTA and shows disabled register state', async ({ page }) => {
    await page.goto('/login');
    await expect(page).toHaveURL(/\/login(?:\?.*)?$/);
    await expect(page.locator('[data-auth-signup-cta]')).toHaveCount(0);

    await page.goto('/register');
    await expect(page).toHaveURL(/\/register(?:\?.*)?$/);
    expectNoSensitiveAuthParams(page.url());
    await expect(page.locator('[data-registration-disabled]')).toBeVisible();
    await expect(page.locator('#register-form')).toHaveCount(0);
    await expect(page.locator('[data-auth-switch]')).toHaveAttribute('href', '/login');
  });
});
