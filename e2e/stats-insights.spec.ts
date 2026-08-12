import { test, expect, type Page } from '@playwright/test';
import { apiOriginHeader } from './support/auth-helpers';
import {
  isoToday,
  markCycleStartViaAPI,
  registerAndOnboardWithStartDaysAgo,
  shiftISODate,
} from './support/stats-helpers';
import { localeText } from './support/locale-helpers';

async function csrfToken(page: Page): Promise<string> {
  return (await page.locator('meta[name="csrf-token"]').getAttribute('content')) ?? '';
}

async function saveDayBBT(page: Page, isoDate: string, bbt: number): Promise<void> {
  // Send JSON so buildUpsertDayEntryInput skips the "preserve hidden fields"
  // shortcut that drops BBT when the user has TrackBBT=false. JSON callers
  // are treated as programmatic clients and the payload is taken as-is.
  const response = await page.request.put(`/api/v1/days/${isoDate}`, {
    headers: {
      ...apiOriginHeader(page),
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
      ...apiOriginHeader(page),
      'X-CSRF-Token': await csrfToken(page),
      'Content-Type': 'application/json',
    },
    data: { is_period: true, flow: 'medium' },
  });
  expect(response.status(), `save period on ${isoDate}`).toBeLessThan(400);
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
    const today = isoToday();

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
        headers: { Accept: 'application/json', ...apiOriginHeader(page) },
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
    // chart.HasBaseline is true, no separate boolean). Under the "3-over-6"
    // coverline rule a detected shift needs 6 preceding readings plus a
    // 3-day rise — this 6-sample drift cannot qualify, so the chart renders
    // WITHOUT a coverline: the section itself is gated only on >= 5 values.
    const chartData = await bbtChart.getAttribute('data-chart');
    expect(chartData).toBeTruthy();
    const parsed = JSON.parse(chartData ?? '');
    expect(Array.isArray(parsed.labels)).toBe(true);
    expect(parsed.labels.length).toBeGreaterThanOrEqual(5);
    expect(Array.isArray(parsed.values)).toBe(true);
    const numericValues = parsed.values.filter((v: number | null) => v !== null);
    expect(numericValues.length).toBeGreaterThanOrEqual(5);
    expect(parsed.baseline).toBeUndefined();
    expect(parsed.markerIndex).toBeUndefined();
  });

  test('a sustained BBT rise after the baseline window flags the probable ovulation marker', async ({
    page,
  }) => {
    // Same HasInsights gate as the chart test, but the current cycle starts
    // at today-14 so it can host nine consecutive BBT samples (cycle days
    // 6..14). markCycleStartViaAPI also sets CycleStart=true (not just
    // is_period) so latestExplicitCycleStartBeforeOrOn picks the day up and
    // stats.LastPeriodStart actually anchors to today-14 instead of remaining
    // on the onboarding date. Default period_length=5 means the auto-period
    // -fill range for the current cycle is cycle days 1..5 = today-14..
    // today-10, which sits entirely outside the today-9..today-1 BBT window —
    // so the bare { bbt: ... } JSON payloads never wipe an is_period flag we
    // care about.
    await registerAndOnboardWithStartDaysAgo(page, 'stats-bbt-marker', 60);
    const today = isoToday();

    await markCycleStartViaAPI(page, shiftISODate(today, -30));
    await markCycleStartViaAPI(page, shiftISODate(today, -14));

    // BBT layout, offsets today-9..today-1 (9 entries, all strictly before
    // "today" so a TZ-induced today+1 shift on the server can't drop the
    // last sample via the `localDay.After(today)` filter). "3-over-6" rule:
    //   coverline = max of the 6 readings preceding the rise = 36.30
    //   final 3 calendar-consecutive days = 36.55 / 36.60 / 36.65 -> all
    //   strictly above the coverline, third >= coverline + 0.2 (36.50)
    const bbtSeries: Array<[number, number]> = [
      [-9, 36.2],
      [-8, 36.25],
      [-7, 36.3],
      [-6, 36.3],
      [-5, 36.25],
      [-4, 36.28],
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
    // the contract: marker is set, labelled correctly, the drawn baseline is
    // the detected coverline, and the three values starting from
    // markerIndex+1 (the day after the marker = first elevated day) are all
    // strictly above it, the third by at least 0.2 °C.
    // Assert the marker's identity through its catalogue key, not the rendered
    // sentence: the copy is owned by the locale files and changes with them,
    // while the key is the stable half of the pair the payload now carries.
    expect(parsed.markerLabelKey).toBe('stats.bbt_probable_ovulation');
    expect(parsed.markerLabel).toBe(localeText('en', 'stats.bbt_probable_ovulation'));
    expect(typeof parsed.markerIndex).toBe('number');
    expect(parsed.markerIndex).toBeGreaterThanOrEqual(0);
    expect(typeof parsed.baseline).toBe('number');

    const coverline = parsed.baseline as number;
    const values = parsed.values as Array<number | null>;
    const riseValues = values.slice(parsed.markerIndex + 1, parsed.markerIndex + 4);
    expect(riseValues).toHaveLength(3);
    for (const v of riseValues) {
      expect(v).not.toBeNull();
      expect(v as number).toBeGreaterThan(coverline);
    }
    expect(riseValues[2] as number).toBeGreaterThanOrEqual(coverline + 0.2);
  });
});

