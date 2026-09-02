import { expect, test } from './support/fixtures';
import { registerAndOnboardWithStartDaysAgo } from './support/stats-helpers';

// Onboarding seeds a single cycle (last_period_start + the default 28-day
// cycle length, models.DefaultCycleLength), enough for the dashboard to show a
// single-date next-period estimate. With the default 3-day reminder window
// (services.DashboardReminderBannerWindowDays), a start N days ago places the
// next period at (28 - N) days out: 26 -> in 2 days (plural "~N days" copy),
// 27 -> tomorrow (the fixed non-plural copy). registerAndOnboardWithStartDaysAgo
// pins "today" to the browser timezone, so those day counts hold whatever zone
// the server runs in.
test.describe('Dashboard reminder banner', () => {
  // The backend HTML regressions stay on the data-reminder-banner-key hook only;
  // the rendered visible copy — including the day count interpolated into the
  // plural variant — is owned here, addressed via that same hook.
  test('a next period two days out renders the plural "~N days" reminder copy', async ({
    page,
  }) => {
    await registerAndOnboardWithStartDaysAgo(page, 'dashboard-reminder-plural', 26);

    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);

    const banner = page.locator('[data-dashboard-reminder-banner]');
    await expect(banner).toBeVisible();
    await expect(banner).toHaveAttribute(
      'data-reminder-banner-key',
      'dashboard.reminder_banner_period'
    );
    await expect(banner).toContainText('Period likely in ~2 days');

    // The always-on medical-safety disclaimer rides alongside every prediction.
    await expect(page.locator('[data-dashboard-prediction-disclaimer]')).toBeVisible();
  });

  test('a next period one day out renders the fixed "tomorrow" reminder copy', async ({
    page,
  }) => {
    await registerAndOnboardWithStartDaysAgo(page, 'dashboard-reminder-tomorrow', 27);

    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);

    const banner = page.locator('[data-dashboard-reminder-banner]');
    await expect(banner).toBeVisible();
    await expect(banner).toHaveAttribute(
      'data-reminder-banner-key',
      'dashboard.reminder_banner_period_tomorrow'
    );
    await expect(banner).toContainText('Period likely tomorrow');

    await expect(page.locator('[data-dashboard-prediction-disclaimer]')).toBeVisible();
  });
});
