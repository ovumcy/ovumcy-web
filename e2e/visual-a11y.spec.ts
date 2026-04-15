import { expect, test, type Locator, type Page } from '@playwright/test';
import { fillDateField } from './support/date-field-helpers';
import {
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  readRecoveryCode,
  registerOwnerViaUI,
} from './support/auth-helpers';
import {
  assertNoHorizontalOverflow,
  expectElementAboveMobileTabbar,
  expectVisibleFocusIndicator,
} from './support/mobile-layout-helpers';
import {
  markCycleStart,
  registerOwnerAndEnableIrregularMode,
  saveBBTOnDay,
  saveCycleFactorOnDay,
  shiftISODate,
  todayISOFromDashboard,
} from './support/stats-helpers';

async function registerOwnerAndReachDashboard(page: Page, prefix: string): Promise<void> {
  const credentials = createCredentials(prefix);

  await registerOwnerViaUI(page, credentials);
  await expectInlineRegisterRecoveryStep(page);
  await readRecoveryCode(page);
  await continueFromRecoveryCode(page);
  await completeOnboardingIfPresent(page);

  await page.goto('/dashboard');
  await expect(page).toHaveURL(/\/dashboard$/);
}

async function setCurrentCycleStart(page: Page, isoDate: string): Promise<void> {
  await page.goto('/settings');
  await expect(page).toHaveURL(/\/settings$/);

  const cycleForm = page.locator('section#settings-cycle form[action="/settings/cycle"]');
  await expect(cycleForm).toBeVisible();
  await fillDateField(cycleForm.locator('#settings-last-period-start'), isoDate);
  await cycleForm.locator('button[data-save-button]').click();
  await expect(page.locator('#settings-cycle-status .status-ok')).toBeVisible();
}

async function seedStatsInsightState(page: Page, prefix: string): Promise<void> {
  await registerOwnerAndEnableIrregularMode(page, prefix);

  const today = await todayISOFromDashboard(page);
  const cycleStarts = [-112, -84, -56, -28].map((offset) => shiftISODate(today, offset));

  for (const cycleStart of cycleStarts) {
    await markCycleStart(page, cycleStart);
  }

  await saveCycleFactorOnDay(page, shiftISODate(cycleStarts[0], 2), 'stress');
  await saveCycleFactorOnDay(page, shiftISODate(cycleStarts[1], 2), 'travel');
  await saveCycleFactorOnDay(page, shiftISODate(cycleStarts[2], 2), 'stress');

  const currentCycleStart = shiftISODate(today, -8);
  await setCurrentCycleStart(page, currentCycleStart);

  const bbtDays = [0, 1, 2, 3, 4].map((offset) => shiftISODate(currentCycleStart, offset));
  const bbtValues = ['36.40', '36.45', '36.50', '36.55', '36.60'];
  for (let index = 0; index < bbtDays.length; index += 1) {
    await saveBBTOnDay(page, bbtDays[index], bbtValues[index]);
  }
}

test.describe('Visual and accessibility regressions', () => {
  test('mobile dashboard, settings, and privacy stay within the viewport and above the tabbar', async ({
    page,
  }) => {
    await registerOwnerAndReachDashboard(page, 'visual-mobile-layout');
    await page.setViewportSize({ width: 390, height: 844 });

    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);
    await assertNoHorizontalOverflow(page);
    const dashboardAutosave = page.locator('[data-dashboard-autosave-indicator]');
    await dashboardAutosave.scrollIntoViewIfNeeded();
    await expectElementAboveMobileTabbar(page, dashboardAutosave);

    await page.goto('/settings');
    await expect(page).toHaveURL(/\/settings$/);
    await assertNoHorizontalOverflow(page);
    const trackingSave = page.locator('[data-settings-tracking-save]');
    await trackingSave.scrollIntoViewIfNeeded();
    await expectElementAboveMobileTabbar(page, trackingSave);

    await page.goto('/privacy?back=%2Fsettings');
    await expect(page).toHaveURL(/\/privacy\?back=%2Fsettings$/);
    await assertNoHorizontalOverflow(page);
    const sourceLink = page.locator('a[href="https://github.com/ovumcy/ovumcy-web"]');
    await sourceLink.scrollIntoViewIfNeeded();
    await expectElementAboveMobileTabbar(page, sourceLink);
  });

  test('primary navigation and actions show visible focus indicators', async ({
    page,
  }) => {
    await registerOwnerAndReachDashboard(page, 'visual-focus');
    await page.setViewportSize({ width: 1280, height: 900 });

    await page.goto('/settings');
    await expect(page).toHaveURL(/\/settings$/);

    const brandMark = page.locator('a.brand-mark');
    const todayLink = page.locator('nav.sm\\:flex a[href="/dashboard"]').first();
    const logoutButton = page.locator('.nav-logout-form button[type="submit"]').first();

    await brandMark.focus();
    await expect(brandMark).toBeFocused();
    await expectVisibleFocusIndicator(brandMark);

    await todayLink.focus();
    await expect(todayLink).toBeFocused();
    await expectVisibleFocusIndicator(todayLink);

    await logoutButton.focus();
    await expect(logoutButton).toBeFocused();
    await expectVisibleFocusIndicator(logoutButton);
  });

  test('stats insight state stays readable on mobile and exposes accessible summaries', async ({
    page,
  }) => {
    test.slow();

    await seedStatsInsightState(page, 'visual-stats-mobile');
    await page.setViewportSize({ width: 390, height: 844 });

    await page.goto('/stats');
    await expect(page).toHaveURL(/\/stats$/);
    await assertNoHorizontalOverflow(page);
    await expect(page.locator('[data-usage-goal-summary]')).toBeVisible();
    await expect(page.locator('[data-stats-factor-context]')).toBeVisible();
    await expect(page.locator('#cycle-chart')).toBeVisible();
    await expect(page.locator('#cycle-chart')).toHaveAttribute('role', 'img');
    await expect(page.locator('#stats-cycle-trend-summary')).toBeVisible();

    const bbtSummary = page.locator('#stats-bbt-summary');
    if ((await bbtSummary.count()) > 0) {
      await expect(bbtSummary).toBeVisible();
    }

    const cycleSummary = page.locator('#stats-cycle-trend-summary');
    await cycleSummary.scrollIntoViewIfNeeded();
    await expectElementAboveMobileTabbar(page, cycleSummary);
  });
});