test.describe('Stats: symptom patterns', () => {
  test('symptom logged across three completed cycles surfaces a pattern card with cycle-day copy', async ({
    page,
  }) => {
    // buildSymptomPatternInsights requires >= minimumPhaseInsightCycles (3)
    // completed cycles. OnboardingDateBounds caps last_period_start at
    // max(Jan 1 of current year, today-60), so the deepest onboarding can
    // anchor is today-60. Lay three more cycle starts with 18-day gaps:
    // ResolveManualCycleStartPolicy floors gapDays through int truncation
    // after a TZ-aware diff, so a nominal 15-day gap can collapse to 14
    // and trip the short-gap confirmation requirement. 18 days leaves
    // headroom regardless of the boundary direction.
    await registerAndOnboardWithStartDaysAgo(page, 'stats-symptom-pattern', 60);
    const today = isoToday();

    await markCycleStartViaAPI(page, shiftISODate(today, -42));
    await markCycleStartViaAPI(page, shiftISODate(today, -24));
    await markCycleStartViaAPI(page, shiftISODate(today, -6));

    // The dashboard renders the user's pre-seeded symptom catalogue as
    // <input name="symptom_ids" value="..."> checkboxes; pick the first one
    // to log across cycles.
    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);
    const firstSymptomInput = page
      .locator('fieldset[data-dashboard-section="symptoms"] input[name="symptom_ids"]')
      .first();
    await expect(firstSymptomInput).toBeAttached();
    const symptomIDRaw = await firstSymptomInput.getAttribute('value');
    expect(symptomIDRaw).toMatch(/^\d+$/);
    const symptomID = Number(symptomIDRaw);

    // Log the same symptom on cycle day 10 of each completed cycle:
    //   cycle 1: today-60 .. today-43 -> day 10 = today-51
    //   cycle 2: today-42 .. today-25 -> day 10 = today-33
    //   cycle 3: today-24 .. today-7  -> day 10 = today-15
    // Day 10 sits past the default 5-day auto-period-fill window, so the
    // JSON PUT (which defaults IsPeriod=false) never wipes a flag we care
    // about.
    const csrf = await csrfToken(page);
    for (const offset of [-51, -33, -15]) {
      const response = await page.request.put(`/api/v1/days/${shiftISODate(today, offset)}`, {
        headers: {
          ...apiOriginHeader(page),
          'X-CSRF-Token': csrf,
          'Content-Type': 'application/json',
        },
        data: { symptom_ids: [symptomID] },
      });
      expect(response.status(), `save symptom at offset ${offset}`).toBeLessThan(400);
    }

    await page.goto('/stats');
    await expect(page).toHaveURL(/\/stats$/);

    // HasSymptomPatterns gate -> the section renders, and at least one card
    // reports the cycle-day window it detected. Read the window off the card's
    // own attributes instead of parsing "Usually on day N of the cycle" out of
    // the rendered sentence: the copy is localized (and pluralized), so the old
    // filter only worked because the run happens to be in English, and a card
    // whose day numbers were wrong still matched.
    const patternsSection = page.locator('[data-stats-symptom-patterns]');
    await expect(patternsSection).toBeVisible();
    await expect(patternsSection.locator('[data-stats-symptom-patterns-heading]')).toHaveAttribute(
      'data-heading-key',
      'stats.symptom_patterns_title'
    );

    const patternCard = patternsSection.locator('[data-symptom-pattern-card]').first();
    await expect(patternCard).toBeVisible();
    const dayStart = Number(await patternCard.getAttribute('data-symptom-pattern-day-start'));
    const dayEnd = Number(await patternCard.getAttribute('data-symptom-pattern-day-end'));
    expect(dayStart).toBeGreaterThan(0);
    expect(dayEnd).toBeGreaterThanOrEqual(dayStart);
    // The symptom was logged on cycle day 10 of each completed cycle; a TZ
    // boundary can shift the derived day by one either way.
    expect(dayStart).toBeGreaterThanOrEqual(9);
    expect(dayEnd).toBeLessThanOrEqual(11);
    // One rendered-copy assertion for this surface: the card really does print
    // the day window, not just carry it in an attribute.
    await expect(patternCard).toContainText(String(dayStart));
  });
});

