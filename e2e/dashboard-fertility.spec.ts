import { test, expect, type Page } from './support/fixtures';
import {
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  readRecoveryCode,
  registerOwnerViaUI,
  apiOriginHeader,
} from './support/auth-helpers';
import { localeText } from './support/locale-helpers';
import { setRequestTimezoneFromBrowser } from './support/timezone-helpers';
import { dashboardStatusLine } from './support/dashboard-helpers';
import {
  markCycleStartViaAPI,
  openCalendarDayEditor,
  shiftISODate,
  todayISOFromDashboard,
} from './support/stats-helpers';

// The best-timing badge is withheld until the account has enough completed
// cycles for the fertility projection to be published. Three is the floor the
// regular mode is moving to, so the seeded history stays valid under it.
const COMPLETED_CYCLES_FOR_FERTILITY = 3;

// Onboarding records the current cycle's start at today-3. Each completed cycle
// is seeded as a 28-day-earlier start behind it, so N seeds give N completed
// cycles ending at that anchor.
async function seedCompletedCycles(page: Page, count: number): Promise<void> {
  const today = await todayISOFromDashboard(page);
  const currentStart = shiftISODate(today, -3);
  for (let index = 1; index <= count; index += 1) {
    await markCycleStartViaAPI(page, shiftISODate(currentStart, -28 * index));
  }
}

async function registerAndSetEggwhiteToday(
  page: Page,
  prefix: string,
  completedCycles: number = COMPLETED_CYCLES_FOR_FERTILITY,
): Promise<void> {
  const credentials = createCredentials(prefix);
  await registerOwnerViaUI(page, credentials);
  await expectInlineRegisterRecoveryStep(page);
  await readRecoveryCode(page);
  await continueFromRecoveryCode(page);
  await completeOnboardingIfPresent(page);
  await setRequestTimezoneFromBrowser(page);
  await seedCompletedCycles(page, completedCycles);

  await page.goto('/settings');
  await expect(page).toHaveURL(/\/settings$/);
  const trackingSection = page.locator('#settings-tracking');
  await trackingSection.locator('input[name="track_cervical_mucus"]').check();
  const trackingForm = trackingSection.locator('form[data-settings-draft-form="tracking"]');
  await trackingForm.evaluate((node) => {
    if (node instanceof HTMLFormElement) {
      node.requestSubmit();
    }
  });
  await expect(page.locator('#settings-tracking-status .status-ok')).toBeVisible();

  const today = await todayISOFromDashboard(page);
  const dayForm = await openCalendarDayEditor(page, today);
  await dayForm
    .locator('label.choice-option:has(input[name="cervical_mucus"][value="eggwhite"])')
    .click();
  const [request] = await Promise.all([
    page.waitForRequest(
      (candidate) =>
        candidate.method() === 'PUT' && candidate.url().includes(`/api/v1/days/${today}`),
    ),
    dayForm.evaluate((node) => {
      if (node instanceof HTMLFormElement) {
        node.requestSubmit();
      }
    }),
  ]);
  const response = await request.response();
  expect(response, `expected a response for PUT /api/v1/days/${today}`).not.toBeNull();
  expect(response!.ok(), `PUT /api/v1/days/${today} failed with ${response!.status()}`).toBeTruthy();
  // The caller navigates straight to /dashboard next; let the
  // calendar-day-updated grid refresh + editor re-lazy-load cascade settle
  // first so that navigation doesn't race an in-flight server request
  // (see saveDayEditorForm in calendar-autofill-clear.spec.ts).
  await page.waitForLoadState('networkidle');
}

async function setUsageGoal(
  page: Page,
  goal: 'avoid_pregnancy' | 'trying_to_conceive' | 'health'
): Promise<void> {
  await page.goto('/settings');
  await expect(page).toHaveURL(/\/settings$/);
  const cycleForm = page.locator('#settings-cycle form[action="/api/v1/users/current/cycle"]');
  await expect(cycleForm).toBeVisible();
  await cycleForm.locator(`label.choice-option:has(input[name="usage_goal"][value="${goal}"])`).click();
  await cycleForm.locator('button[data-save-button]').click();
  await expect(page.locator('#settings-cycle-status .status-ok')).toBeVisible();
}

