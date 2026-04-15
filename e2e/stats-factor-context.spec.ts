import { expect, test } from '@playwright/test';
import { dashboardNextPeriodText } from './support/dashboard-helpers';
import {
  markCycleStart,
  registerOwnerAndEnableIrregularMode,
  saveCycleFactorOnDay,
  shiftISODate,
  todayISOFromDashboard,
} from './support/stats-helpers';

test.describe('Stats factor context', () => {
  test('owner sees sparse irregular explanations before range mode unlocks', async ({ page }) => {
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
  });

  test('owner sees conservative factor explanations in dashboard and stats', async ({ page }) => {
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
    await expect(page.locator('a[href="/settings#settings-cycle"]')).toHaveCount(0);
    await expect(page.locator('.warning-amber')).toHaveCount(0);
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
  });
});
