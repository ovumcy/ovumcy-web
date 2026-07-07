import { expect, test, type Page } from '@playwright/test';
import {
  continueFromRecoveryCode,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  readRecoveryCode,
  registerOwnerViaUI,
} from './support/auth-helpers';
import { dateFieldRoot, fillDateField } from './support/date-field-helpers';
import { setRequestTimezoneFromBrowser } from './support/timezone-helpers';

// Onboarding seeds a single cycle (last_period_start + the default 28-day
// cycle length, models.DefaultCycleLength), enough for the dashboard to show a
// single-date next-period estimate. With the default 3-day reminder window
// (services.DashboardReminderBannerWindowDays), a start N days ago places the
// next period at (28 - N) days out: 26 -> in 2 days (plural "~N days" copy),
// 27 -> tomorrow (the fixed non-plural copy).
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
  const credentials = createCredentials(prefix);
  await registerOwnerViaUI(page, credentials);
  await expectInlineRegisterRecoveryStep(page);
  await readRecoveryCode(page);
  await continueFromRecoveryCode(page);

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

  // Pin "today" to the browser timezone so the day-count baked into the banner
  // copy matches the offsets computed above regardless of the server's zone.
  await setRequestTimezoneFromBrowser(page);
}

test.describe('Dashboard reminder banner', () => {
  // testing.md keeps the backend HTML regressions on the data-reminder-banner-key
  // hook only; the rendered visible copy — including the day count interpolated
  // into the plural variant — is owned here, addressed via that same hook.
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
