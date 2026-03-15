import { expect, test, type Locator, type Page } from '@playwright/test';
import {
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  readRecoveryCode,
  registerOwnerViaUI,
} from './support/auth-helpers';
import { setRequestTimezoneFromBrowser } from './support/timezone-helpers';

function shiftISODate(iso: string, days: number): string {
  const [year, month, day] = iso.split('-').map((part) => Number(part));
  const shifted = new Date(year, month - 1, day);
  shifted.setDate(shifted.getDate() + days);
  const yyyy = shifted.getFullYear();
  const mm = String(shifted.getMonth() + 1).padStart(2, '0');
  const dd = String(shifted.getDate()).padStart(2, '0');
  return `${yyyy}-${mm}-${dd}`;
}

async function registerOwnerAndEnableIrregularMode(page: Page, prefix: string): Promise<void> {
  const credentials = createCredentials(prefix);

  await registerOwnerViaUI(page, credentials);
  await expectInlineRegisterRecoveryStep(page);
  await readRecoveryCode(page);
  await continueFromRecoveryCode(page);
  await completeOnboardingIfPresent(page);
  await setRequestTimezoneFromBrowser(page);

  await page.goto('/settings');
  await expect(page).toHaveURL(/\/settings$/);

  const cycleForm = page.locator('section#settings-cycle form[action="/settings/cycle"]');
  await expect(cycleForm).toBeVisible();
  await cycleForm.locator('input[name="irregular_cycle"]').check();
  await cycleForm.locator('button[data-save-button]').click();
  await expect(page.locator('#settings-cycle-status .status-ok')).toBeVisible();
}

async function todayISO(page: Page): Promise<string> {
  await page.goto('/dashboard');
  await expect(page).toHaveURL(/\/dashboard$/);
  const action = await page.locator('[data-dashboard-save-form]').first().getAttribute('hx-post');
  expect(action).toMatch(/^\/api\/days\/\d{4}-\d{2}-\d{2}$/);
  return String(action).replace('/api/days/', '');
}

async function markCycleStart(page: Page, isoDate: string): Promise<void> {
  const month = isoDate.slice(0, 7);
  await page.goto(`/calendar?month=${month}&day=${isoDate}`);
  await expect(page).toHaveURL(new RegExp(`/calendar\\?month=${month}&day=${isoDate}`));

  const manualStartButton = page.locator(
    `[data-day-cycle-start-form][data-day-cycle-start-date="${isoDate}"] [data-day-cycle-start-button]`
  );
  await expect(manualStartButton).toBeVisible();
  await Promise.all([
    page.waitForNavigation({
      url: new RegExp(`/calendar\\?month=${month}&day=${isoDate}`),
      waitUntil: 'load',
    }),
    page.waitForResponse((response) => {
      return (
        response.request().method() === 'POST' &&
        response.url().includes(`/api/days/${isoDate}/cycle-start?source=calendar`)
      );
    }),
    manualStartButton.click(),
  ]);
}

async function openCalendarDayEditor(page: Page, isoDate: string): Promise<Locator> {
  const month = isoDate.slice(0, 7);
  await page.goto(`/calendar?month=${month}&day=${isoDate}`);
  await expect(page).toHaveURL(new RegExp(`/calendar\\?month=${month}&day=${isoDate}`));

  const editButton = page.locator(`[data-day-editor-open="${isoDate}"]`).first();
  await expect(editButton).toBeVisible();
  await editButton.click();

  const form = page.locator(`[data-day-editor-form][data-day-editor-date="${isoDate}"]`);
  await expect(form).toBeVisible();
  return form;
}

async function saveCycleFactorOnDay(page: Page, isoDate: string, factorKey: string): Promise<void> {
  const form = await openCalendarDayEditor(page, isoDate);
  const factorChip = form.locator(
    `label.choice-option:has(input[name="cycle_factor_keys"][value="${factorKey}"]) .check-chip`
  );
  await factorChip.click();
  await Promise.all([
    page.waitForResponse((response) => {
      return response.request().method() === 'POST' && response.url().includes(`/api/days/${isoDate}`);
    }),
    form.locator('button[data-save-button]').click(),
  ]);
  const savedForm = await openCalendarDayEditor(page, isoDate);
  await expect(savedForm.locator(`input[name="cycle_factor_keys"][value="${factorKey}"]`)).toBeChecked();
}

test.describe('Stats factor context', () => {
  test('owner sees conservative factor explanations in dashboard and stats', async ({ page }) => {
    await registerOwnerAndEnableIrregularMode(page, 'stats-factor-context');

    const today = await todayISO(page);
    const cycleStarts = [-112, -84, -56, -28].map((offset) => shiftISODate(today, offset));

    for (const cycleStart of cycleStarts) {
      await markCycleStart(page, cycleStart);
    }

    await saveCycleFactorOnDay(page, shiftISODate(cycleStarts[0], 2), 'stress');
    await saveCycleFactorOnDay(page, shiftISODate(cycleStarts[1], 2), 'travel');
    await saveCycleFactorOnDay(page, shiftISODate(cycleStarts[2], 2), 'stress');

    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);
    await expect(page.locator('a[href="/settings#settings-cycle"]')).toHaveCount(0);
    await expect(page.locator('.warning-amber')).toHaveCount(0);
    await expect(page.locator('[data-dashboard-next-period]')).toContainText(/\w{3} \d{1,2}, \d{4} — \w{3} \d{1,2}, \d{4}/);
    await expect(page.locator('[data-dashboard-next-period]')).not.toContainText('3 cycles are needed');
    const dashboardHint = page.locator('[data-dashboard-factor-hint]');
    await expect(dashboardHint).toBeVisible();
    await expect(dashboardHint.getByText('Stress', { exact: true }).first()).toBeVisible();
    await expect(dashboardHint.getByText('Travel', { exact: true }).first()).toBeVisible();

    await page.goto('/stats');
    await expect(page).toHaveURL(/\/stats$/);
    const factorSection = page.locator('[data-stats-factor-context]');
    await expect(factorSection).toBeVisible();
    await expect(factorSection).toContainText('Stress');
    await expect(factorSection).toContainText('Travel');
    await expect(factorSection).toContainText('Recent cycle context');
  });
});
