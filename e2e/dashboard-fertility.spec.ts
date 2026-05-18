import { test, expect, type Page } from '@playwright/test';
import {
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  readRecoveryCode,
  registerOwnerViaUI,
} from './support/auth-helpers';
import { setRequestTimezoneFromBrowser } from './support/timezone-helpers';
import { openCalendarDayEditor, todayISOFromDashboard } from './support/stats-helpers';

async function registerAndSetEggwhiteToday(page: Page, prefix: string): Promise<void> {
  const credentials = createCredentials(prefix);
  await registerOwnerViaUI(page, credentials);
  await expectInlineRegisterRecoveryStep(page);
  await readRecoveryCode(page);
  await continueFromRecoveryCode(page);
  await completeOnboardingIfPresent(page);
  await setRequestTimezoneFromBrowser(page);

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
  await Promise.all([
    page.waitForResponse((response) => {
      return (
        response.request().method() === 'PUT' &&
        response.url().includes(`/api/v1/days/${today}`)
      );
    }),
    dayForm.evaluate((node) => {
      if (node instanceof HTMLFormElement) {
        node.requestSubmit();
      }
    }),
  ]);
}

async function setUsageGoal(
  page: Page,
  goal: 'avoid_pregnancy' | 'trying_to_conceive' | 'health'
): Promise<void> {
  await page.goto('/settings');
  await expect(page).toHaveURL(/\/settings$/);
  const cycleForm = page.locator('section#settings-cycle form[action="/api/v1/users/current/cycle"]');
  await expect(cycleForm).toBeVisible();
  await cycleForm.locator(`label.choice-option:has(input[name="usage_goal"][value="${goal}"])`).click();
  await cycleForm.locator('button[data-save-button]').click();
  await expect(page.locator('#settings-cycle-status .status-ok')).toBeVisible();
}

test.describe('Dashboard: fertility badge', () => {
  test('eggwhite cervical mucus shows the High fertility badge on dashboard', async ({ page }) => {
    await registerAndSetEggwhiteToday(page, 'fertility-eggwhite');

    // Dashboard hero now carries the high-fertility badge. For the default
    // usage_goal=health the localized text is "High fertility" and the badge
    // has neither the warning nor the positive variant class.
    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);

    const heroBadge = page.locator('.dashboard-cycle-hero-badge');
    await expect(heroBadge).toBeVisible();
    await expect(heroBadge).toContainText('High fertility');
    await expect(heroBadge).not.toHaveClass(/dashboard-cycle-hero-badge-warning/);
    await expect(heroBadge).not.toHaveClass(/dashboard-cycle-hero-badge-positive/);
    // The fallback `.dashboard-status-item` copy of the badge only renders
    // when the cycle hero is hidden (`{{if not .CycleHero.Visible}}` in
    // dashboard.html) — exercised separately by tests that suppress the hero.
  });

  test('usage_goal switches the eggwhite badge between health, avoid, and trying-to-conceive variants', async ({
    page,
  }) => {
    await registerAndSetEggwhiteToday(page, 'fertility-goals');
    const heroBadge = page.locator('.dashboard-cycle-hero-badge');

    // usage_goal=avoid_pregnancy -> warning copy + warning class.
    await setUsageGoal(page, 'avoid_pregnancy');
    await page.goto('/dashboard');
    await expect(heroBadge).toBeVisible();
    await expect(heroBadge).toContainText('Fertile period');
    await expect(heroBadge).not.toContainText('best timing');
    await expect(heroBadge).not.toContainText('High fertility');
    await expect(heroBadge).toHaveClass(/dashboard-cycle-hero-badge-warning/);

    // usage_goal=trying_to_conceive -> positive copy + positive class.
    await setUsageGoal(page, 'trying_to_conceive');
    await page.goto('/dashboard');
    await expect(heroBadge).toBeVisible();
    await expect(heroBadge).toContainText('Fertile period, best timing');
    await expect(heroBadge).toHaveClass(/dashboard-cycle-hero-badge-positive/);
    await expect(heroBadge).not.toHaveClass(/dashboard-cycle-hero-badge-warning/);

    // usage_goal=health -> generic copy + no variant class. Mirrors the
    // default state covered by the first test in this describe block.
    await setUsageGoal(page, 'health');
    await page.goto('/dashboard');
    await expect(heroBadge).toBeVisible();
    await expect(heroBadge).toContainText('High fertility');
    await expect(heroBadge).not.toHaveClass(/dashboard-cycle-hero-badge-warning/);
    await expect(heroBadge).not.toHaveClass(/dashboard-cycle-hero-badge-positive/);
  });
});
