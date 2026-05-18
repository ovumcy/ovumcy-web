import { test, expect, type Page } from '@playwright/test';
import { dateFieldRoot, fillDateField } from './support/date-field-helpers';
import {
  continueFromRecoveryCode,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  readRecoveryCode,
  registerOwnerViaUI,
} from './support/auth-helpers';
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
): Promise<string> {
  const credentials = createCredentials(prefix);
  await registerOwnerViaUI(page, credentials);
  await expectInlineRegisterRecoveryStep(page);
  await readRecoveryCode(page);
  await continueFromRecoveryCode(page);

  // Replicate completeOnboardingIfPresent's UI flow but with a custom
  // start_date so the cycle window is wide enough for the BBT chart.
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
  return startISO;
}

async function csrfToken(page: Page): Promise<string> {
  return (await page.locator('meta[name="csrf-token"]').getAttribute('content')) ?? '';
}

async function saveDayBBT(page: Page, isoDate: string, bbt: number): Promise<void> {
  // Send JSON so buildUpsertDayEntryInput skips the "preserve hidden fields"
  // shortcut that drops BBT when the user has TrackBBT=false. JSON callers
  // are treated as programmatic clients and the payload is taken as-is.
  const response = await page.request.put(`/api/v1/days/${isoDate}`, {
    headers: {
      'X-CSRF-Token': await csrfToken(page),
      'Content-Type': 'application/json',
    },
    data: { bbt },
  });
  expect(response.status(), `save BBT on ${isoDate}`).toBeLessThan(400);
}

async function savePeriodDay(page: Page, isoDate: string): Promise<void> {
  const response = await page.request.put(`/api/v1/days/${isoDate}`, {
    headers: {
      'X-CSRF-Token': await csrfToken(page),
      'Content-Type': 'application/json',
    },
    data: { is_period: true, flow: 'medium' },
  });
  expect(response.status(), `save period on ${isoDate}`).toBeLessThan(400);
}

async function markCycleStartViaAPI(page: Page, isoDate: string): Promise<void> {
  // POST /api/v1/days/{date}/cycle-start sets IsPeriod=true AND CycleStart=true
  // on the day, then triggers auto-period-fill. Setting CycleStart explicitly
  // is what makes latestExplicitCycleStartBeforeOrOn pick the day up; a plain
  // is_period upsert without the explicit flag leaves stats.LastPeriodStart
  // anchored to user.LastPeriodStart from onboarding.
  const response = await page.request.post(`/api/v1/days/${isoDate}/cycle-start`, {
    headers: {
      'X-CSRF-Token': await csrfToken(page),
      'Content-Type': 'application/x-www-form-urlencoded',
    },
    form: { replace_existing: 'true' },
  });
  expect(response.status(), `mark cycle start at ${isoDate}`).toBeLessThan(400);
}