test.describe('Stats: cycle range', () => {
  test('two completed cycles of different lengths populate the cycle range stat card', async ({
    page,
  }) => {
    // Two cycle starts after onboarding -> two completed cycles of distinct
    // lengths (20 and 25 days nominally). populateObservedCycleStats fills
    // MinCycleLength / MaxCycleLength from cycleLengths(observedStarts), and
    // the Range card prints stats.cycle_range_summary when MinCycleLength>0.
    await registerAndOnboardWithStartDaysAgo(page, 'stats-cycle-range', 60);
    const today = isoToday();

    await markCycleStartViaAPI(page, shiftISODate(today, -40));
    await markCycleStartViaAPI(page, shiftISODate(today, -15));

    await page.goto('/stats');
    await expect(page).toHaveURL(/\/stats$/);

    // Address the card by its hook and read the observed lengths off its own
    // attributes. Matching `.stat-label` on the literal "Range" and then
    // parsing "Your cycles: N to M days" out of the value made this test an
    // English-only test of a localized, pluralized sentence.
    // Avoid pinning the exact numbers: a TZ-induced ±1 boundary shift on
    // the seeded starts can shift the observed cycle lengths by one, and
    // the card behaviour we want to lock in is "renders with two distinct
    // positive integers", not "renders with literal 20 and 25".
    const rangeArticle = page.locator('[data-stat-card="cycle-range"]');
    await expect(rangeArticle).toBeVisible();
    const minLen = Number(await rangeArticle.getAttribute('data-cycle-range-min'));
    const maxLen = Number(await rangeArticle.getAttribute('data-cycle-range-max'));
    expect(minLen).toBeGreaterThan(0);
    expect(maxLen).toBeGreaterThan(minLen);
    // One rendered-copy assertion: the populated card prints both bounds rather
    // than only carrying them in attributes.
    const valueText = (await rangeArticle.locator('.stat-value').textContent()) ?? '';
    expect(valueText, `range card value: ${valueText}`).toContain(String(minLen));
    expect(valueText, `range card value: ${valueText}`).toContain(String(maxLen));
  });
});
