import { expect, test } from './support/fixtures';
import { displayDatesIn } from './support/date-field-helpers';
import { dashboardNextPeriodText } from './support/dashboard-helpers';
import { localeText } from './support/locale-helpers';
import {
  isoToday,
  markCycleStartViaAPI,
  registerAndOnboardWithStartDaysAgo,
  shiftISODate,
} from './support/stats-helpers';

test.describe('Dashboard prediction range', () => {
  // Regular (non-irregular) users with at least three completed cycles and
  // measurable variability now see a data-driven uncertainty range on the
  // dashboard, replacing the previous age-35+ widening that applied to the
  // cohort with the lowest within-individual variability per Gibson et al.,
  // npj Digital Medicine 2023 (Apple Women's Health Study).
  test('regular user with variable cycles sees a confidence range and no explainer', async ({
    page,
  }) => {
    // Onboarding's MinDate is today - 60 days on every calendar date, so we
    // seed at the 60-day boundary and use the cycle-start API (which has no
    // past-date limit) to backfill an older cycle anchor and add the
    // subsequent cycle starts.
    await registerAndOnboardWithStartDaysAgo(page, 'dashboard-prediction-range', 60);

    const today = isoToday();
    // Cycle starts: today-90, today-60 (onboarding anchor), today-35, today-5.
    // Completed cycle lengths: 30, 25, 30. Population StdDev ≈ 2.36 → span
    // = 2 days. Max-min spread = 5, below the IsIrregularCycleSpread
    // threshold of 7, so no irregularity notice fires.
    for (const offset of [-90, -35, -5]) {
      await markCycleStartViaAPI(page, shiftISODate(today, offset));
    }

    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);

    // Two real calendar dates, in order — derived through Intl from the app's
    // own display format rather than matched against an EN-US shape regex,
    // which passed just as happily on "Foo 99, 0000". The exact range width is
    // the prediction service's business (unit-tested there), so this asserts
    // the surface contract: a range, forward in time, starting after today.
    const nextPeriodText = await dashboardNextPeriodText(page);
    const renderedDates = await displayDatesIn(page, nextPeriodText);
    expect(renderedDates, `regular user with variability should see a range: ${nextPeriodText}`)
      .toHaveLength(2);
    expect(renderedDates[0] > today).toBeTruthy();
    expect(renderedDates[1] > renderedDates[0]).toBeTruthy();

    // The range names the quantity it shows, so a regular owner gets no
    // explainer sentence under it. Absence of the block is the state: the
    // explainer element is single-valued, so a count of zero also proves the
    // sparse and irregular explainers are not the ones that rendered.
    await expect(page.locator('[data-dashboard-prediction-explainer]')).toHaveCount(0);

    // ...and the range says which quantity it is, in the catalogue's own words
    // rather than a re-typed phrase.
    // The literal tail after the last date placeholder — "(start window)" in
    // English, its own wording in every other locale.
    const startWindowLabel = localeText('en', 'dashboard.next_period_start_window')
      .split('%s')
      .pop()!
      .trim();
    expect(startWindowLabel.length, 'start-window key must carry a distinguishing tail').toBeGreaterThan(0);
    expect(nextPeriodText).toContain(startWindowLabel);
  });

});
