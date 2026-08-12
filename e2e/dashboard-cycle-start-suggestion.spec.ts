import { test, expect, type Page } from '@playwright/test';
import {
  apiOriginHeader,
  continueFromRecoveryCode,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  readRecoveryCode,
  registerOwnerViaUI,
} from './support/auth-helpers';
import { cancelConfirmDialog, mutatingRequestsDuring } from './support/confirm-dialog-helpers';
import { localeText } from './support/locale-helpers';
import { selectOnboardingStartDate } from './support/onboarding-helpers';
import { shiftISODate, todayISOFromDashboard } from './support/stats-helpers';
import { setRequestTimezoneFromBrowser } from './support/timezone-helpers';

/**
 * The dashboard's calm cycle-start SUGGESTION — the `[data-cycle-start-suggestion]`
 * hint inside `#dashboard-cycle-start` — as opposed to the inline cycle-start
 * QUESTION beside the period toggle, which `dashboard_journal_regressions_test.go`
 * and the day-editor specs already cover.
 *
 * Reaching it is not just "a period day after a long gap". The dashboard renders
 * the hint under `ShowCycleStartSuggestion && !ShowCycleStartQuestion`
 * (`dashboard.html`), and both flags share one core in
 * `internal/services/cycle_start_policy.go`:
 *
 *   ShowCycleStartSuggestion = today is a period day, is not already a cycle
 *                              start, and sits >= 15 days (manualCycleStart
 *                              SuggestionGapDays) past the latest anchor —
 *                              latest explicit `cycle_start` log, else the
 *                              account's last-period-start.
 *   ShowCycleStartQuestion   = the same core AND no competing cycle start
 *                              inside today's period cluster.
 *
 * So the suggestion state is exactly the core plus a competing cycle start in
 * the cluster: bleeding that already carries this cycle's recorded start.
 * `ShouldAskCycleStartQuestion`'s own comment names why the inline one-tap
 * question steps aside there — answering yes would have to REPLACE that start,
 * which belongs to the manual control's confirmation flow — and the calm hint
 * is what points at that control.
 *
 * Seeded state (offsets from the server's today, read off the dashboard):
 *
 *   -40  onboarding start, so the account's last-period-start cannot become the
 *        anchor (the default onboarding of today-3 would make the gap 3 and no
 *        hint would render at all)
 *   -20  a period day, which only exists so the recorded start below sits INSIDE
 *        the cluster rather than on its first day (see below); it carries no
 *        cycle start, so it does not move the anchor
 *   -16  explicit cycle start -> the anchor, gap 16 >= 15, and the competing
 *        start inside the cluster
 *   -12, -8, -4, today  period days at <= 5-day steps, which is what keeps them
 *        in ONE period cluster: `buildPeriodClusters` splits only on a gap of
 *        5+ empty days, so -20..today stays a single cluster and -16's start
 *        competes with today
 *
 * Auto-period-fill (on by default from onboarding) widens each written day
 * forward by up to the period length and never writes a cycle start, so it can
 * only reinforce that single cluster, never split it or move the anchor.
 *
 * Why the competing start may not be the cluster's first day: inside
 * `findCompetingCycleStart` each candidate is rebuilt at location midnight while
 * the cluster bounds come from `dateOnly` (UTC midnight), so under a positive
 * UTC offset a start on the first day of the cluster reads as sitting *before*
 * the cluster and is skipped. Measured in `internal/services`: with
 * `Europe/Belgrade` the same logs yield no conflict for a start on the cluster's
 * first day and the expected conflict for an interior one. An interior day is
 * strictly inside the bounds at every offset, which is what makes this spec
 * timezone-independent.
 *
 * Seeding goes through the API with an explicit `Origin` (the CSRF middleware
 * validates it and `page.request.*` sends none of its own) rather than through
 * the dashboard's save button: the button is being removed with the autosave
 * work, and this spec's subject is the hint, not the save control.
 */

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
  // completeOnboardingIfPresent hardcodes today-3 as the period start, which
  // would leave the account's last-period-start as a three-day-old anchor and
  // silence the policy. Run onboarding with an explicit older date instead.
  const credentials = createCredentials(prefix);
  await registerOwnerViaUI(page, credentials);
  await expectInlineRegisterRecoveryStep(page);
  await readRecoveryCode(page);
  await continueFromRecoveryCode(page);

  await selectOnboardingStartDate(page, isoDateDaysAgo(startDaysAgo));
  await page.locator('form[hx-post="/api/v1/onboarding/steps/1"] button[type="submit"]').click();

  const stepTwoForm = page.locator('form[hx-post="/api/v1/onboarding/steps/2"]');
  await expect(stepTwoForm).toBeVisible();
  await Promise.all([
    page.waitForURL(/\/dashboard(?:\?.*)?$/, { timeout: 15000 }),
    stepTwoForm.locator('[data-onboarding-step2-submit]').click(),
  ]);

  await setRequestTimezoneFromBrowser(page);
}