test.describe('Dashboard: fertility badge', () => {
  test('eggwhite cervical mucus shows the High fertility badge on dashboard', async ({ page }) => {
    await registerAndSetEggwhiteToday(page, 'fertility-eggwhite');

    // The dashboard status header carries the high-fertility badge. For the
    // default usage_goal=health the localized text is "High fertility" and the
    // badge has neither the warning nor the positive variant class.
    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);

    const fertilityBadge = page.locator('[data-dashboard-status-line] [data-fertility-badge]');
    await expect(fertilityBadge).toBeVisible();
    await expect(fertilityBadge).toHaveAttribute('data-fertility-badge-variant', 'neutral');
    await expect(fertilityBadge).toHaveAttribute(
      'data-fertility-badge-key',
      'dashboard.high_fertility_badge'
    );
    // One rendered-copy assertion for this surface, taken from the catalogue;
    // the variant is proved by the attribute above rather than by two negated
    // class checks.
    await expect(fertilityBadge).toContainText(localeText('en', 'dashboard.high_fertility_badge'));
  });

  test('eggwhite cervical mucus shows no badge while the account has no completed cycle', async ({
    page,
  }) => {
    await registerAndSetEggwhiteToday(page, 'fertility-no-history', 0);

    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);

    // The status line itself renders (the phase item is always there), so an
    // absent badge is the withheld state and not a page that failed to load.
    await expect(dashboardStatusLine(page)).toBeVisible();
    await expect(page.locator('[data-fertility-badge]')).toHaveCount(0);
  });

  test('marking more than 8 consecutive period days surfaces the long-period warning once', async ({
    page,
  }) => {
    // Register and onboard. The default onboarding helper sets
    // last_period_start to today-3 and period_length=5; auto_period_fill stops
    // at today, so it creates period days today-3 .. today. A period cannot
    // be recorded past today+2, and the streak is counted backward from the
    // saved day, so the run is first extended backward to today-6 and the
    // threshold is then crossed by saving forward up to today+2.
    const credentials = createCredentials('long-period-warning');
    await registerOwnerViaUI(page, credentials);
    await expectInlineRegisterRecoveryStep(page);
    await readRecoveryCode(page);
    await continueFromRecoveryCode(page);
    await completeOnboardingIfPresent(page);
    await setRequestTimezoneFromBrowser(page);

    const today = await todayISOFromDashboard(page);

    function shiftISO(iso: string, days: number): string {
      const [year, month, day] = iso.split('-').map(Number);
      const date = new Date(year, month - 1, day);
      date.setDate(date.getDate() + days);
      return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`;
    }

    async function csrfToken(): Promise<string> {
      return (await page.locator('meta[name="csrf-token"]').getAttribute('content')) ?? '';
    }

    async function savePeriodDay(isoDate: string) {
      return page.request.put(`/api/v1/days/${isoDate}`, {
        headers: {
          ...apiOriginHeader(page),
          'X-CSRF-Token': await csrfToken(),
          'HX-Request': 'true',
          'Accept-Language': 'en',
        },
        form: { is_period: 'true', flow: 'medium' },
      });
    }

    // Days -6 .. -4 extend the run backward, and day +1 brings the streak
    // counted from it to 8 (-6 .. +1). None of these saves may emit the
    // long-period warning yet (the threshold is `> 8`).
    for (const offset of [-6, -5, -4, 1]) {
      const response = await savePeriodDay(shiftISO(today, offset));
      expect(response.status(), `save offset ${offset} status`).toBeLessThan(400);
      expect(response.headers()['x-ovumcy-notice-key'] ?? '').not.toBe('dashboard.long_period_warning');
    }

    // Day +2 crosses the threshold; the response carries the localized
    // warning copy URL-encoded in X-Ovumcy-Notice.
    const ninthDay = shiftISO(today, 2);
    const ninthResponse = await savePeriodDay(ninthDay);
    expect(ninthResponse.status()).toBeLessThan(400);
    // Which warning fired is asserted through the companion key header, so the
    // check does not rest on a fragment of English copy the catalogue owns.
    expect(ninthResponse.headers()['x-ovumcy-notice-key']).toBe('dashboard.long_period_warning');
    // The rendered sentence still has to survive the transport intact, and that
    // is a property of the encoding rather than of the wording: Go's
    // url.QueryEscape turns a space into '+', so decode in two passes
    // (decode-uri-component handles %XX but leaves '+' alone) and compare
    // against the catalogue entry the key just named.
    const rawNotice = ninthResponse.headers()['x-ovumcy-notice'] ?? '';
    const notice = decodeURIComponent(rawNotice.replace(/\+/g, '%20'));
    expect(notice).toBe(localeText('en', 'dashboard.long_period_warning'));

    // The acknowledgement persists user.LongPeriodWarnedAt; a follow-up save
    // in the same cycle does NOT re-emit the warning. This is the "shown
    // once" half of the audit invariant. Re-saving day +2 counts the same
    // 9-day streak from the same start, so only the acknowledgement keeps it
    // silent.
    const followUpResponse = await savePeriodDay(ninthDay);
    expect(followUpResponse.status()).toBeLessThan(400);
    expect(followUpResponse.headers()['x-ovumcy-notice'] ?? '').toBe('');
  });

  test('usage_goal switches the eggwhite badge between health, avoid, and trying-to-conceive variants', async ({
    page,
  }) => {
    await registerAndSetEggwhiteToday(page, 'fertility-goals');
    const fertilityBadge = page.locator('[data-dashboard-status-line] [data-fertility-badge]');

    // Each goal selects a distinct badge variant + copy key. Asserting the two
    // single-valued attributes replaces the mix of positive copy, negated copy
    // and negated class checks: a key cannot be two values at once, so pinning
    // it proves the other two variants did not render.
    const expectedByGoal = [
      ['avoid_pregnancy', 'warning', 'dashboard.fertility_badge_warning'],
      ['trying_to_conceive', 'positive', 'dashboard.fertility_badge_positive'],
      ['health', 'neutral', 'dashboard.high_fertility_badge'],
    ] as const;

    for (const [goal, variant, copyKey] of expectedByGoal) {
      await setUsageGoal(page, goal);
      await page.goto('/dashboard');
      await expect(fertilityBadge).toBeVisible();
      await expect(fertilityBadge).toHaveAttribute('data-fertility-badge-variant', variant);
      await expect(fertilityBadge).toHaveAttribute('data-fertility-badge-key', copyKey);
      await expect(fertilityBadge).toContainText(localeText('en', copyKey));
    }
  });
});
