import { expect, test, type Page } from './support/fixtures';
import {
  apiOriginHeader,
  continueFromRecoveryCode,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  readRecoveryCode,
  registerOwnerViaUI,
} from './support/auth-helpers';
import { isoToday, shiftISODate } from './support/iso-date-helpers';
import { ensureNotesFieldVisible } from './support/note-helpers';
import { selectOnboardingStartDate } from './support/onboarding-helpers';
import { openCalendarDayEditor, saveDayEditorForm } from './support/stats-helpers';
import { setRequestTimezoneFromBrowser } from './support/timezone-helpers';

// The period is four days old and none of its days after the first has a row:
// onboarding with automatic period fill OFF records only the stored start, so the
// days between it and today are exactly the gap a later tick has to catch up on.
// The setting is armed afterwards, then today is ticked by hand.
const PERIOD_STARTED_DAYS_AGO = 4;

async function onboardWithAutoFillOffAndAnOldPeriodStart(page: Page, prefix: string): Promise<string> {
  await registerOwnerViaUI(page, createCredentials(prefix));
  await expectInlineRegisterRecoveryStep(page);
  await readRecoveryCode(page);
  await continueFromRecoveryCode(page);

  const startISO = shiftISODate(isoToday(), -PERIOD_STARTED_DAYS_AGO);
  await selectOnboardingStartDate(page, startISO);
  await page.locator('form[hx-post="/api/v1/onboarding/steps/1"] button[type="submit"]').click();

  const stepTwoForm = page.locator('form[hx-post="/api/v1/onboarding/steps/2"]');
  await expect(stepTwoForm).toBeVisible();
  // The precondition the spec stands on: nothing is filled while this is off.
  await expect(stepTwoForm.locator('input[name="auto_period_fill"]')).not.toBeChecked();
  await Promise.all([
    page.waitForURL(/\/dashboard(?:\?.*)?$/, { timeout: 15000 }),
    stepTwoForm.locator('[data-onboarding-step2-submit]').click(),
  ]);

  await setRequestTimezoneFromBrowser(page);
  return startISO;
}

async function armAutoPeriodFill(page: Page): Promise<void> {
  await page.goto('/calendar');
  await expect(page).toHaveURL(/\/calendar(?:\?.*)?$/);
  const csrf = (await page.locator('meta[name="csrf-token"]').getAttribute('content')) ?? '';

  const response = await page.request.patch('/api/v1/users/current/cycle', {
    headers: { ...apiOriginHeader(page), 'X-CSRF-Token': csrf, 'Content-Type': 'application/json' },
    data: {
      cycle_length: 28,
      period_length: 5,
      auto_period_fill: true,
      irregular_cycle: false,
      unpredictable_cycle: false,
      age_group: '',
    },
  });
  expect(response.status(), 'patch auto_period_fill=true').toBeLessThan(400);
}

async function tickPeriodOnDay(page: Page, isoDate: string): Promise<void> {
  const form = await openCalendarDayEditor(page, isoDate);
  await form.locator('input[name="is_period"]').check();
  await saveDayEditorForm(page, isoDate, form);
}

async function expectPeriodTick(page: Page, isoDate: string, ticked: boolean): Promise<void> {
  const form = await openCalendarDayEditor(page, isoDate);
  await expect(form.locator('input[name="is_period"]')).toBeChecked({ checked: ticked });
}

test.describe('calendar auto-fill catch-up inside the running period', () => {
  test('ticking today fills the empty days between the period start and today', async ({ page }) => {
    const startISO = await onboardWithAutoFillOffAndAnOldPeriodStart(page, 'autofill-topup-gap');
    const todayISO = isoToday();
    const gapISOs = [1, 2, 3].map((offset) => shiftISODate(startISO, offset));

    // Off: the days between the stored start and today are bare.
    for (const gapISO of gapISOs) {
      await expectPeriodTick(page, gapISO, false);
    }

    await armAutoPeriodFill(page);
    await tickPeriodOnDay(page, todayISO);

    for (const gapISO of gapISOs) {
      await expectPeriodTick(page, gapISO, true);
    }
    await expectPeriodTick(page, todayISO, true);

    // Un-ticking today takes the days its tick filled with it.
    const todayForm = await openCalendarDayEditor(page, todayISO);
    await todayForm.locator('input[name="is_period"]').uncheck();
    await saveDayEditorForm(page, todayISO, todayForm);

    for (const gapISO of gapISOs) {
      await expectPeriodTick(page, gapISO, false);
    }
    await expectPeriodTick(page, todayISO, false);
  });

  test('a day the owner left un-ticked inside the gap is not refilled', async ({ page }) => {
    const startISO = await onboardWithAutoFillOffAndAnOldPeriodStart(page, 'autofill-topup-unticked');
    const todayISO = isoToday();
    const [firstGapISO, keptISO, lastGapISO] = [1, 2, 3].map((offset) => shiftISODate(startISO, offset));

    // A notes-only save leaves a row with the period un-ticked on the middle day.
    const keptNote = `topup-kept-${Date.now()}`;
    const keptForm = await openCalendarDayEditor(page, keptISO);
    await ensureNotesFieldVisible(keptForm, '#calendar-notes');
    await keptForm.locator('#calendar-notes').fill(keptNote);
    await saveDayEditorForm(page, keptISO, keptForm);

    await armAutoPeriodFill(page);
    await tickPeriodOnDay(page, todayISO);

    await expectPeriodTick(page, firstGapISO, true);
    await expectPeriodTick(page, lastGapISO, true);

    const preservedForm = await openCalendarDayEditor(page, keptISO);
    await expect(preservedForm.locator('input[name="is_period"]')).not.toBeChecked();
    await ensureNotesFieldVisible(preservedForm, '#calendar-notes');
    await expect(preservedForm.locator('#calendar-notes')).toHaveValue(keptNote);
  });
});
