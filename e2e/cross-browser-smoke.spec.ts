import { expect, test, type Page } from '@playwright/test';
import {
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  readRecoveryCode,
  registerOwnerViaUI,
} from './support/auth-helpers';
import { ensureNotesFieldVisible } from './support/note-helpers';
import { setRequestTimezoneFromBrowser } from './support/timezone-helpers';

async function registerOwnerAndReachDashboard(page: Page, prefix: string): Promise<void> {
  const credentials = createCredentials(prefix);

  await registerOwnerViaUI(page, credentials);
  await expectInlineRegisterRecoveryStep(page);

  await readRecoveryCode(page);
  await continueFromRecoveryCode(page);
  await completeOnboardingIfPresent(page);
  await setRequestTimezoneFromBrowser(page);

  await page.goto('/dashboard');
  await expect(page).toHaveURL(/\/dashboard$/);
  await expect(page.locator('.dashboard-status-line')).toBeVisible();
  await expect(page.locator('[data-dashboard-save-form]').first()).toBeVisible();
}

async function todayISO(page: Page): Promise<string> {
  const action = await page.locator('[data-dashboard-save-form]').first().getAttribute('hx-post');
  expect(action).toMatch(/^\/api\/days\/\d{4}-\d{2}-\d{2}$/);
  return String(action).replace('/api/days/', '');
}

test.describe('Cross-browser smoke', () => {
  test('owner can register, recover, onboard, and reach the dashboard', async ({ page }) => {
    await registerOwnerAndReachDashboard(page, 'cross-browser-auth');
  });

  test('dashboard save persists into calendar and stats routes', async ({ page }) => {
    await registerOwnerAndReachDashboard(page, 'cross-browser-journal');

    const notes = await ensureNotesFieldVisible(page, '#today-notes');
    const noteText = `cross-browser-note-${Date.now()}`;

    const flowMedium = page.locator('input[name="flow"][value="medium"]');
    const flowMediumChip = page.locator(
      'label.choice-option:has(input[name="flow"][value="medium"]) .radio-tile'
    );
    await page.locator('input[name="is_period"]').check();
    await expect(flowMedium).toBeEnabled();
    await flowMediumChip.click();
    await expect(flowMedium).toBeChecked();
    await notes.fill(noteText);
    await page.locator('button[data-save-button]').first().click();
    await expect(page.locator('#save-status .status-ok')).toBeVisible();

    const iso = await todayISO(page);
    await page.goto(`/calendar?month=${iso.slice(0, 7)}&day=${iso}`);
    await expect(page).toHaveURL(new RegExp(`/calendar\\?month=${iso.slice(0, 7)}&day=${iso}`));
    await expect(page.locator('#day-editor')).toContainText(noteText);

    await page.goto('/stats');
    await expect(page).toHaveURL(/\/stats$/);
    await expect(page.locator('h1.journal-title')).toContainText(/Insights|Анализ|Análisis/);
  });

  test('theme and language switches persist across core routes', async ({ page }) => {
    await registerOwnerAndReachDashboard(page, 'cross-browser-settings');

    await page.goto('/settings');
    await expect(page).toHaveURL(/\/settings$/);

    const html = page.locator('html');
    const interfaceForm = page.locator('[data-settings-interface-form]');
    await interfaceForm.locator('[data-settings-interface-theme-option="dark"] .radio-tile').click();
    await expect(html).toHaveAttribute('data-theme', 'dark');
    await interfaceForm.locator('[data-settings-interface-language-option="es"] .radio-tile').click();
    await interfaceForm.locator('[data-settings-interface-save]').click();
    await expect(page).toHaveURL(/\/settings$/);
    await expect(html).toHaveAttribute('lang', 'es');
    await expect(page.locator('h1.journal-title')).toContainText('Configuración');

    await page.goto('/calendar');
    await expect(page).toHaveURL(/\/calendar(?:\?.*)?$/);
    await expect(html).toHaveAttribute('lang', 'es');
    await expect(html).toHaveAttribute('data-theme', 'dark');
    await expect(page.locator('#calendar-grid-panel')).toBeVisible();
    await expect(page.locator('h1')).toContainText('Calendario');
  });
});
