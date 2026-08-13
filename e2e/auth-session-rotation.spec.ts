import { expect, test, type Locator, type Page } from '@playwright/test';
import {
  completeOnboardingIfPresent,
  confirmRecoveryCode,
  continueFromRecoveryCode,
  createCredentials,
  expectDedicatedRecoveryPage,
  expectInlineRegisterRecoveryStep,
  loginViaUI,
  readRecoveryCode,
  registerOwnerViaUI,
} from './support/auth-helpers';

async function setRangeValue(locator: Locator, value: number): Promise<void> {
  await locator.evaluate((element, rawValue) => {
    const input = element as HTMLInputElement;
    input.value = String(rawValue);
    input.dispatchEvent(new Event('input', { bubbles: true }));
    input.dispatchEvent(new Event('change', { bubbles: true }));
  }, value);
}

async function registerOwnerAndOpenSettings(page: Page, prefix: string) {
  const creds = createCredentials(prefix);

  await registerOwnerViaUI(page, creds);
  await expectInlineRegisterRecoveryStep(page);

  const recoveryCode = await readRecoveryCode(page);
  await continueFromRecoveryCode(page);
  await completeOnboardingIfPresent(page);

  await page.goto('/settings');
  await expect(page).toHaveURL(/\/settings$/);

  return { ...creds, recoveryCode };
}

test.describe('Auth session rotation', () => {
  test('regenerating recovery code revokes other active sessions but keeps the originating one', async ({
    browser,
    page,
  }) => {
    const state = await registerOwnerAndOpenSettings(page, 'session-rotation');

    // Open a second, isolated browser context and sign in with the same
    // credentials. Two contexts means two separate cookie jars / "devices".
    const otherContext = await browser.newContext();
    const otherPage = await otherContext.newPage();
    try {
      await loginViaUI(otherPage, { email: state.email, password: state.password });
      await expect(otherPage).toHaveURL(/\/dashboard(?:\?.*)?$/);

      // Make a benign change on /settings so the originating context has a
      // visible artifact to confirm the fresh cookie kept it signed in.
      const cycleForm = page.locator('#settings-cycle form[action="/api/v1/users/current/cycle"]');
      await expect(cycleForm).toBeVisible();
      await setRangeValue(page.locator('#settings-cycle-length'), 29);
      await cycleForm.locator('button[data-save-button]').click();
      await expect(page.locator('#settings-cycle-status .status-ok')).toBeVisible();

      // Regenerate the recovery code from the originating context. The
      // handler atomically bumps AuthSessionVersion and issues a fresh auth
      // cookie to the current request only.
      await page
        .locator(
          'form[action="/api/v1/users/current/recovery-code"] #settings-recovery-code-password'
        )
        .fill(state.password);
      await page
        .locator('form[action="/api/v1/users/current/recovery-code"] button[type="submit"]')
        .click();
      await expect(page.locator('#confirm-modal')).toBeVisible();
      await page.locator('#confirm-modal-accept').click();

      await expectDedicatedRecoveryPage(page);
      const rotatedCode = await readRecoveryCode(page);
      expect(rotatedCode).not.toBe(state.recoveryCode);
      await confirmRecoveryCode(page);

      // Originating context: cookie was rotated alongside the bump, the user
      // is still signed in and the previously saved cycle length persists.
      await expect(page).toHaveURL(/\/settings(?:\?.*)?$/);
      await page.goto('/dashboard');
      await expect(page).toHaveURL(/\/dashboard(?:\?.*)?$/);

      // Other context: cookie still carries the pre-bump AuthSessionVersion,
      // so any authenticated request must redirect to /login.
      await otherPage.goto('/dashboard');
      await expect(otherPage).toHaveURL(/\/login(?:\?.*)?$/);
    } finally {
      await otherContext.close();
    }
  });
});
