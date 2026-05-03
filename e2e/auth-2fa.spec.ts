import { expect, test } from '@playwright/test';
import { generateSync } from 'otplib';
import {
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  cookieByName,
  createCredentials,
  loginViaUI,
  registerOwnerViaUI,
} from './support/auth-helpers';

// Reads the raw TOTP secret from the visible manual-entry element on the
// enrollment page (the same string the user copies into their authenticator).
async function readTOTPSecret(page: import('@playwright/test').Page): Promise<string> {
  const el = page.locator('[data-totp-manual-secret]');
  await expect(el).toBeVisible();
  const secret = (await el.textContent())?.trim() ?? '';
  if (!secret) throw new Error('manual TOTP secret element is missing or empty');
  return secret;
}

test.describe('Auth: TOTP two-factor authentication', () => {
  test('setup page shows QR code and manual secret before enrollment', async ({ page }) => {
    const creds = createCredentials('2fa-setup-qr');
    await registerOwnerViaUI(page, creds);
    await continueFromRecoveryCode(page);
    await completeOnboardingIfPresent(page);

    await page.goto('/settings/2fa');
    await expect(page).toHaveURL('/settings/2fa');

    // QR image rendered as inline data URI
    const qrImage = page.locator('img[src^="data:image/png;base64,"]');
    await expect(qrImage).toBeVisible();

    // Manual secret attribute present and non-empty
    const secret = await readTOTPSecret(page);
    expect(secret.length).toBeGreaterThan(10);
  });

  test('enrolling with a valid code enables 2FA', async ({ page, context }) => {
    const creds = createCredentials('2fa-enroll');
    await registerOwnerViaUI(page, creds);
    await continueFromRecoveryCode(page);
    await completeOnboardingIfPresent(page);

    await page.goto('/settings/2fa');
    const secret = await readTOTPSecret(page);

    const code = generateSync({ secret, strategy: 'totp' });
    await page.locator('input[name="code"]').fill(code);
    await page.locator('button[type="submit"]').click();

    // After successful enrollment the management view shows the disable button.
    await expect(page.locator('button[type="submit"]', { hasText: /disable/i })).toBeVisible({
      timeout: 5_000,
    });

    // The ovumcy_totp_setup cookie should be cleared.
    const setupCookie = await cookieByName(context, 'ovumcy_totp_setup');
    expect(setupCookie).toBeFalsy();
  });

  test('login after enrollment redirects to 2FA challenge page', async ({
    page,
    context,
    browserName,
  }) => {
    test.skip(browserName === 'webkit', 'flaky redirect timing on webkit; covered in chromium');

    const creds = createCredentials('2fa-login-redirect');
    await registerOwnerViaUI(page, creds);
    await continueFromRecoveryCode(page);
    await completeOnboardingIfPresent(page);

    // Enroll
    await page.goto('/settings/2fa');
    const secret = await readTOTPSecret(page);
    const code = generateSync({ secret, strategy: 'totp' });
    await page.locator('input[name="code"]').fill(code);
    await page.locator('button[type="submit"]').click();
    await expect(page.locator('button[type="submit"]', { hasText: /disable/i })).toBeVisible({
      timeout: 5_000,
    });

    // Log out
    await page.goto('/api/auth/logout', { waitUntil: 'networkidle' });

    // Log back in — should hit challenge page
    await page.goto('/login');
    await page.locator('input[name="email"]').fill(creds.email);
    await page.locator('input[name="password"]').fill(creds.password);
    await page.locator('button[type="submit"]').click();

    await expect(page).toHaveURL('/auth/2fa', { timeout: 5_000 });

    // A pending TOTP cookie must be present (no auth session yet)
    const authCookie = await cookieByName(context, 'ovumcy_auth');
    expect(authCookie).toBeFalsy();
    const pendingCookie = await cookieByName(context, 'ovumcy_totp_pending');
    expect(pendingCookie).toBeTruthy();
  });

  test('completing the challenge with a valid code issues a session', async ({
    page,
    context,
  }) => {
    const creds = createCredentials('2fa-challenge-valid');
    await registerOwnerViaUI(page, creds);
    await continueFromRecoveryCode(page);
    await completeOnboardingIfPresent(page);

    // Enroll
    await page.goto('/settings/2fa');
    const secret = await readTOTPSecret(page);
    const enrollCode = generateSync({ secret, strategy: 'totp' });
    await page.locator('input[name="code"]').fill(enrollCode);
    await page.locator('button[type="submit"]').click();
    await expect(page.locator('button[type="submit"]', { hasText: /disable/i })).toBeVisible({
      timeout: 5_000,
    });

    // Log out
    await page.goto('/api/auth/logout', { waitUntil: 'networkidle' });

    // Log back in
    await page.goto('/login');
    await page.locator('input[name="email"]').fill(creds.email);
    await page.locator('input[name="password"]').fill(creds.password);
    await page.locator('button[type="submit"]').click();
    await expect(page).toHaveURL('/auth/2fa', { timeout: 5_000 });

    // Provide valid code on the challenge page
    const challengeCode = generateSync({ secret, strategy: 'totp' });
    await page.locator('input[name="code"]').fill(challengeCode);
    await page.locator('button[type="submit"]').click();

    // Should be on the dashboard
    await expect(page).toHaveURL('/', { timeout: 5_000 });
    const authCookie = await cookieByName(context, 'ovumcy_auth');
    expect(authCookie).toBeTruthy();
  });

  test('invalid code on challenge page is rejected without issuing a session', async ({
    page,
    context,
  }) => {
    const creds = createCredentials('2fa-challenge-invalid');
    await registerOwnerViaUI(page, creds);
    await continueFromRecoveryCode(page);
    await completeOnboardingIfPresent(page);

    // Enroll
    await page.goto('/settings/2fa');
    const secret = await readTOTPSecret(page);
    const enrollCode = generateSync({ secret, strategy: 'totp' });
    await page.locator('input[name="code"]').fill(enrollCode);
    await page.locator('button[type="submit"]').click();
    await expect(page.locator('button[type="submit"]', { hasText: /disable/i })).toBeVisible({
      timeout: 5_000,
    });

    // Log out
    await page.goto('/api/auth/logout', { waitUntil: 'networkidle' });

    // Log back in
    await page.goto('/login');
    await page.locator('input[name="email"]').fill(creds.email);
    await page.locator('input[name="password"]').fill(creds.password);
    await page.locator('button[type="submit"]').click();
    await expect(page).toHaveURL('/auth/2fa', { timeout: 5_000 });

    // Submit wrong code
    await page.locator('input[name="code"]').fill('000000');
    await page.locator('button[type="submit"]').click();

    // Should stay on challenge page
    await expect(page).toHaveURL('/auth/2fa', { timeout: 5_000 });
    const authCookie = await cookieByName(context, 'ovumcy_auth');
    expect(authCookie).toBeFalsy();
  });

  test('disabling 2FA with correct password stops the challenge on next login', async ({
    page,
    context,
  }) => {
    const creds = createCredentials('2fa-disable');
    await registerOwnerViaUI(page, creds);
    await continueFromRecoveryCode(page);
    await completeOnboardingIfPresent(page);

    // Enroll
    await page.goto('/settings/2fa');
    const secret = await readTOTPSecret(page);
    const enrollCode = generateSync({ secret, strategy: 'totp' });
    await page.locator('input[name="code"]').fill(enrollCode);
    await page.locator('button[type="submit"]').click();
    await expect(page.locator('button[type="submit"]', { hasText: /disable/i })).toBeVisible({
      timeout: 5_000,
    });

    // Disable
    await page.locator('input[name="password"]').fill(creds.password);
    await page.locator('button[type="submit"]', { hasText: /disable/i }).click();

    // After disabling, the QR setup view should reappear.
    await expect(page.locator('img[src^="data:image/png;base64,"]')).toBeVisible({
      timeout: 5_000,
    });

    // Log out and back in — should land directly on dashboard (no challenge)
    await page.goto('/api/auth/logout', { waitUntil: 'networkidle' });
    await loginViaUI(page, creds);
    await expect(page).not.toHaveURL('/auth/2fa');

    const authCookie = await cookieByName(context, 'ovumcy_auth');
    expect(authCookie).toBeTruthy();
  });
});
