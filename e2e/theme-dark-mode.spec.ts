import { expect, test, type Page } from '@playwright/test';
import {
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  createCredentials,
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
  await expect(page).toHaveURL(/\/recovery-code$/);

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

    const html = page.locator('html');
    const toggle = page.locator('[data-theme-toggle]');
    await expect(toggle).toBeVisible();

    const initialTheme = await html.getAttribute('data-theme');
    expect(initialTheme === 'light' || initialTheme === 'dark').toBeTruthy();

    const nextTheme = initialTheme === 'dark' ? 'light' : 'dark';
    await toggle.click();

    await expect(html).toHaveAttribute('data-theme', nextTheme);
    await expect
      .poll(async () => page.evaluate(() => window.localStorage.getItem('ovumcy_theme')))
      .toBe(nextTheme);

    await page.reload();
    await expect(html).toHaveAttribute('data-theme', nextTheme);

    await page.goto('/settings');
    await expect(html).toHaveAttribute('data-theme', nextTheme);
    await expect(page.locator('h1.journal-title')).toBeVisible();

    const secondPage = await context.newPage();
    await secondPage.goto('/privacy');
    await expect(secondPage.locator('html')).toHaveAttribute('data-theme', nextTheme);
    await secondPage.close();

    await logoutViaAPI(page);
  });
});