async function csrfToken(page: Page): Promise<string> {
  return (await page.locator('meta[name="csrf-token"]').getAttribute('content')) ?? '';
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
  expect(response.status(), `save period day on ${isoDate}`).toBeLessThan(400);
}

async function markCycleStartViaAPI(page: Page, isoDate: string): Promise<void> {
  // `replace_existing=false` on purpose: this seed day must be conflict-free, so
  // a rejected replace is a broken precondition rather than something to paper
  // over. The endpoint sets is_period AND cycle_start on the day, which is what
  // makes it the explicit anchor.
  const response = await page.request.post(`/api/v1/days/${isoDate}/cycle-start`, {
    headers: {
      ...apiOriginHeader(page),
      'X-CSRF-Token': await csrfToken(page),
      'Content-Type': 'application/x-www-form-urlencoded',
    },
    form: { replace_existing: 'false' },
  });
  expect(response.status(), `mark cycle start on ${isoDate}`).toBeLessThan(400);
}

async function fetchDay(page: Page, isoDate: string): Promise<Record<string, unknown>> {
  const response = await page.request.get(`/api/v1/days/${isoDate}`, {
    headers: { Accept: 'application/json', ...apiOriginHeader(page) },
  });
  expect(response.status(), `GET day ${isoDate}`).toBe(200);
  return (await response.json()) as Record<string, unknown>;
}

const CLUSTER_OPENING_DAYS_BACK = 20;
const ANCHOR_DAYS_BACK = 16;
const BRIDGE_DAYS_BACK = [12, 8, 4, 0];

