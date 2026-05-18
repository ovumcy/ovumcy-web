import { test, expect, type Page } from '@playwright/test';
import {
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  readRecoveryCode,
  registerOwnerViaUI,
} from './support/auth-helpers';
import { dateFieldRoot, fillDateField } from './support/date-field-helpers';
import { setRequestTimezoneFromBrowser } from './support/timezone-helpers';
import { openCalendarDayEditor, shiftISODate, todayISOFromDashboard } from './support/stats-helpers';

async function registerAndOnboardDefault(page: Page, prefix: string): Promise<void> {
  const credentials = createCredentials(prefix);
  await registerOwnerViaUI(page, credentials);
  await expectInlineRegisterRecoveryStep(page);
  await readRecoveryCode(page);
  await continueFromRecoveryCode(page);
  await completeOnboardingIfPresent(page);
  await setRequestTimezoneFromBrowser(page);
}

function isoDateDaysAgo(days: number): string {
  const date = new Date();
  date.setHours(0, 0, 0, 0);
  date.setDate(date.getDate() - days);
  const yyyy = date.getFullYear();
  const mm = String(date.getMonth() + 1).padStart(2, '0');
  const dd = String(date.getDate()).padStart(2, '0');
  return `${yyyy}-${mm}-${dd}`;
}

async function registerAndOnboardWithStartDaysAgo(
  page: Page,
  prefix: string,
  startDaysAgo: number,
): Promise<void> {
  // completeOnboardingIfPresent hardcodes today-3 as the period start, which
  // makes today an auto-period-fill day. Spotting-warning + future-cycle-
  // start scenarios need today to sit outside the onboarding period cluster,
  // so we run a custom onboarding flow that submits an explicit older date.
  const credentials = createCredentials(prefix);
  await registerOwnerViaUI(page, credentials);
  await expectInlineRegisterRecoveryStep(page);
  await readRecoveryCode(page);
  await continueFromRecoveryCode(page);

  const startISO = isoDateDaysAgo(startDaysAgo);
  const startInput = page.locator('#last-period-start');
  await expect(dateFieldRoot(startInput)).toBeVisible();
  await fillDateField(startInput, startISO);
  await page.locator('form[hx-post="/api/v1/onboarding/steps/1"] button[type="submit"]').click();

  const stepTwoForm = page.locator('form[hx-post="/api/v1/onboarding/steps/2"]');
  await expect(stepTwoForm).toBeVisible();
  await Promise.all([
    page.waitForURL(/\/dashboard(?:\?.*)?$/, { timeout: 15000 }),
    stepTwoForm.locator('button[type="submit"]').click(),
  ]);

  await setRequestTimezoneFromBrowser(page);
}

async function csrfToken(page: Page): Promise<string> {
  return (await page.locator('meta[name="csrf-token"]').getAttribute('content')) ?? '';
}

test.describe('Dashboard: spotting cycle warning', () => {
  test('saving today as a period day with spotting flow surfaces the day-1 spotting tip on the dashboard', async ({
    page,
  }) => {
    // Anchor onboarding 30 days back so the auto-period-fill window
    // (today-30 .. today-26) sits well before today. currentPeriodStreak
    // AtDay walks backwards from today: with no period days adjacent, the
    // streak collapses to 1 and cycleStart = today, which is exactly what
    // shouldShowSpottingCycleWarning needs.
    await registerAndOnboardWithStartDaysAgo(page, 'dashboard-spotting-warning', 30);
    const today = await todayISOFromDashboard(page);

    const response = await page.request.put(`/api/v1/days/${today}`, {
      headers: {
        'X-CSRF-Token': await csrfToken(page),
        'Content-Type': 'application/json',
      },
      data: { is_period: true, flow: 'spotting' },
    });
    expect(response.status()).toBeLessThan(400);

    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);

    // The warning sits inside the [data-period-fields] fieldset right after
    // the flow chips — scope the locator so a generic copy match on
    // .journal-muted elsewhere on the page cannot mask a regression.
    const periodFields = page.locator('[data-period-fields]');
    await expect(periodFields).toBeVisible();
    await expect(periodFields).toContainText('Spotting may not be day 1. Check again tomorrow.');
  });
});

test.describe('Dashboard: period tip once', () => {
  test('toggling period on the dashboard reveals the once-only tip and persists the acknowledgement', async ({
    page,
  }) => {
    // Fresh users land with ShownPeriodTip=false: the dashboard renders the
    // hidden <p data-period-tip-copy> and body[data-period-tip-pending=true].
    // Toggling [data-period-toggle] ON wires through maybeAcknowledgePeriodTip
    // -> sets the data-period-tip-ack hidden input and reveals the copy.
    // The 2-second-debounced autosave POSTs ack_period_tip=true to flip
    // ShownPeriodTip server-side; on next render the {{if not .CurrentUser.
    // ShownPeriodTip}} guard removes the tip element entirely.
    await registerAndOnboardDefault(page, 'dashboard-period-tip-once');

    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);

    await expect(page.locator('body[data-period-tip-pending="true"]')).toBeVisible();
    const tipCopy = page.locator('[data-period-tip-copy]');
    await expect(tipCopy).toHaveCount(1);
    await expect(tipCopy).toBeHidden();

    // Toggle period ON inside the autosave-bound dashboard editor and wait
    // for the autosave PUT. The native input is wrapped by a styled
    // .period-toggle label; ordinary label clicks misbehave in this
    // configuration (the live .checked property flips back unchecked even
    // after the label fires its delegated handlers). Set the property and
    // dispatch change explicitly — that is the exact path the dashboard
    // quick-action button takes, so the same downstream listeners fire.
    const periodInput = page.locator('input[data-period-toggle]').first();
    const autosavePromise = page.waitForResponse((response) => {
      return (
        response.request().method() === 'PUT' &&
        /\/api\/v1\/days\/\d{4}-\d{2}-\d{2}$/.test(response.url()) &&
        response.status() < 400
      );
    });
    await periodInput.evaluate((node) => {
      if (node instanceof HTMLInputElement) {
        node.checked = true;
        node.dispatchEvent(new Event('change', { bubbles: true }));
      }
    });
    await expect(periodInput).toBeChecked();
    await expect(tipCopy).toBeVisible();
    await expect(tipCopy).toContainText('Day 1 is the first day of full flow, not spotting.');
    await autosavePromise;

    await page.reload();
    await expect(page.locator('[data-period-tip-copy]')).toHaveCount(0);
    await expect(page.locator('body[data-period-tip-pending="false"]')).toBeVisible();
  });
});

test.describe('Calendar: future cycle start notice', () => {
  test('opening the day editor for tomorrow shows the future cycle-start prediction notice', async ({
    page,
  }) => {
    // ShowFutureCycleStartNotice = isFutureDate && AllowManualCycleStart.
    // IsAllowedManualCycleStartDate caps the future window at today+2 days,
    // and AllowManualCycleStart is true for owners by default — so the day
    // editor for tomorrow flips the notice on.
    await registerAndOnboardDefault(page, 'calendar-future-cycle-start');
    const today = await todayISOFromDashboard(page);
    const tomorrow = shiftISODate(today, 1);

    const form = await openCalendarDayEditor(page, tomorrow);
    await expect(form).toContainText('Predictions will be recalculated when that day arrives.');
  });
});
