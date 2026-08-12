import { expect, test, type Page } from '@playwright/test';
import {
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  readRecoveryCode,
  registerOwnerViaUI,
} from './support/auth-helpers';
import { saveDashboardEntry } from './support/dashboard-helpers';
import { saveSettingsLanguage } from './support/language-helpers';
import { localeText, type Locale } from './support/locale-helpers';
import { ensureNotesFieldVisible } from './support/note-helpers';
import { setRequestTimezoneFromBrowser } from './support/timezone-helpers';

// Address the page title by the key it declares and take the expected copy from
// the catalogue. The literals this replaces ('Configuración', 'Calendario') and
// the /Insights|Аналитика|Análisis/ alternation both re-typed shipped strings
// into the spec, and the alternation matched any of three languages regardless
// of which one the page was rendering.
async function expectPageTitle(page: Page, key: string, locale: Locale = 'en'): Promise<void> {
  const title = page.locator(`h1[data-title-key="${key}"]`);
  await expect(title).toBeVisible();
  await expect(title).toContainText(localeText(locale, key));
}

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
  await expect(page.locator('[data-dashboard-status-header]')).toBeVisible();
  await expect(page.locator('[data-dashboard-status-line]')).toBeVisible();
  await expect(page.locator('[data-dashboard-save-form]').first()).toBeVisible();
}

async function todayISO(page: Page): Promise<string> {
  const action = await page.locator('[data-dashboard-save-form]').first().getAttribute('hx-put');
  expect(action).toMatch(/^\/api\/v1\/days\/\d{4}-\d{2}-\d{2}$/);
  return String(action).replace('/api/v1/days/', '');
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
    await saveDashboardEntry(page, async () => {
      await page.locator('input[name="is_period"]').check();
      await expect(flowMedium).toBeEnabled();
      await flowMediumChip.click();
      await expect(flowMedium).toBeChecked();
      await notes.fill(noteText);
    });

    const iso = await todayISO(page);
    await page.goto(`/calendar?month=${iso.slice(0, 7)}&day=${iso}`);
    await expect(page).toHaveURL(new RegExp(`/calendar\\?month=${iso.slice(0, 7)}&day=${iso}`));
    await expect(page.locator('#day-editor')).toContainText(noteText);

    await page.goto('/stats');
    await expect(page).toHaveURL(/\/stats$/);
    await expectPageTitle(page, 'stats.title');
  });

  test('theme and language switches persist across core routes', async ({ page }) => {
    await registerOwnerAndReachDashboard(page, 'cross-browser-settings');

    await page.goto('/settings');
    await expect(page).toHaveURL(/\/settings$/);

    const html = page.locator('html');
    const interfaceForm = page.locator('[data-settings-interface-form]');
    await interfaceForm.locator('[data-settings-interface-theme-option="dark"] .radio-tile').click();
    await expect(html).toHaveAttribute('data-theme', 'dark');
    // Bind the language save to its own PATCH before navigating away — a bare
    // save click followed by page.goto races the in-flight request and can drop
    // the just-chosen language (saveSettingsLanguage documents the mechanism).
    await saveSettingsLanguage(page, 'es');
    await expect(html).toHaveAttribute('lang', 'es');
    await expectPageTitle(page, 'settings.title', 'es');

    await page.goto('/calendar');
    await expect(page).toHaveURL(/\/calendar(?:\?.*)?$/);
    await expect(html).toHaveAttribute('lang', 'es');
    await expect(html).toHaveAttribute('data-theme', 'dark');
    await expect(page.locator('#calendar-grid-panel')).toBeVisible();
    await expectPageTitle(page, 'calendar.title', 'es');
  });
});
