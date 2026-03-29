import fs from 'node:fs/promises';
import { expect, test } from '@playwright/test';
import {
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  cookieByName,
  createCredentials,
  enableClipboardRoundTripIfSupported,
  expectDedicatedRecoveryPage,
  expectInlineRegisterRecoveryStep,
  expectNoSensitiveAuthParams,
  expectValueNotInWebStorage,
  loginViaUI,
  logoutViaAPI,
  openForgotPasswordRecoveryStep,
  readRecoveryCode,
  registerOwnerViaUI,
} from './support/auth-helpers';

test.describe('Auth: recovery and reset password', () => {
  test('post-registration recovery step supports copy/download and blocks continue until confirmation', async ({
    page,
    context,
  }) => {
    const creds = createCredentials('auth-recovery-tools');

    await registerOwnerViaUI(page, creds);
    await expectInlineRegisterRecoveryStep(page);

    const recoveryCode = await readRecoveryCode(page);

    const continueButton = page.locator('form[data-recovery-code-confirm] button[type="submit"]');
    await expect(continueButton).toHaveAttribute('aria-disabled', 'true');
    await expectInlineRegisterRecoveryStep(page);
    await expect(page.locator('#recovery-code-saved')).not.toBeChecked();

    const canAssertClipboardRoundTrip = await enableClipboardRoundTripIfSupported(page, context);

    const toolButtons = page.locator('div.mt-4.flex.flex-wrap.gap-2 button.btn-secondary');
    if (canAssertClipboardRoundTrip) {
      await toolButtons.nth(0).click();
      await expect
        .poll(async () => page.evaluate(() => navigator.clipboard.readText()))
        .toBe(recoveryCode);
    }

    const downloadPromise = page.waitForEvent('download');
    await toolButtons.nth(1).click();
    const download = await downloadPromise;

    expect(download.suggestedFilename()).toBe('ovumcy-recovery-code.txt');
    const downloadPath = await download.path();
    expect(downloadPath).toBeTruthy();
    const downloadedContent = await fs.readFile(downloadPath!, 'utf8');
    expect(downloadedContent).toContain(recoveryCode);

    await page.locator('#recovery-code-saved').check();
    await expect(continueButton).toHaveAttribute('aria-disabled', 'false');
    const continueButtonBox = await continueButton.boundingBox();
    expect(continueButtonBox).toBeTruthy();
    await page.mouse.click(
      continueButtonBox!.x + continueButtonBox!.width / 2,
      continueButtonBox!.y + continueButtonBox!.height / 2
    );
    await expect(page).toHaveURL(/\/onboarding(?:\?.*)?$/);
  });

  test('forgot-password flow keeps PII out of URL and validates recovery code format', async ({
    page,
  }) => {
    const creds = createCredentials('auth-forgot-validation');

    await registerOwnerViaUI(page, creds);
    const recoveryCode = await readRecoveryCode(page);
    await logoutViaAPI(page);

    await openForgotPasswordRecoveryStep(page, creds.email);
    expectNoSensitiveAuthParams(page.url());

    await page.locator('#recovery-code').fill('invalid-code-format');
    await page.locator('form[action="/api/auth/forgot-password"] button[type="submit"]').click();

    await expect(page).toHaveURL(/\/forgot-password$/);
    expectNoSensitiveAuthParams(page.url());
    await expect(page.locator('.status-error')).toBeVisible();

    await page.locator('#recovery-code').fill('OVUM-0000-0000-0000');
    await page.locator('form[action="/api/auth/forgot-password"] button[type="submit"]').click();

    await expect(page).toHaveURL(/\/forgot-password$/);
    expectNoSensitiveAuthParams(page.url());
    await expect(page.locator('.status-error')).toBeVisible();

    await page.locator('#recovery-code').fill(recoveryCode);
    await page.locator('form[action="/api/auth/forgot-password"] button[type="submit"]').click();

    await expect(page).toHaveURL(/\/reset-password$/);
    expectNoSensitiveAuthParams(page.url());
  });

  test('reset password via recovery code rotates credentials and old password stops working', async ({
    page,
  }) => {
    const creds = createCredentials('auth-reset-flow');
    const newPassword = 'EvenStronger2';

    await registerOwnerViaUI(page, creds);
    const oldRecoveryCode = await readRecoveryCode(page);
    await continueFromRecoveryCode(page);
    await completeOnboardingIfPresent(page);
    await logoutViaAPI(page);

    await openForgotPasswordRecoveryStep(page, creds.email);
    await page.locator('#recovery-code').fill(oldRecoveryCode);
    await page.locator('form[action="/api/auth/forgot-password"] button[type="submit"]').click();

    await expect(page).toHaveURL(/\/reset-password$/);

    await page.locator('#reset-password').fill(newPassword);
    await page.locator('#reset-password-confirm').fill(newPassword);
    await page.locator('form[action="/api/auth/reset-password"] button[type="submit"]').click();

    await expectDedicatedRecoveryPage(page);
    expectNoSensitiveAuthParams(page.url());

    const newRecoveryCode = await readRecoveryCode(page);
    expect(newRecoveryCode).not.toBe(oldRecoveryCode);
    await expectValueNotInWebStorage(page, newRecoveryCode);

    await continueFromRecoveryCode(page);
    await expect(page).toHaveURL(/\/dashboard(?:\?.*)?$/);

    await logoutViaAPI(page);

    await page.goto('/login');
    await page.locator('#login-email').fill(creds.email);
    await page.locator('#login-password').fill(creds.password);
    await page.locator('form[action="/api/auth/login"] button[type="submit"]').click();
    await expect(page).toHaveURL(/\/login$/);
    await expect(page.locator('.status-error')).toBeVisible();

    await loginViaUI(page, { email: creds.email, password: newPassword });
    await expect(page).toHaveURL(/\/dashboard$/);
  });

  test('reset-password server error clears as soon as the user edits the password fields', async ({
    page,
  }) => {
    const creds = createCredentials('auth-reset-error-clear');

    await registerOwnerViaUI(page, creds);
    const recoveryCode = await readRecoveryCode(page);
    await logoutViaAPI(page);

    await openForgotPasswordRecoveryStep(page, creds.email);
    await page.locator('#recovery-code').fill(recoveryCode);
    await page.locator('form[action="/api/auth/forgot-password"] button[type="submit"]').click();

    await expect(page).toHaveURL(/\/reset-password$/);

    await page.locator('#reset-password').fill('weak');
    await page.locator('#reset-password-confirm').fill('weak');
    await page.locator('form[action="/api/auth/reset-password"] button[type="submit"]').click();

    const serverError = page.locator('[data-auth-server-error]');
    await expect(serverError).toBeVisible();

    await page.locator('#reset-password').fill('ValidStrong2');
    await expect(serverError).toHaveCount(0);

    await page.locator('#reset-password-confirm').fill('ValidStrong2');
    await expect(page.locator('[data-auth-server-error]')).toHaveCount(0);
  });

  test('recovery code page is no longer available after re-login', async ({ page }) => {
    const creds = createCredentials('auth-recovery-once');

    await registerOwnerViaUI(page, creds);
    await readRecoveryCode(page);
    await continueFromRecoveryCode(page);
    await completeOnboardingIfPresent(page);
    await logoutViaAPI(page);

    await loginViaUI(page, creds);
    await expect(page).toHaveURL(/\/dashboard$/);

    await page.goto('/recovery-code');
    await expect(page).toHaveURL(/\/dashboard$/);
    await expect(page.locator('#recovery-code')).toHaveCount(0);
  });

  test('recovery code page is consumed after the first successful view', async ({ page }) => {
    const creds = createCredentials('auth-recovery-consumed');

    await registerOwnerViaUI(page, creds);
    await expectInlineRegisterRecoveryStep(page);
    await readRecoveryCode(page);

    await page.goto('/recovery-code');
    await expect(page).toHaveURL(/\/onboarding(?:\?.*)?$/);
    await expect(page.locator('#recovery-code')).toHaveCount(0);
  });

  test('basic security: csrf enforcement and cookie flags for auth/recovery/reset cookies', async ({
    page,
    context,
  }) => {
    const creds = createCredentials('auth-security-basic');

    await registerOwnerViaUI(page, creds);
    const recoveryCode = await readRecoveryCode(page);

    const authCookie = await cookieByName(context, 'ovumcy_auth');
    const recoveryCookie = await cookieByName(context, 'ovumcy_recovery_code');

    expect(authCookie).toBeTruthy();
    expect(authCookie?.httpOnly).toBe(true);
    expect(authCookie?.secure).toBe(false);

    expect(recoveryCookie).toBeFalsy();

    const csrfFailure = await page.request.post('/api/auth/logout', {
      form: {},
      maxRedirects: 0,
    });
    expect(csrfFailure.status()).toBe(403);

    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/onboarding(?:\?.*)?$/);

    await logoutViaAPI(page);

    await openForgotPasswordRecoveryStep(page, creds.email);
    await page.locator('#recovery-code').fill(recoveryCode);
    await page.locator('form[action="/api/auth/forgot-password"] button[type="submit"]').click();
    await expect(page).toHaveURL(/\/reset-password$/);

    const resetCookie = await cookieByName(context, 'ovumcy_reset_password');
    expect(resetCookie).toBeTruthy();
    expect(resetCookie?.httpOnly).toBe(true);
    expect(resetCookie?.secure).toBe(false);
  });
});
