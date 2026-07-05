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

async function openCalendarDayEditor(page: Page, isoDate: string) {
  const month = isoDate.slice(0, 7);
  await page.goto(`/calendar?month=${month}&day=${isoDate}`);
  await expect(page).toHaveURL(new RegExp(`/calendar\\?month=${month}&day=${isoDate}`));

  const editButton = page.locator(`[data-day-editor-open="${isoDate}"]`).first();
  if (await editButton.count()) {
    await editButton.click();
  }

  const form = page.locator(`[data-day-editor-form][data-day-editor-date="${isoDate}"]`);
  await expect(form).toBeVisible();
  return form;
}

async function saveDayEditorForm(page: Page, isoDate: string, form: import('@playwright/test').Locator): Promise<void> {
  // Bind the wait to the request this click issues, not to any PUT response for
  // the date. waitForResponse would resolve on the first matching response to
  // arrive after registration — under CPU contention a still-in-flight earlier
  // PUT's response can land inside this window and satisfy the predicate before
  // the actual save lands. The `request` event only fires for requests issued
  // after registration, so this captures exactly the click's PUT; awaiting that
  // request's own response then blocks until this save has truly committed.
  const [request] = await Promise.all([
    page.waitForRequest(
      (candidate) =>
        candidate.method() === 'PUT' && candidate.url().includes(`/api/v1/days/${isoDate}`),
    ),
    form.locator('button[data-save-button]').click(),
  ]);
  const response = await request.response();
  expect(response, `expected a response for PUT /api/v1/days/${isoDate}`).not.toBeNull();
  expect(response!.ok(), `PUT /api/v1/days/${isoDate} failed with ${response!.status()}`).toBeTruthy();

  // The PUT response only means the write committed. Its htmx afterSwap then
  // fires `calendar-day-updated`, which reloads the whole calendar grid
  // (GET /calendar) and re-lazy-loads the day editor — a cascade that outlives
  // this click. If the caller navigates (openCalendarDayEditor → page.goto)
  // while that cascade is still hitting the server, the next page's own
  // hx-trigger="load" editor fetch competes with it and, under CPU contention,
  // can miss the 5s visibility window. Let the app go quiescent first so the
  // save is fully settled — not just committed — before returning.
  await page.waitForLoadState('networkidle');
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
