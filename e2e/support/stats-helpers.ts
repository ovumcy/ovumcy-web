import { expect, type Locator, type Page } from '@playwright/test';
import {
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  readRecoveryCode,
  registerOwnerViaUI,
} from './auth-helpers';
import { setRequestTimezoneFromBrowser } from './timezone-helpers';

export function shiftISODate(iso: string, days: number): string {
  const [year, month, day] = iso.split('-').map((part) => Number(part));
  const shifted = new Date(year, month - 1, day);
  shifted.setDate(shifted.getDate() + days);
  const yyyy = shifted.getFullYear();
  const mm = String(shifted.getMonth() + 1).padStart(2, '0');
  const dd = String(shifted.getDate()).padStart(2, '0');
  return `${yyyy}-${mm}-${dd}`;
}

export async function registerOwnerAndEnableIrregularMode(
  page: Page,
  prefix: string
): Promise<void> {
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

export async function todayISOFromDashboard(page: Page): Promise<string> {
  await page.goto('/dashboard');
  await expect(page).toHaveURL(/\/dashboard$/);
  const action = await page.locator('[data-dashboard-save-form]').first().getAttribute('hx-post');
  expect(action).toMatch(/^\/api\/days\/\d{4}-\d{2}-\d{2}$/);
  return String(action).replace('/api/days/', '');
}

export async function markCycleStart(page: Page, isoDate: string): Promise<void> {
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

export async function openCalendarDayEditor(page: Page, isoDate: string): Promise<Locator> {
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

export async function saveCycleFactorOnDay(
  page: Page,
  isoDate: string,
  factorKey: string
): Promise<void> {
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

export async function saveBBTOnDay(page: Page, isoDate: string, value: string): Promise<void> {
  await page.goto('/settings');
  await expect(page).toHaveURL(/\/settings$/);

  const trackingSection = page.locator('#settings-tracking');
  await expect(trackingSection).toBeVisible();

  const trackBBT = trackingSection.locator('input[name="track_bbt"]');
  if (!(await trackBBT.isChecked())) {
    await trackBBT.check();
    await trackingSection.locator('button[data-save-button]').click();
    await expect(page.locator('#settings-tracking-status .status-ok')).toBeVisible();
  }

  const form = await openCalendarDayEditor(page, isoDate);
  const bbtInput = form.locator('#calendar-bbt');
  await expect(bbtInput).toBeVisible();
  await bbtInput.fill(value);
  await Promise.all([
    page.waitForResponse((response) => {
      return response.request().method() === 'POST' && response.url().includes(`/api/days/${isoDate}`);
    }),
    form.locator('button[data-save-button]').click(),
  ]);

  const savedForm = await openCalendarDayEditor(page, isoDate);
  await expect(savedForm.locator('#calendar-bbt')).not.toHaveValue('');
}
