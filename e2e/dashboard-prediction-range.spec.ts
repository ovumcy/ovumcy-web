import { expect, test } from './support/fixtures';
import { displayDatesIn } from './support/date-field-helpers';
import { dashboardNextPeriodText } from './support/dashboard-helpers';
import { localeText } from './support/locale-helpers';
import { setRequestTimezoneFromBrowser } from './support/timezone-helpers';
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
  // cohort with the lowest within-individual variability per Li H. et al.
  // (senior author Gibson EA), npj Digital Medicine 2023 (Apple Women's Health
  // Study).
  test('regular user with variable cycles sees a confidence range and no explainer', async ({
    page,
  }) => {
    // Onboarding's MinDate is today - 60 days on every calendar date, so we
    // seed at the 60-day boundary and use the cycle-start API (which has no
    // past-date limit) to backfill an older cycle anchor and add the
    // subsequent cycle starts.
    // One constant feeds both the registration helper and the derivation below,
    // so the anchor offset cannot drift from where onboarding really put it.
    const onboardingStartDaysAgo = 60;
    await registerAndOnboardWithStartDaysAgo(page, 'dashboard-prediction-range', onboardingStartDaysAgo);

    const today = isoToday();
    // Cycle starts: today-90, today-60 (onboarding anchor), today-35, today-5.
    // Completed cycle lengths: 30, 25, 30. Population StdDev ≈ 2.36 → span
    // = 2 days. Max-min spread = 5, below the IsIrregularCycleSpread
    // threshold of 7, so no irregularity notice fires.
    const onboardingAnchorOffset = -onboardingStartDaysAgo;
    const cycleStartOffsets = [-90, onboardingAnchorOffset, -35, -5];
    for (const offset of cycleStartOffsets.filter((value) => value !== onboardingAnchorOffset)) {
      await markCycleStartViaAPI(page, shiftISODate(today, offset));
    }
    // The projected start is the last start plus the median completed cycle
    // length (dates are median-first); derive it from the seeded starts so the
    // assertion below follows the fixture instead of a typed day count.
    const completedLengths = cycleStartOffsets
      .slice(1)
      .map((offset, index) => offset - cycleStartOffsets[index])
      .sort((a, b) => a - b);
    const medianLength = completedLengths[Math.floor(completedLengths.length / 2)];
    const daysToProjectedStart = cycleStartOffsets[cycleStartOffsets.length - 1] + medianLength;

    // The page reads "today" from the request timezone; pin it to the browser's
    // so the rendered dates and the dates computed here share one calendar day.
    await setRequestTimezoneFromBrowser(page);
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
    const projectedStart = shiftISODate(today, daysToProjectedStart);
    expect(renderedDates[0] > today).toBeTruthy();
    expect(renderedDates[1] > renderedDates[0]).toBeTruthy();
    // The window must contain the day the fixture projects, so two ordered
    // dates drawn from the wrong anchor no longer pass. The bound holds by
    // construction of the symmetric ±span window around the projected start
    // (dashboard_cycle.go), so it checks the anchor date, not the window width.
    expect(
      renderedDates[0] <= projectedStart && projectedStart <= renderedDates[1],
      `window ${renderedDates[0]}..${renderedDates[1]} must contain ${projectedStart}`,
    ).toBeTruthy();

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
