import { expect, test, type Locator, type Page } from '@playwright/test';
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

function shiftISODate(iso: string, days: number): string {
  const [y, m, d] = iso.split('-').map((part) => Number(part));
  const date = new Date(y, m - 1, d);
  date.setDate(date.getDate() + days);

  const yyyy = date.getFullYear();
  const mm = String(date.getMonth() + 1).padStart(2, '0');
  const dd = String(date.getDate()).padStart(2, '0');
  return `${yyyy}-${mm}-${dd}`;
}

async function registerOwnerOnCalendar(page: Page, prefix: string): Promise<void> {
  const creds = createCredentials(prefix);

  await registerOwnerViaUI(page, creds);
  await expectInlineRegisterRecoveryStep(page);

  await readRecoveryCode(page);
  await continueFromRecoveryCode(page);
  await completeOnboardingIfPresent(page);

  await setRequestTimezoneFromBrowser(page);
  await page.goto('/calendar');
  await expect(page).toHaveURL(/\/calendar(?:\?.*)?$/);
}

async function openCalendarDayEditor(page: Page, isoDate: string) {
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

async function openCalendarNotes(form: Locator): Promise<void> {
  await ensureNotesFieldVisible(form, '#calendar-notes');
}

async function openSexActivityDisclosure(form: Locator): Promise<void> {
  const disclosure = form.locator('details[data-sex-activity-details]');
  const isOpen = await disclosure.evaluate((element) => element.hasAttribute('open'));
  if (!isOpen) {
    await disclosure.locator('summary').click();
  }
  await expect(form.locator('[data-sex-activity-option="protected"]')).toBeVisible();
}

async function todayISOFromCalendar(page: Page): Promise<string> {
  const todayButton = page.locator('button[data-day]:has(.calendar-today-pill)').first();
  await expect(todayButton).toBeVisible();
  const todayISO = await todayButton.getAttribute('data-day');
  expect(todayISO).toMatch(/^\d{4}-\d{2}-\d{2}$/);
  return todayISO!;
}

test.describe('Calendar page', () => {
  test('default month renders and navigation prev/next/today works', async ({ page }) => {
    await registerOwnerOnCalendar(page, 'calendar-nav');

    const navigationCard = page.locator('div.journal-card').filter({
      has: page.locator('a.btn-primary[href="/calendar"]'),
    }).first();
    const monthLabel = navigationCard.locator('p.journal-muted').first();
    const prevLink = navigationCard.locator('a.btn-secondary[href^="/calendar?month="]').first();
    const nextLink = navigationCard.locator('a.btn-secondary[href^="/calendar?month="]').nth(1);
    const todayLink = navigationCard.locator('a.btn-primary[href="/calendar"]');

    const initialLabel = ((await monthLabel.textContent()) ?? '').trim();
    expect(initialLabel.length).toBeGreaterThan(0);

    await prevLink.click();
    await expect(page).toHaveURL(/\/calendar\?month=\d{4}-\d{2}/);
    const prevLabel = ((await monthLabel.textContent()) ?? '').trim();
    expect(prevLabel).not.toBe(initialLabel);

    await nextLink.click();
    await expect(page).toHaveURL(/\/calendar\?month=\d{4}-\d{2}/);

    await todayLink.click();
    await expect(page).toHaveURL(/\/calendar$/);
    await expect(page.locator('button[data-day]:has(.calendar-today-pill)')).toHaveCount(1);
  });

  test('invalid month query redirects to the current calendar page', async ({ page }) => {
    await registerOwnerOnCalendar(page, 'calendar-invalid-month');

    await page.goto('/calendar?month=9999-99');
    await expect(page).toHaveURL(/\/calendar$/);
    await expect(page.locator('h1')).toContainText(/Calendar|Календарь|Calendario/);
  });

  test('legend includes period/predicted/fertility/ovulation markers', async ({ page }) => {
    await registerOwnerOnCalendar(page, 'calendar-legend');

    await expect(page.locator('.legend-dot.legend-dot-period')).toHaveCount(1);
    await expect(page.locator('.legend-dot.legend-dot-predicted')).toHaveCount(1);
    await expect(page.locator('.legend-outline')).toHaveCount(1);
    await expect(page.locator('.legend-dot.legend-dot-fertile-edge')).toHaveCount(1);
    await expect(page.locator('.legend-dot.legend-dot-fertile-peak')).toHaveCount(1);
    const ovulationDot = page.locator('.legend-item .calendar-ovulation-dot');
    const tentativeOvulation = page.locator('.legend-item .calendar-ovulation-dash');
    await expect(ovulationDot).toHaveCount(1);
    await expect(tentativeOvulation).toHaveCount(1);

    const styles = await ovulationDot.evaluate((node) => {
      const computed = window.getComputedStyle(node);
      return {
        width: parseFloat(computed.width || '0'),
        boxShadow: computed.boxShadow || '',
      };
    });
    expect(styles.width).toBeGreaterThanOrEqual(12);
    expect(styles.boxShadow).not.toBe('none');
  });

  test('past day entry can be edited from calendar and persists after reload', async ({ page }) => {
    await registerOwnerOnCalendar(page, 'calendar-past-edit');

    const todayISO = await todayISOFromCalendar(page);
    const pastISO = shiftISODate(todayISO, -2);
    const pastMonth = pastISO.slice(0, 7);

    const dayEditorForm = await openCalendarDayEditor(page, pastISO);

    await dayEditorForm.locator('input[name="is_period"]').check();
    await dayEditorForm.locator('input[name="flow"][value="medium"]').check({ force: true });

    const noteText = `calendar-note-${Date.now()}`;
    await openCalendarNotes(dayEditorForm);
    await dayEditorForm.locator('#calendar-notes').fill(noteText);
    await dayEditorForm.locator('button[data-save-button]').click();

    await page.goto(`/calendar?month=${pastMonth}&day=${pastISO}`);
    await expect(page).toHaveURL(new RegExp(`/calendar\\?month=${pastMonth}&day=${pastISO}`));
    await expect(page.locator('#day-editor')).toContainText(noteText);

    const editButton = page.locator(`[data-day-editor-open="${pastISO}"]`).first();
    await expect(editButton).toBeVisible();
    await editButton.click();
    await expect(page.locator(`[data-day-editor-form][data-day-editor-date="${pastISO}"] #calendar-notes`)).toHaveValue(noteText);
    await expect(page.locator(`[data-day-editor-form][data-day-editor-date="${pastISO}"] input[name="is_period"]`)).toBeChecked();
  });

  test('logged day renders data and sex markers in the calendar grid', async ({ page }) => {
    await registerOwnerOnCalendar(page, 'calendar-markers');

    const todayISO = await todayISOFromCalendar(page);
    const pastISO = shiftISODate(todayISO, -1);
    const pastMonth = pastISO.slice(0, 7);

    const dayEditorForm = await openCalendarDayEditor(page, pastISO);
    await openSexActivityDisclosure(dayEditorForm);
    await dayEditorForm.locator('[data-sex-activity-option="protected"]').click();
    await openCalendarNotes(dayEditorForm);
    await dayEditorForm.locator('#calendar-notes').fill(`calendar-marker-${Date.now()}`);
    await dayEditorForm.locator('button[data-save-button]').click();

    await page.goto(`/calendar?month=${pastMonth}&day=${pastISO}`);
    const dayButton = page.locator(`button[data-day="${pastISO}"]`);
    await expect(dayButton).toHaveAttribute('data-calendar-has-data', 'true');
    await expect(dayButton.locator('.calendar-data-marker')).toBeVisible();
    await expect(dayButton.locator('.calendar-sex-marker')).toBeVisible();
  });

  test('existing day entry can be deleted from calendar after confirmation', async ({ page }) => {
    await registerOwnerOnCalendar(page, 'calendar-delete-entry');

    const todayISO = await todayISOFromCalendar(page);
    const pastISO = shiftISODate(todayISO, -2);
    const pastMonth = pastISO.slice(0, 7);
    const noteText = `calendar-delete-${Date.now()}`;

    const dayEditorForm = await openCalendarDayEditor(page, pastISO);
    await dayEditorForm.locator('input[name="is_period"]').check();
    await openCalendarNotes(dayEditorForm);
    await dayEditorForm.locator('#calendar-notes').fill(noteText);
    await dayEditorForm.locator('button[data-save-button]').click();

    await page.goto(`/calendar?month=${pastMonth}&day=${pastISO}`);
    await expect(page.locator('#day-editor')).toContainText(noteText);

    await page.locator(`[data-day-editor-open="${pastISO}"]`).first().click();
    const deleteButton = page.locator(`[data-day-delete-form][data-day-delete-date="${pastISO}"] [data-day-delete-button]`);
    await expect(deleteButton).toBeVisible();
    await deleteButton.click();

    await expect(page.locator('#confirm-modal')).toBeVisible();
    await page.locator('#confirm-modal-accept').click();

    await expect(page.locator(`[data-day-editor-form][data-day-editor-date="${pastISO}"]`)).toHaveCount(0);
    await expect(page.locator(`[data-day-editor-open="${pastISO}"]`).first()).toBeVisible();
    await expect(page.locator('#day-editor')).not.toContainText(noteText);
  });

  test('future day panel shows future warning context', async ({ page }) => {
    await registerOwnerOnCalendar(page, 'calendar-future-day');

    const todayISO = await todayISOFromCalendar(page);
    const futureISO = shiftISODate(todayISO, 3);
    const futureMonth = futureISO.slice(0, 7);

    await page.goto(`/calendar?month=${futureMonth}&day=${futureISO}`);
    await expect(page).toHaveURL(new RegExp(`/calendar\\?month=${futureMonth}&day=${futureISO}`));

    const warningPanel = page.locator('#day-editor .journal-panel.text-sm').first();
    await expect(warningPanel).toBeVisible();
    await expect(warningPanel).not.toHaveText(/^$/);
    await expect(page.locator(`[data-day-editor-form][data-day-editor-date="${futureISO}"]`)).toHaveCount(0);
    await expect(page.locator(`[data-day-editor-open="${futureISO}"]`)).toBeVisible();
  });

  test('language route preserves selected month/day query and visible panel', async ({ page }) => {
    await registerOwnerOnCalendar(page, 'calendar-lang-query');

    const todayISO = await todayISOFromCalendar(page);
    const pastISO = shiftISODate(todayISO, -2);
    const pastMonth = pastISO.slice(0, 7);

    await page.goto(`/calendar?month=${pastMonth}&day=${pastISO}`);
    await expect(page.locator(`[data-day-editor-open="${pastISO}"]`)).toBeVisible();

    await page.goto(`/lang/ru?next=${encodeURIComponent(`/calendar?month=${pastMonth}&day=${pastISO}`)}`);
    await expect(page.locator('html')).toHaveAttribute('lang', 'ru');

    const currentURL = new URL(page.url());
    expect(currentURL.pathname).toBe('/calendar');
    expect(currentURL.searchParams.get('month')).toBe(pastMonth);
    expect(currentURL.searchParams.get('day')).toBe(pastISO);
    await expect(page.locator(`[data-day-editor-open="${pastISO}"]`)).toBeVisible();
  });

  test('manual cycle start button in calendar creates a period entry for that day', async ({ page }) => {
    await registerOwnerOnCalendar(page, 'calendar-manual-cycle-start');

    const todayISO = await todayISOFromCalendar(page);
    const pastISO = shiftISODate(todayISO, -3);
    const pastMonth = pastISO.slice(0, 7);

    await page.goto(`/calendar?month=${pastMonth}&day=${pastISO}`);
    await expect(page).toHaveURL(new RegExp(`/calendar\\?month=${pastMonth}&day=${pastISO}`));

    const manualStartButton = page.locator(`[data-day-cycle-start-form][data-day-cycle-start-date="${pastISO}"] [data-day-cycle-start-button]`);
    await expect(manualStartButton).toBeVisible();
    await Promise.all([
      page.waitForResponse((response) => {
        return (
          response.request().method() === 'POST' &&
          response.url().includes(`/api/days/${pastISO}/cycle-start?source=calendar`)
        );
      }),
      manualStartButton.click(),
    ]);

    const editButton = page.locator(`[data-day-editor-open="${pastISO}"]`).first();
    await expect(editButton).toBeVisible();
    await editButton.click();

    const dayEditorForm = page.locator(`[data-day-editor-form][data-day-editor-date="${pastISO}"]`);
    await expect(dayEditorForm).toBeVisible();
    await expect(dayEditorForm.locator('input[name="is_period"]')).toBeChecked();
  });

  test('tomorrow keeps manual cycle start available with a warning', async ({ page }) => {
    await registerOwnerOnCalendar(page, 'calendar-future-cycle-start');

    const todayISO = await todayISOFromCalendar(page);
    const tomorrowISO = shiftISODate(todayISO, 1);
    const month = tomorrowISO.slice(0, 7);

    await page.goto(`/calendar?month=${month}&day=${tomorrowISO}`);
    await expect(page).toHaveURL(new RegExp(`/calendar\\?month=${month}&day=${tomorrowISO}`));

    const manualStartButton = page.locator(`[data-day-cycle-start-form][data-day-cycle-start-date="${tomorrowISO}"] [data-day-cycle-start-button]`);
    await expect(manualStartButton).toBeVisible();
    await expect(page.locator('#day-editor')).toContainText(/recalculated|пересчитан|recalcular/i);

    await Promise.all([
      page.waitForResponse((response) => {
        return (
          response.request().method() === 'POST' &&
          response.url().includes(`/api/days/${tomorrowISO}/cycle-start?source=calendar`)
        );
      }),
      manualStartButton.click(),
    ]);

    await page.locator(`[data-day-editor-open="${tomorrowISO}"]`).first().click();
    const dayEditorForm = page.locator(`[data-day-editor-form][data-day-editor-date="${tomorrowISO}"]`);
    await expect(dayEditorForm).toBeVisible();
    await expect(dayEditorForm.locator('input[name="is_period"]')).toBeChecked();
  });
});
