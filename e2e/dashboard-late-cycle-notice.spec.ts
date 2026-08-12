import { expect, test, type Page } from '@playwright/test';
import { saveSettingsLanguage } from './support/language-helpers';
import { localeText, localeTextByLocale, type Locale } from './support/locale-helpers';
import {
  isoToday,
  markCycleStartViaAPI,
  registerAndOnboardWithStartDaysAgo,
  shiftISODate,
} from './support/stats-helpers';

/**
 * The three late-cycle notice states, rendered.
 *
 * `services.BuildLateCycleNotice` picks one of three keys and a tone;
 * `late_cycle_policy_test.go` covers that choice against synthesized stats. What
 * no test covered until this spec is the other half of the contract: that a
 * dashboard driven only through the product's own surfaces reaches each state,
 * and that the copy it renders is the catalogue's — including the CLDR plural
 * variant the count selects, which the template resolves through `tn` and which
 * a Go unit test on the policy struct never sees.
 *
 * Every expected string comes from `internal/i18n/locales/*.json` via
 * `locale-helpers`; the only formatting done here is substituting the counts
 * into the pattern's `%d` verbs, the way the template's `printf` does.
 *
 * The notice is addressed exclusively through the hooks the policy owns —
 * `[data-dashboard-cycle-warnings]`, `[data-dashboard-cycle-day-warning]`,
 * `data-late-cycle-key`, `data-late-cycle-tone` — and never through the status
 * header above it, so a redesign of the header (#429) cannot move this spec. The
 * one deliberate exception is the beyond-range test, which also reads the
 * header's next-period slot: the same threshold that raises this notice withholds
 * the projected window, and "the notice appeared" is worth little if a confident
 * date is still sitting one line above it.
 */
const BEYOND_RANGE_KEY = 'dashboard.late_cycle.beyond_range';
const WITHIN_RANGE_KEY = 'dashboard.late_cycle.within_range';
const NO_PERSONAL_RANGE_KEY = 'dashboard.late_cycle.no_personal_range';
const ESTIMATE_PAUSED_KEY = 'dashboard.next_period_estimate_paused';

/** Whole calendar days from `fromISO` to `toISO`, DST-proof (UTC arithmetic). */
function isoDaysBetween(fromISO: string, toISO: string): number {
  const [fromYear, fromMonth, fromDay] = fromISO.split('-').map((part) => Number(part));
  const [toYear, toMonth, toDay] = toISO.split('-').map((part) => Number(part));
  const millisPerDay = 24 * 60 * 60 * 1000;
  return Math.round(
    (Date.UTC(toYear, toMonth - 1, toDay) - Date.UTC(fromYear, fromMonth - 1, fromDay)) /
      millisPerDay
  );
}

/**
 * The cycle day the dashboard is showing for a baseline anchored at `startISO`,
 * measured against the clock at call time rather than at seeding time. Reading
 * it from the rendered header would bind this spec to a surface it is not
 * testing, and freezing it at seeding time would break a run that crosses
 * midnight.
 */
function cycleDayFor(startISO: string): number {
  return isoDaysBetween(startISO, isoToday()) + 1;
}

/**
 * The catalogue key of the plural variant `count` selects.
 *
 * `i18n.PluralCategory` implements the CLDR integer rules (one/few/many for
 * Russian, one/other elsewhere), which is what `Intl.PluralRules` returns for
 * integers too — and `localeText` throws on a key the catalogue does not carry,
 * so a category that disagreed with the shipped variants fails loudly here
 * instead of degrading into a blank expectation.
 */
function pluralVariantKey(locale: Locale, baseKey: string, count: number): string {
  return `${baseKey}.${new Intl.PluralRules(locale).select(count)}`;
}

/** The template's `printf` over a pattern's `%d` verbs, in order. */
function formatCounts(pattern: string, counts: number[]): string {
  let index = 0;
  return pattern.replace(/%d/g, () => String(counts[index++]));
}

function lateCycleNotice(page: Page) {
  return page.locator('[data-dashboard-cycle-warnings] [data-dashboard-cycle-day-warning]');
}

