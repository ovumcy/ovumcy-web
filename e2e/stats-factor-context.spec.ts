import { expect, test } from '@playwright/test';
import { logoutViaAPI } from './support/auth-helpers';
import { dashboardNextPeriodText } from './support/dashboard-helpers';
import { fillDateField } from './support/date-field-helpers';
import {
  markCycleStart,
  registerOwnerAndEnableIrregularMode,
  saveCycleFactorOnDay,
  shiftISODate,
  todayISOFromDashboard,
} from './support/stats-helpers';

test.describe('Stats factor context', () => {
  test('owner sees sparse irregular explanations before range mode unlocks', async ({ page }) => {
    // Seeding walks the calendar per cycle start and per saved factor, and the
    // in-test positive anchor adds one more day-editor round trip. Same budget
    // as the comparably seeded stats test in visual-a11y.spec.ts.
    test.slow();

    await registerOwnerAndEnableIrregularMode(page, 'stats-factor-sparse');

    const today = await todayISOFromDashboard(page);
    const cycleStarts = [-56, -28].map((offset) => shiftISODate(today, offset));

    for (const cycleStart of cycleStarts) {
      await markCycleStart(page, cycleStart);
    }

    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);
    expect(await dashboardNextPeriodText(page)).toContain('3 cycles are needed for a reliable range');
    await expect(page.locator('[data-dashboard-prediction-explainer]')).toContainText(
      'Irregular cycle mode needs at least 3 completed cycles before Ovumcy can show steadier ranges.'
    );
    await expect(page.locator('[data-dashboard-factor-hint]')).toHaveCount(0);

    await page.goto('/stats');
    await expect(page).toHaveURL(/\/stats$/);
    await expect(page.locator('[data-stats-prediction-explainer]')).toContainText(
      'Irregular cycle mode needs at least 3 completed cycles before Ovumcy can show steadier ranges.'
    );

    await page.goto(`/calendar?month=${today.slice(0, 7)}&day=${today}`);
    await expect(page).toHaveURL(new RegExp(`/calendar\\?month=${today.slice(0, 7)}&day=${today}`));
    const calendarExplainer = page.locator('[data-calendar-prediction-explainer]');
    await expect(calendarExplainer).toBeVisible();
    await expect(calendarExplainer).toContainText(
      'Irregular cycle mode needs at least 3 completed cycles before Ovumcy can show steadier ranges.'
    );

    // Positive anchor for the count-0 assertion above: the hint hook is alive
    // for this very owner once a cycle factor exists inside the 90-day context
    // window. Without it the absent hint proves nothing — a hook that never
    // renders reads exactly the same way.
    await saveCycleFactorOnDay(page, shiftISODate(cycleStarts[1], 2), 'stress');

    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);
    await expect(page.locator('[data-dashboard-factor-hint]')).toBeVisible();
  });

  test('owner sees conservative factor explanations in dashboard and stats', async ({ page }) => {
    // Four seeded cycle starts, three factor saves, three page surfaces, and a
    // second owner for the paired warning phase: the default 30s budget left no
    // headroom on a loaded runner even before the phase was added.
    test.slow();

    await registerOwnerAndEnableIrregularMode(page, 'stats-factor-context');

    const today = await todayISOFromDashboard(page);
    const cycleStarts = [-112, -84, -56, -28].map((offset) => shiftISODate(today, offset));

    for (const cycleStart of cycleStarts) {
      await markCycleStart(page, cycleStart);
    }

    await saveCycleFactorOnDay(page, shiftISODate(cycleStarts[0], 2), 'stress');
    await saveCycleFactorOnDay(page, shiftISODate(cycleStarts[1], 2), 'travel');
    await saveCycleFactorOnDay(page, shiftISODate(cycleStarts[2], 2), 'stress');

    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);
    // Address the suppressed warnings through their own hook rather than the
    // bare styling class: every amber warning on this page lives inside
    // [data-dashboard-cycle-warnings], together with the update-cycle-data link.
    await expect(page.locator('[data-dashboard-cycle-warnings]')).toHaveCount(0);
    await expect(page.locator('a[href="/settings#settings-cycle"]')).toHaveCount(0);
    const nextPeriodText = await dashboardNextPeriodText(page);
    expect(nextPeriodText).toMatch(/\w{3} \d{1,2}, \d{4} — \w{3} \d{1,2}, \d{4}/);
    expect(nextPeriodText).not.toContain('3 cycles are needed');
    await expect(page.locator('[data-dashboard-prediction-explainer]')).toContainText(
      'Irregular cycle mode uses ranges instead of exact prediction dates.'
    );
    const dashboardHint = page.locator('[data-dashboard-factor-hint]');
    await expect(dashboardHint).toBeVisible();
    await expect(dashboardHint).toContainText('Recent tags can add context when timing feels less steady.');
    await expect(dashboardHint.getByText('Stress', { exact: true }).first()).toBeVisible();
    await expect(dashboardHint.getByText('Travel', { exact: true }).first()).toBeVisible();

    await page.goto('/stats');
    await expect(page).toHaveURL(/\/stats$/);
    await expect(page.locator('[data-stats-prediction-explainer]')).toContainText(
      'Irregular cycle mode uses ranges instead of exact prediction dates.'
    );
    const factorSection = page.locator('[data-stats-factor-context]');
    await expect(factorSection).toBeVisible();
    await expect(factorSection).toContainText('Stress');
    await expect(factorSection).toContainText('Travel');
    await expect(factorSection).toContainText('Recent cycle context');

    await page.goto(`/calendar?month=${today.slice(0, 7)}&day=${today}`);
    await expect(page).toHaveURL(new RegExp(`/calendar\\?month=${today.slice(0, 7)}&day=${today}`));
    const calendarExplainer = page.locator('[data-calendar-prediction-explainer]');
    await expect(calendarExplainer).toBeVisible();
    await expect(calendarExplainer).toContainText('Irregular cycle mode uses ranges instead of exact prediction dates.');
    await expect(calendarExplainer).toContainText('Recent tags can add context when timing feels less steady.');

    // Paired positive phase for the two count-0 assertions above. A conservative
    // baseline must SUPPRESS the cycle-warning block; that only means something
    // if the same block renders when the data warrants it. Second owner, same
    // irregular mode, but a cycle baseline older than the reference cycle length
    // — the single input DashboardCycleDataLooksStale keys on. Owners are
    // isolated by user_id, so this account observes only its own dashboard.
    await logoutViaAPI(page);
    await registerOwnerAndEnableIrregularMode(page, 'stats-factor-stale-baseline');

    const staleCycleForm = page.locator('section#settings-cycle form[action="/api/v1/users/current/cycle"]');
    await expect(staleCycleForm).toBeVisible();
    await fillDateField(staleCycleForm.locator('#settings-last-period-start'), shiftISODate(today, -30));
    // Bind to this save's own PATCH: the irregular-mode save inside
    // registerOwnerAndEnableIrregularMode already left a .status-ok in
    // #settings-cycle-status, so waiting on that alone resolves instantly and
    // the /dashboard navigation below aborts a still-in-flight baseline write.
    const [staleCycleSave] = await Promise.all([
      page.waitForRequest(
        (request) =>
          request.method() === 'PATCH' && request.url().includes('/api/v1/users/current/cycle'),
      ),
      staleCycleForm.locator('button[data-save-button]').click(),
    ]);
    const staleCycleResponse = await staleCycleSave.response();
    expect(staleCycleResponse, 'expected a response for PATCH /api/v1/users/current/cycle').not.toBeNull();
    expect(staleCycleResponse!.ok()).toBeTruthy();

    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);
    const cycleWarnings = page.locator('[data-dashboard-cycle-warnings]');
    await expect(cycleWarnings).toBeVisible();
    await expect(cycleWarnings.locator('[data-dashboard-stale-warning]')).toBeVisible();
    await expect(cycleWarnings.locator('a[href="/settings#settings-cycle"]')).toBeVisible();
  });
});
