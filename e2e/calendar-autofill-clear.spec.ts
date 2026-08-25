import { expect, test, type Page } from './support/fixtures';
import {
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  readRecoveryCode,
  registerOwnerViaUI,
} from './support/auth-helpers';
import { ensureNotesFieldVisible } from './support/note-helpers';
import { openCalendarDayEditor, saveDayEditorForm } from './support/stats-helpers';
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

async function todayISOFromCalendar(page: Page): Promise<string> {
  const todayButton = page.locator('button[data-day]:has(.calendar-today-pill)').first();
  await expect(todayButton).toBeVisible();
  const todayISO = await todayButton.getAttribute('data-day');
  expect(todayISO).toMatch(/^\d{4}-\d{2}-\d{2}$/);
  return todayISO!;
}

test.describe('calendar auto-fill clear-on-toggle-off', () => {
  test('clears bare auto-filled neighbors when the anchor period day is toggled off', async ({ page }) => {
    await registerOwnerOnCalendar(page, 'calendar-autofill-clear');

    const todayISO = await todayISOFromCalendar(page);
    // Pick a date far from both lastPeriodStart=today-3 (set by completeOnboardingIfPresent)
    // and the predicted next period so the new period block has no interference.
    const anchorISO = shiftISODate(todayISO, -30);
    const neighborISOs = [1, 2, 3, 4].map((offset) => shiftISODate(anchorISO, offset));

    const onForm = await openCalendarDayEditor(page, anchorISO);
    await onForm.locator('input[name="is_period"]').check();
    await saveDayEditorForm(page, anchorISO, onForm);

    for (const neighborISO of neighborISOs) {
      const dayButton = page.locator(`button[data-day="${neighborISO}"]`);
      await expect(dayButton).toHaveAttribute('data-calendar-has-data', 'true');
    }

    const offForm = await openCalendarDayEditor(page, anchorISO);
    await expect(offForm.locator('input[name="is_period"]')).toBeChecked();
    await offForm.locator('input[name="is_period"]').uncheck();
    await saveDayEditorForm(page, anchorISO, offForm);

    for (const neighborISO of neighborISOs) {
      const dayButton = page.locator(`button[data-day="${neighborISO}"]`);
      await expect(dayButton).not.toHaveAttribute('data-calendar-has-data', 'true');
    }

    for (const neighborISO of neighborISOs) {
      const neighborForm = await openCalendarDayEditor(page, neighborISO);
      await expect(neighborForm.locator('input[name="is_period"]')).not.toBeChecked();
    }
  });

  test('preserves a manual annotation while clearing the rest of the auto-fill window', async ({ page }) => {
    await registerOwnerOnCalendar(page, 'calendar-autofill-preserve');

    const todayISO = await todayISOFromCalendar(page);
    const anchorISO = shiftISODate(todayISO, -30);
    const manualISO = shiftISODate(anchorISO, 2);
    const earlyNeighborISO = shiftISODate(anchorISO, 1);
    const lateNeighborISOs = [3, 4].map((offset) => shiftISODate(anchorISO, offset));

    const onForm = await openCalendarDayEditor(page, anchorISO);
    await onForm.locator('input[name="is_period"]').check();
    await saveDayEditorForm(page, anchorISO, onForm);

    const manualForm = await openCalendarDayEditor(page, manualISO);
    await expect(manualForm.locator('input[name="is_period"]')).toBeChecked();
    const manualNote = `autofill-preserve-${Date.now()}`;
    await ensureNotesFieldVisible(manualForm, '#calendar-notes');
    await manualForm.locator('#calendar-notes').fill(manualNote);
    await saveDayEditorForm(page, manualISO, manualForm);

    const offForm = await openCalendarDayEditor(page, anchorISO);
    await offForm.locator('input[name="is_period"]').uncheck();
    await saveDayEditorForm(page, anchorISO, offForm);

    const earlyButton = page.locator(`button[data-day="${earlyNeighborISO}"]`);
    await expect(earlyButton).not.toHaveAttribute('data-calendar-has-data', 'true');

    const manualButton = page.locator(`button[data-day="${manualISO}"]`);
    await expect(manualButton).toHaveAttribute('data-calendar-has-data', 'true');

    for (const lateISO of lateNeighborISOs) {
      const lateButton = page.locator(`button[data-day="${lateISO}"]`);
      await expect(lateButton).toHaveAttribute('data-calendar-has-data', 'true');
    }

    const preservedForm = await openCalendarDayEditor(page, manualISO);
    await expect(preservedForm.locator('input[name="is_period"]')).toBeChecked();
    await ensureNotesFieldVisible(preservedForm, '#calendar-notes');
    await expect(preservedForm.locator('#calendar-notes')).toHaveValue(manualNote);
  });
});