test.describe('Dashboard late-cycle notice', () => {
  test('a cycle past the recorded range names the measured excess in days', async ({ page }) => {
    // Cycle starts today-116, today-88, today-60 (the onboarding anchor): two
    // completed cycles of 28 days each, which is both the minimum for a personal
    // range and the range itself (min = max = 28). The running cycle is on day 61
    // — past the 28-day reference plus a week, and past the recorded maximum — so
    // the policy reports the excess with the amber tone.
    const anchorISO = await registerAndOnboardWithStartDaysAgo(page, 'late-cycle-beyond', 60);
    for (const offset of [-56, -28]) {
      await markCycleStartViaAPI(page, shiftISODate(anchorISO, offset));
    }

    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);

    const notice = lateCycleNotice(page);
    await expect(notice).toBeVisible();
    await expect(notice).toHaveAttribute('data-late-cycle-key', BEYOND_RANGE_KEY);
    await expect(notice).toHaveAttribute('data-late-cycle-tone', 'warning');

    const excessDays = cycleDayFor(anchorISO) - 28;
    expect(excessDays, 'the seeded baseline must run past the recorded 28-day maximum')
      .toBeGreaterThan(0);
    await expect(notice).toHaveText(
      formatCounts(localeText('en', pluralVariantKey('en', BEYOND_RANGE_KEY, excessDays)), [
        excessDays,
      ])
    );

    // The other half of the same threshold: no next-period window is rendered at
    // all. The projection would happily roll forward to the anchor plus another
    // whole cycle, so the assertion is the slot's absence, not its contents.
    await expect(page.locator('[data-dashboard-next-period]')).toHaveCount(0);
    const pausedEstimate = page.locator('[data-dashboard-next-period-paused]');
    await expect(pausedEstimate).toBeVisible();
    await expect(pausedEstimate).toHaveText(localeText('en', ESTIMATE_PAUSED_KEY));
  });

  test('a long cycle still inside the recorded range names both bounds', async ({ page }) => {
    // Cycle starts today-113, today-88, today-43 (the onboarding anchor):
    // completed cycles of 25 and 45 days, so the average reference is 35 and the
    // recorded range is 25..45. The running cycle is on day 44 — past 35 + 7, so
    // the notice fires, but not past the 45-day maximum, so it reassures rather
    // than warns. The rendered bounds are seeded facts, independent of the clock.
    const anchorISO = await registerAndOnboardWithStartDaysAgo(page, 'late-cycle-within', 43);
    for (const offset of [-70, -45]) {
      await markCycleStartViaAPI(page, shiftISODate(anchorISO, offset));
    }

    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);

    const notice = lateCycleNotice(page);
    await expect(notice).toBeVisible();
    await expect(notice).toHaveAttribute('data-late-cycle-key', WITHIN_RANGE_KEY);
    await expect(notice).toHaveAttribute('data-late-cycle-tone', 'neutral');

    // The plural variant of this key is selected by the range's UPPER bound (the
    // «до 45 дней» rule), not by the cycle day.
    await expect(notice).toHaveText(
      formatCounts(localeText('en', pluralVariantKey('en', WITHIN_RANGE_KEY, 45)), [25, 45])
    );
  });

  test('an account with no completed cycles is told only the cycle day, in its own language', async ({
    page,
  }) => {
    // The onboarding anchor is the only cycle start, so there is no completed
    // cycle and no personal range to compare against. The cycle is long against
    // the configured cycle length (day 61 versus 28 + 7), so the notice is
    // visible — and says nothing about a range, which is the state the policy
    // exists to protect.
    const anchorISO = await registerAndOnboardWithStartDaysAgo(page, 'late-cycle-no-range', 60);
    expect(cycleDayFor(anchorISO), 'the seeded baseline must read as a long cycle')
      .toBeGreaterThan(35);

    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);

    const notice = lateCycleNotice(page);
    await expect(notice).toBeVisible();
    await expect(notice).toHaveAttribute('data-late-cycle-key', NO_PERSONAL_RANGE_KEY);
    await expect(notice).toHaveAttribute('data-late-cycle-tone', 'neutral');

    // This key takes no count, so its value is one string per locale: resolve the
    // whole table, assert the English entry, switch the interface language, and
    // assert the Russian one. Resolving the table rather than two entries also
    // means a locale that ships without this key fails here instead of silently.
    const byLocale = localeTextByLocale(NO_PERSONAL_RANGE_KEY);
    await expect(notice).toHaveText(byLocale.en);

    await page.goto('/settings');
    await expect(page).toHaveURL(/\/settings$/);
    await saveSettingsLanguage(page, 'ru');

    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);
    await expect(page.locator('html')).toHaveAttribute('lang', 'ru');
    const localizedNotice = lateCycleNotice(page);
    await expect(localizedNotice).toHaveAttribute('data-late-cycle-key', NO_PERSONAL_RANGE_KEY);
    await expect(localizedNotice).toHaveText(byLocale.ru);
  });
});