test.describe('Dashboard: cycle-start suggestion', () => {
  test('a period day far past a start the same bleeding already carries gets the calm suggestion, and only accepting it moves the cycle start', async ({
    page,
  }) => {
    test.slow();

    await registerAndOnboardWithStartDaysAgo(page, 'dashboard-cycle-start-suggestion', 40);
    const today = await todayISOFromDashboard(page);
    const anchorISO = shiftISODate(today, -ANCHOR_DAYS_BACK);

    await savePeriodDay(page, shiftISODate(today, -CLUSTER_OPENING_DAYS_BACK));
    await markCycleStartViaAPI(page, anchorISO);
    for (const daysBack of BRIDGE_DAYS_BACK) {
      await savePeriodDay(page, shiftISODate(today, -daysBack));
    }

    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);

    // Positive anchor first: the journal renders and today is the period day the
    // policy needs, so the absent question below is the policy staying quiet
    // rather than a page that failed to load.
    await expect(page.locator('[data-period-toggle]')).toBeChecked();

    // The hint lives with the manual control it points at, so scope it there —
    // the block below the journal keeps its hooks while the dashboard above the
    // journal is being reshaped.
    const suggestion = page.locator('#dashboard-cycle-start [data-cycle-start-suggestion]');
    await expect(suggestion).toHaveCount(1);
    await expect(suggestion).toBeVisible();
    await expect(suggestion).toHaveAttribute(
      'data-notice-key',
      'dashboard.cycle_start_suggestion'
    );
    await expect(suggestion).toHaveText(localeText('en', 'dashboard.cycle_start_suggestion'));

    // The distinction this state is defined by: no inline one-tap question.
    await expect(page.locator('[data-cycle-start-question]')).toHaveCount(0);

    const manualStartButton = page.locator('[data-dashboard-cycle-start-button]');
    await expect(manualStartButton).toBeVisible();

    // The dialog copy comes from the catalogue, with the two dates taken from
    // the server-rendered policy node — the same substitution the client makes
    // (`formatCycleStartMessage` replaces one `%s` per value, in order).
    const policy = page.locator('#dashboard-cycle-start [data-cycle-start-policy]');
    await expect(policy).toHaveCount(1);
    await expect(policy).toHaveAttribute('data-cycle-start-conflict', 'true');
    const conflictDate = ((await policy.getAttribute('data-cycle-start-conflict-date')) ?? '').trim();
    const targetDate = ((await policy.getAttribute('data-cycle-start-target-date')) ?? '').trim();
    expect(conflictDate, 'the policy node must carry the competing start date').not.toBe('');
    expect(targetDate, 'the policy node must carry the target date').not.toBe('');
    const expectedReplaceMessage = localeText('en', 'dashboard.cycle_start_replace_message')
      .replace('%s', conflictDate)
      .replace('%s', targetDate);

    // Dismissing the offer must recompute nothing. An unchanged-looking page is
    // a weaker claim than it looks — it also holds when the POST fired and
    // succeeded — so record the wire across the whole day surface, and reload
    // inside the window so anything the cancelled click was going to issue has
    // had its chance.
    const dismissedRequests = await mutatingRequestsDuring(
      page,
      (pathname) => pathname.startsWith('/api/v1/days/'),
      async () => {
        await manualStartButton.click();
        await expect(page.locator('#confirm-modal-message')).toHaveText(expectedReplaceMessage);
        await expect(page.locator('#confirm-modal-accept')).toHaveText(
          localeText('en', 'dashboard.cycle_start_replace_accept')
        );
        await cancelConfirmDialog(page);

        await page.reload();
        await expect(suggestion).toBeVisible();
      }
    );
    expect(dismissedRequests, 'dismissing the suggestion must issue no day mutation').toEqual([]);

    const dismissedToday = await fetchDay(page, today);
    expect(dismissedToday.cycle_start, 'today must stay a plain period day').toBe(false);
    expect(dismissedToday.is_period, 'today must stay a period day').toBe(true);
    const dismissedAnchor = await fetchDay(page, anchorISO);
    expect(dismissedAnchor.cycle_start, 'the recorded start must stay where it was').toBe(true);

    // Accepting is the answer the hint points at: the manual control, through
    // its replace confirmation.
    const [request] = await Promise.all([
      page.waitForRequest(
        (candidate) =>
          candidate.method() === 'POST' &&
          candidate.url().includes(`/api/v1/days/${today}/cycle-start?source=dashboard`)
      ),
      (async () => {
        await manualStartButton.click();
        await expect(page.locator('#confirm-modal-message')).toHaveText(expectedReplaceMessage);
        await page.locator('#confirm-modal-accept').click();
      })(),
    ]);
    const response = await request.response();
    expect(
      response,
      `expected a response for POST /api/v1/days/${today}/cycle-start?source=dashboard`
    ).not.toBeNull();
    expect(
      response!.ok(),
      `POST /api/v1/days/${today}/cycle-start?source=dashboard failed with ${response!.status()}`
    ).toBeTruthy();
    // The handler answers HTMX with HX-Refresh, so the dashboard reloads itself;
    // let that settle before reading state (see saveDayEditorForm).
    await page.waitForLoadState('networkidle');

    const markedToday = await fetchDay(page, today);
    expect(markedToday.cycle_start, 'accepting must mark today as the cycle start').toBe(true);
    expect(markedToday.is_period, 'accepting must keep today a period day').toBe(true);
    const replacedAnchor = await fetchDay(page, anchorISO);
    expect(
      replacedAnchor.cycle_start,
      'the replaced start inside the cluster must be cleared'
    ).toBe(false);

    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);
    // Positive anchor again: the block still renders, so the hint is gone
    // because today is now the cycle start, not because the section vanished.
    await expect(page.locator('[data-dashboard-cycle-start-button]')).toBeVisible();
    await expect(page.locator('[data-cycle-start-suggestion]')).toHaveCount(0);
    await expect(page.locator('[data-cycle-start-question]')).toHaveCount(0);
  });
});
