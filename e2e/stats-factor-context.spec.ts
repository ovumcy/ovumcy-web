import { expect, test } from '@playwright/test';
import { dashboardNextPeriodText } from './support/dashboard-helpers';
import { displayDatesIn } from './support/date-field-helpers';
import { localeText } from './support/locale-helpers';
import {
  markCycleStart,
  registerOwnerAndEnableIrregularMode,
  saveCycleFactorOnDay,
  shiftISODate,
  todayISOFromDashboard,
} from './support/stats-helpers';

const SPARSE_EXPLAINER_KEY = 'prediction.explainer.irregular_sparse';
const RANGES_EXPLAINER_KEY = 'prediction.explainer.irregular_ranges';
const FACTOR_CONTEXT_EXPLAINER_KEY = 'prediction.explainer.factor_context';

test.describe('Stats factor context', () => {
  test('owner sees sparse irregular explanations before range mode unlocks', async ({ page }) => {
    await registerOwnerAndEnableIrregularMode(page, 'stats-factor-sparse');

    const today = await todayISOFromDashboard(page);
    const cycleStarts = [-56, -28].map((offset) => shiftISODate(today, offset));

    for (const cycleStart of cycleStarts) {
      await markCycleStart(page, cycleStart);
    }

    // The explainer key is the state under test on all three surfaces; the copy
    // is asserted once, from the catalogue, so the three surfaces cannot drift
    // apart and no sentence is re-typed per surface.
    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);
    expect(await dashboardNextPeriodText(page)).toContain(
      localeText('en', 'dashboard.next_period_need_cycles')
    );
    const dashboardExplainer = page.locator('[data-dashboard-prediction-explainer]');
    await expect(dashboardExplainer).toHaveAttribute('data-explainer-key', SPARSE_EXPLAINER_KEY);
    await expect(dashboardExplainer).toContainText(localeText('en', SPARSE_EXPLAINER_KEY));
    await expect(page.locator('[data-dashboard-factor-hint]')).toHaveCount(0);

    await page.goto('/stats');
    await expect(page).toHaveURL(/\/stats$/);
    await expect(page.locator('[data-stats-prediction-explainer]')).toHaveAttribute(
      'data-explainer-key',
      SPARSE_EXPLAINER_KEY
    );

    await page.goto(`/calendar?month=${today.slice(0, 7)}&day=${today}`);
    await expect(page).toHaveURL(new RegExp(`/calendar\\?month=${today.slice(0, 7)}&day=${today}`));
    const calendarExplainer = page.locator('[data-calendar-prediction-explainer]');
    await expect(calendarExplainer).toBeVisible();
    await expect(calendarExplainer).toHaveAttribute(
      'data-explainer-primary-key',
      SPARSE_EXPLAINER_KEY
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

    // Two real calendar dates in order, derived through Intl from the app's own
    // display format — the EN-US shape regex this replaces matched any
    // three-letter word followed by digits.
    const nextPeriodText = await dashboardNextPeriodText(page);
    const renderedDates = await displayDatesIn(page, nextPeriodText);
    expect(renderedDates, `range mode should render a date range: ${nextPeriodText}`).toHaveLength(2);
    expect(renderedDates[1] > renderedDates[0]).toBeTruthy();

    // Range mode, not sparse mode — asserted on the single-valued explainer key
    // rather than by forbidding the sparse phrase, which any rewording defeats.
    await expect(page.locator('[data-dashboard-prediction-explainer]')).toHaveAttribute(
      'data-explainer-key',
      RANGES_EXPLAINER_KEY
    );

    // The chips were seeded by key, so address them by key: getByText('Stress')
    // only worked because the run happens to be in English.
    const dashboardHint = page.locator('[data-dashboard-factor-hint]');
    await expect(dashboardHint).toBeVisible();
    await expect(dashboardHint).toContainText(localeText('en', FACTOR_CONTEXT_EXPLAINER_KEY));
    await expect(dashboardHint.locator('[data-cycle-factor="stress"]')).toHaveCount(1);
    await expect(dashboardHint.locator('[data-cycle-factor="travel"]')).toHaveCount(1);

    await page.goto('/stats');
    await expect(page).toHaveURL(/\/stats$/);
    await expect(page.locator('[data-stats-prediction-explainer]')).toHaveAttribute(
      'data-explainer-key',
      RANGES_EXPLAINER_KEY
    );
    const factorSection = page.locator('[data-stats-factor-context]');
    await expect(factorSection).toBeVisible();
    await expect(factorSection.locator('[data-cycle-factor="stress"]').first()).toBeVisible();
    await expect(factorSection.locator('[data-cycle-factor="travel"]').first()).toBeVisible();
    await expect(factorSection.locator('[data-stats-factor-recent-cycles]')).toContainText(
      localeText('en', 'stats.factor_recent_cycles_title')
    );

    await page.goto(`/calendar?month=${today.slice(0, 7)}&day=${today}`);
    await expect(page).toHaveURL(new RegExp(`/calendar\\?month=${today.slice(0, 7)}&day=${today}`));
    const calendarExplainer = page.locator('[data-calendar-prediction-explainer]');
    await expect(calendarExplainer).toBeVisible();
    await expect(calendarExplainer).toHaveAttribute(
      'data-explainer-primary-key',
      RANGES_EXPLAINER_KEY
    );
    await expect(calendarExplainer).toHaveAttribute(
      'data-explainer-secondary-key',
      FACTOR_CONTEXT_EXPLAINER_KEY
    );
  });
});
