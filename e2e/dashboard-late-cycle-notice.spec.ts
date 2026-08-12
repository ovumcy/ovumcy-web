import { expect, test, type Page } from '@playwright/test';
import {
  apiOriginHeader,
  continueFromRecoveryCode,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  readRecoveryCode,
  registerOwnerViaUI,
} from './support/auth-helpers';
import { saveSettingsLanguage } from './support/language-helpers';
import { localeText, localeTextByLocale, type Locale } from './support/locale-helpers';
import { selectOnboardingStartDate } from './support/onboarding-helpers';
import { shiftISODate } from './support/stats-helpers';
import { setRequestTimezoneFromBrowser } from './support/timezone-helpers';

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
 * header above it, so a redesign of the header (#429) cannot move this spec.
 */
const BEYOND_RANGE_KEY = 'dashboard.late_cycle.beyond_range';
const WITHIN_RANGE_KEY = 'dashboard.late_cycle.within_range';
const NO_PERSONAL_RANGE_KEY = 'dashboard.late_cycle.no_personal_range';

function isoToday(): string {
  const date = new Date();
  date.setHours(0, 0, 0, 0);
  const yyyy = date.getFullYear();
  const mm = String(date.getMonth() + 1).padStart(2, '0');
  const dd = String(date.getDate()).padStart(2, '0');
  return `${yyyy}-${mm}-${dd}`;
}

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

/**
 * Registers an owner and onboards with an explicit last-period-start date,
 * returning that date.
 *
 * Onboarding's own picker is the only way to set the baseline in step 1, and its
 * MinDate is the later of (Jan 1 of the current year) and (today - 60 days), so
 * every anchor below stays inside that window and older cycle starts are added
 * afterwards through the cycle-start endpoint, which has no past-date bound.
 */
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

  const startISO = shiftISODate(isoToday(), -startDaysAgo);
  await selectOnboardingStartDate(page, startISO);
  await page.locator('form[hx-post="/api/v1/onboarding/steps/1"] button[type="submit"]').click();

  const stepTwoForm = page.locator('form[hx-post="/api/v1/onboarding/steps/2"]');
  await expect(stepTwoForm).toBeVisible();
  await Promise.all([
    page.waitForURL(/\/dashboard(?:\?.*)?$/, { timeout: 15000 }),
    stepTwoForm.locator('[data-onboarding-step2-submit]').click(),
  ]);

  await setRequestTimezoneFromBrowser(page);
  return startISO;
}

/**
 * Backdates one cycle start. `page.request` sends no Origin of its own and the
 * CSRF middleware validates it on every mutating request under the HTTPS
 * posture, so the header is explicit here.
 */
async function markCycleStartViaAPI(page: Page, isoDate: string): Promise<void> {
  const csrf = (await page.locator('meta[name="csrf-token"]').getAttribute('content')) ?? '';
  const response = await page.request.post(`/api/v1/days/${isoDate}/cycle-start`, {
    headers: {
      ...apiOriginHeader(page),
      'X-CSRF-Token': csrf,
      'Content-Type': 'application/x-www-form-urlencoded',
    },
    form: { replace_existing: 'true' },
  });
  expect(response.status(), `mark cycle start at ${isoDate}`).toBeLessThan(400);
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
