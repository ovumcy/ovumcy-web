import { expect, test, type Page } from '@playwright/test';
import {
  continueFromRecoveryCode,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  readRecoveryCode,
  registerOwnerViaUI,
} from './support/auth-helpers';
import { dateFieldRoot, fillDateField } from './support/date-field-helpers';
import { dashboardNextPeriodText } from './support/dashboard-helpers';
import { setRequestTimezoneFromBrowser } from './support/timezone-helpers';
import { shiftISODate } from './support/stats-helpers';

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
  startDaysAgo: number
): Promise<void> {
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

async function markCycleStartViaAPI(page: Page, isoDate: string): Promise<void> {
  const csrf = (await page.locator('meta[name="csrf-token"]').getAttribute('content')) ?? '';
  const response = await page.request.post(`/api/v1/days/${isoDate}/cycle-start`, {
    headers: {
      'X-CSRF-Token': csrf,
      'Content-Type': 'application/x-www-form-urlencoded',
    },
    form: { replace_existing: 'true' },
  });
  expect(response.status(), `mark cycle start at ${isoDate}`).toBeLessThan(400);
}

test.describe('Dashboard prediction range', () => {
  // Regular (non-irregular) users with at least three completed cycles and
  // measurable variability now see a data-driven uncertainty range on the
  // dashboard, replacing the previous age-35+ widening that applied to the
  // cohort with the lowest within-individual variability per Gibson et al.,
  // npj Digital Medicine 2023 (Apple Women's Health Study).
  test('regular user with variable cycles sees a confidence range and the variability explainer', async ({
    page,
  }) => {
    // Onboarding's MinDate is the later of (Jan 1 of current year) and
    // (today - 60 days), so we seed at the 60-day boundary and use the
    // cycle-start API (which has no past-date limit) to backfill an older
    // cycle anchor and add the subsequent cycle starts.
    await registerAndOnboardWithStartDaysAgo(page, 'dashboard-prediction-range', 60);

    const today = isoDateDaysAgo(0);
    // Cycle starts: today-90, today-60 (onboarding anchor), today-35, today-5.
    // Completed cycle lengths: 30, 25, 30. Population StdDev ≈ 2.36 → span
    // = 2 days. Max-min spread = 5, below the IsIrregularCycleSpread
    // threshold of 7, so no irregularity notice fires.
    for (const offset of [-90, -35, -5]) {
      await markCycleStartViaAPI(page, shiftISODate(today, offset));
    }

    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);

    const nextPeriodText = await dashboardNextPeriodText(page);
    expect(nextPeriodText, 'regular user with variability should see a range').toMatch(
      /\w{3} \d{1,2}, \d{4} — \w{3} \d{1,2}, \d{4}/
    );
    expect(nextPeriodText).not.toContain('3 cycles are needed');

    await expect(page.locator('[data-dashboard-prediction-explainer]')).toContainText(
      'Your prediction shows a range that reflects how much your cycle length varies.'
    );
  });

});
