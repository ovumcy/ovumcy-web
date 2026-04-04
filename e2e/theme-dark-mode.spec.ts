import { expect, test, type Page } from '@playwright/test';
import {
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  logoutViaAPI,
  readRecoveryCode,
  registerOwnerViaUI,
} from './support/auth-helpers';

async function registerAndReachDashboard(
  page: Page,
  prefix: string
): Promise<{ email: string; password: string }> {
  const credentials = createCredentials(prefix);

  await registerOwnerViaUI(page, credentials);
  await expectInlineRegisterRecoveryStep(page);

  await readRecoveryCode(page);
  await continueFromRecoveryCode(page);
  await completeOnboardingIfPresent(page);

  await page.goto('/dashboard');
  await expect(page).toHaveURL(/\/dashboard$/);
  return credentials;
}

test.describe('Theme mode', () => {
  test('theme toggle switches mode and persists between pages', async ({ page, context }) => {
    await registerAndReachDashboard(page, 'theme-mode');

    await page.goto('/settings');
    await expect(page).toHaveURL(/\/settings$/);

    const interfaceForm = page.locator('[data-settings-interface-form]');
    const html = page.locator('html');
    const lightOption = interfaceForm.locator('[data-settings-interface-theme-option="light"]');
    const darkOption = interfaceForm.locator('[data-settings-interface-theme-option="dark"]');
    const saveButton = interfaceForm.locator('[data-settings-interface-save]');
    const discardButton = interfaceForm.locator('[data-settings-interface-discard]');
    await expect(lightOption).toBeVisible();
    await expect(darkOption).toBeVisible();
    await expect(saveButton).toBeDisabled();
    await expect(discardButton).toBeDisabled();

    const initialTheme = await html.getAttribute('data-theme');
    expect(initialTheme === 'light' || initialTheme === 'dark').toBeTruthy();
    const storedBefore = await page.evaluate(() => window.localStorage.getItem('ovumcy_theme'));

    const nextTheme = initialTheme === 'dark' ? 'light' : 'dark';
    const nextOption = nextTheme === 'dark' ? darkOption : lightOption;
    const previousOption = nextTheme === 'dark' ? lightOption : darkOption;
    await nextOption.locator('.radio-tile').click();

    await expect(html).toHaveAttribute('data-theme', nextTheme);
    await expect(nextOption).toHaveAttribute('data-selected', 'true');
    await expect(previousOption).toHaveAttribute('data-selected', 'false');
    await expect(saveButton).toBeEnabled();
    await expect(discardButton).toBeEnabled();
    await expect
      .poll(async () => page.evaluate(() => window.localStorage.getItem('ovumcy_theme')))
      .toBe(storedBefore);

    await page.locator('a.brand-mark').click();
    await expect(page.locator('#confirm-modal')).toBeVisible();
    await page.locator('#confirm-modal-cancel').click();
    await expect(page).toHaveURL(/\/settings$/);
    await expect(html).toHaveAttribute('data-theme', nextTheme);

    await discardButton.click();
    await expect(html).toHaveAttribute('data-theme', String(initialTheme));
    await expect(nextOption).toHaveAttribute('data-selected', 'false');
    await expect(previousOption).toHaveAttribute('data-selected', 'true');
    await expect(saveButton).toBeDisabled();
    await expect(discardButton).toBeDisabled();
    await expect
      .poll(async () => page.evaluate(() => window.localStorage.getItem('ovumcy_theme')))
      .toBe(storedBefore);

    await nextOption.locator('.radio-tile').click();
    await expect(saveButton).toBeEnabled();
    await saveButton.click();
    await expect(saveButton).toBeDisabled();
    await expect(html).toHaveAttribute('data-theme', nextTheme);
    await expect
      .poll(async () => page.evaluate(() => window.localStorage.getItem('ovumcy_theme')))
      .toBe(nextTheme);

    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);
    await expect(html).toHaveAttribute('data-theme', nextTheme);
    await expect(page.locator('.dashboard-status-line')).toBeVisible();

    const secondPage = await context.newPage();
    await secondPage.goto('/privacy');
    await expect(secondPage.locator('html')).toHaveAttribute('data-theme', nextTheme);
    await secondPage.close();

    await logoutViaAPI(page);
  });
});
