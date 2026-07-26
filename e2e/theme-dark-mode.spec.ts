import { expect, test, type Frame, type Page } from '@playwright/test';
import { dashboardPrimarySummaryMode } from './support/dashboard-helpers';
import { cancelConfirmDialog } from './support/confirm-dialog-helpers';
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

    // Cancelling the unsaved-changes prompt must leave the page where it is. A
    // URL assertion is already true the moment it runs, so it would pass just as
    // well with the guarded navigation still pending: record what the main frame
    // actually navigates to across the whole window instead.
    const brandLink = page.locator('a.brand-mark');
    const leaveTarget = new URL((await brandLink.getAttribute('href')) ?? '', page.url()).pathname;
    expect(leaveTarget).toBe('/dashboard');

    const navigatedTo: string[] = [];
    const recordNavigation = (frame: Frame) => {
      if (frame === page.mainFrame()) {
        navigatedTo.push(frame.url());
      }
    };
    page.on('framenavigated', recordNavigation);
    try {
      await brandLink.click();
      await cancelConfirmDialog(page);
      await expect(html).toHaveAttribute('data-theme', nextTheme);

      // Concrete signal that the guarded link has had its chance: discarding the
      // draft is a real interaction with — and a state transition on — the very
      // document the accept branch would have navigated away from.
      await discardButton.click();
      await expect(html).toHaveAttribute('data-theme', String(initialTheme));
    } finally {
      page.off('framenavigated', recordNavigation);
    }
    expect(navigatedTo, `cancelling the prompt must not navigate to ${leaveTarget}`).toEqual([]);

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
    await dashboardPrimarySummaryMode(page);

    const secondPage = await context.newPage();
    await secondPage.goto('/privacy');
    await expect(secondPage.locator('html')).toHaveAttribute('data-theme', nextTheme);
    await secondPage.close();

    await logoutViaAPI(page);
  });
});
