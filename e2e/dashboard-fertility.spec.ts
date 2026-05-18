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

  test('marking more than 8 consecutive period days surfaces the long-period warning once', async ({
    page,
  }) => {
    // Register and onboard. The default onboarding helper sets
    // last_period_start to today-3 and period_length=5, so auto_period_fill
    // creates 5 period days [today-3 .. today+1]. Extending the streak past
    // 8 requires saving four more consecutive period days (today+2 .. +5).
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
          'X-CSRF-Token': await csrfToken(),
          'HX-Request': 'true',
          'Accept-Language': 'en',
        },
        form: { is_period: 'true', flow: 'medium' },
      });
    }

    // Days +2 .. +4 bring the streak from 5 (auto-filled) up to 8. None of
    // these saves should emit the long-period warning notice yet (the
    // threshold is `> 8`).
    for (const offset of [2, 3, 4]) {
      const response = await savePeriodDay(shiftISO(today, offset));
      expect(response.status(), `save offset ${offset} status`).toBeLessThan(400);
      expect(response.headers()['x-ovumcy-notice'] ?? '').toBe('');
    }

    // Day +5 crosses the threshold; the response carries the localized
    // warning copy URL-encoded in X-Ovumcy-Notice.
    const ninthDay = shiftISO(today, 5);
    const ninthResponse = await savePeriodDay(ninthDay);
    expect(ninthResponse.status()).toBeLessThan(400);
    // Go's url.QueryEscape encodes space as '+', so decode the header in two
    // passes (decode-uri-component handles %XX but leaves '+' alone).
    const rawNotice = ninthResponse.headers()['x-ovumcy-notice'] ?? '';
    const notice = decodeURIComponent(rawNotice.replace(/\+/g, '%20'));
    expect(notice).toContain('longer than 8 days');

    // The acknowledgement persists user.LongPeriodWarnedAt; a follow-up save
    // in the same cycle does NOT re-emit the warning. This is the "shown
    // once" half of the audit invariant.
    const followUpResponse = await savePeriodDay(shiftISO(today, 6));
    expect(followUpResponse.status()).toBeLessThan(400);
    expect(followUpResponse.headers()['x-ovumcy-notice'] ?? '').toBe('');
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