test.describe('Stats: BBT chart', () => {
  test('logging 5+ BBT values within the current cycle renders the BBT chart section', async ({
    page,
  }) => {
    // Two layered gates make this test non-trivial:
    //
    //   1. /stats hides every insight (the BBT section included) behind
    //      `HasInsights = completedCycleCount >= 2`, computed by
    //      CompletedCycleTrendLengths. So at least three cycle starts must
    //      exist before today.
    //   2. buildCurrentCycleBBTSeries requires >= 5 BBT points inside
    //      [cycleStart..today], so the current (third) cycle has to be
    //      old enough to fit five sample days.
    //
    // Onboard with start_date=today-60 (cycle 1), then seed period days at
    // today-30 (cycle 2 start) and today-7 (cycle 3 start, the current
    // cycle). Layer the BBT samples on today-5..today.
    await registerAndOnboardWithStartDaysAgo(page, 'stats-bbt-chart', 60);
    const today = isoDateDaysAgo(0);

    await savePeriodDay(page, shiftISODate(today, -30));
    await savePeriodDay(page, shiftISODate(today, -7));

    // Slight upward drift mimics a typical follicular -> luteal pattern.
    const bbtSeries = [36.2, 36.3, 36.35, 36.4, 36.55, 36.7];
    for (let offset = -5; offset <= 0; offset += 1) {
      await saveDayBBT(page, shiftISODate(today, offset), bbtSeries[offset + 5]);
    }

    // Sanity-check persistence before asserting the chart renders.
    for (let offset = -5; offset <= 0; offset += 1) {
      const isoDate = shiftISODate(today, offset);
      const response = await page.request.get(`/api/v1/days/${isoDate}`, {
        headers: { Accept: 'application/json' },
      });
      expect(response.status(), `GET ${isoDate}`).toBe(200);
      const body = await response.json();
      expect(body.BBT ?? body.bbt, `BBT on ${isoDate}`).toBeGreaterThan(35);
    }

    // /stats now shows the current-cycle BBT chart section. The whole
    // section is guarded by `{{if .HasCurrentCycleBBTChart}}`, so a visible
    // #stats-bbt-title is itself the gate-passed signal.
    await page.goto('/stats');
    await expect(page).toHaveURL(/\/stats$/);
    await expect(page.locator('#stats-bbt-title')).toBeVisible();

    const bbtChart = page.locator('#bbt-chart');
    await expect(bbtChart).toBeVisible();

    // The chart's data-chart attribute carries the JSON payload produced by
    // mapStatsBBTChartData (lowercase keys; baseline is present only when
    // chart.HasBaseline is true, no separate boolean).
    const chartData = await bbtChart.getAttribute('data-chart');
    expect(chartData).toBeTruthy();
    const parsed = JSON.parse(chartData ?? '');
    expect(Array.isArray(parsed.labels)).toBe(true);
    expect(parsed.labels.length).toBeGreaterThanOrEqual(5);
    expect(Array.isArray(parsed.values)).toBe(true);
    const numericValues = parsed.values.filter((v: number | null) => v !== null);
    expect(numericValues.length).toBeGreaterThanOrEqual(5);
    expect(typeof parsed.baseline).toBe('number');
    expect(parsed.baseline).toBeGreaterThan(35);
    expect(parsed.baseline).toBeLessThan(38);
  });

  test('a sustained BBT rise after the baseline window flags the probable ovulation marker', async ({
    page,
  }) => {
    // Same HasInsights gate as the chart test, but the current cycle starts
    // at today-12 so it can host eight consecutive BBT samples (cycle days
    // 6..13). markCycleStartViaAPI also sets CycleStart=true (not just
    // is_period) so latestExplicitCycleStartBeforeOrOn picks the day up and
    // stats.LastPeriodStart actually anchors to today-12 instead of remaining
    // on the onboarding date. Default period_length=5 means the auto-period
    // -fill range for the current cycle is cycle days 1..5 = today-12..
    // today-8, which sits entirely outside the today-7..today BBT window —
    // so the bare { bbt: ... } JSON payloads never wipe an is_period flag we
    // care about.
    await registerAndOnboardWithStartDaysAgo(page, 'stats-bbt-marker', 60);
    const today = isoDateDaysAgo(0);

    await markCycleStartViaAPI(page, shiftISODate(today, -30));
    await markCycleStartViaAPI(page, shiftISODate(today, -12));

    // BBT layout, offsets today-8..today-1 (8 entries, all strictly before
    // "today" so a TZ-induced today+1 shift on the server can't drop the
    // last sample via the `localDay.After(today)` filter):
    //   baseline window = first 5 recorded ≈ 36.26
    //   threshold = baseline + 0.2 = 36.46
    //   final 3 days = 36.55 / 36.60 / 36.65 -> three consecutive above threshold
    // detectProbableOvulationMarker iterates from recordedDays[5], so the
    // lower baseline window cannot trigger a false marker.
    const bbtSeries: Array<[number, number]> = [
      [-8, 36.2],
      [-7, 36.25],
      [-6, 36.3],
      [-5, 36.3],
      [-4, 36.25],
      [-3, 36.55],
      [-2, 36.6],
      [-1, 36.65],
    ];
    for (const [offset, value] of bbtSeries) {
      await saveDayBBT(page, shiftISODate(today, offset), value);
    }

    await page.goto('/stats');
    await expect(page).toHaveURL(/\/stats$/);
    await expect(page.locator('#stats-bbt-title')).toBeVisible();

    const chartData = await page.locator('#bbt-chart').getAttribute('data-chart');
    expect(chartData).toBeTruthy();
    const parsed = JSON.parse(chartData ?? '');
    // The exact maxDay (and therefore markerIndex) shifts by ±1 with TZ
    // boundary effects between the JS local date and the server's calendar
    // day for stats.LastPeriodStart. Instead of pinning the index, assert
    // the contract: marker is set, labelled correctly, and the three values
    // starting from markerIndex+1 are all above baseline + 0.2 (the
    // detectProbableOvulationMarker threshold).
    expect(parsed.markerLabel).toBe('Probable ovulation');
    expect(typeof parsed.markerIndex).toBe('number');
    expect(parsed.markerIndex).toBeGreaterThanOrEqual(0);

    const threshold = parsed.baseline + 0.2;
    const values = parsed.values as Array<number | null>;
    const riseValues = values.slice(parsed.markerIndex + 1, parsed.markerIndex + 4);
    expect(riseValues).toHaveLength(3);
    for (const v of riseValues) {
      expect(v).not.toBeNull();
      expect(v as number).toBeGreaterThanOrEqual(threshold);
    }
  });
});
