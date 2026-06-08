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

async function registerOnboardedOwner(page: Page, prefix: string): Promise<void> {
  const credentials = createCredentials(prefix);
  await registerOwnerViaUI(page, credentials);
  await expectInlineRegisterRecoveryStep(page);
  await readRecoveryCode(page);
  await continueFromRecoveryCode(page);
  await completeOnboardingIfPresent(page);
  await setRequestTimezoneFromBrowser(page);
}

test.describe('Dashboard: pregnancy test pause', () => {
  // The resume path (a cycle start logged after the positive test lifts the
  // pause) is exercised at the unit level by ResolvePregnancyPause and
  // BuildCycleStatsForRange tests; this spec covers the user-visible happy
  // path: logging a positive test persists and pauses dashboard predictions.
  test('positive pregnancy test persists and pauses owner predictions', async ({ page }) => {
    await registerOnboardedOwner(page, 'pregnancy-pause');

    const today = await todayISOFromDashboard(page);

    // pregnancy_test is a free, always-shown day field (no tracking toggle to
    // enable, unlike cervical mucus / BBT).
    const dayForm = await openCalendarDayEditor(page, today);
    await dayForm
      .locator('label.choice-option:has(input[name="pregnancy_test"][value="positive"])')
      .click();
    await Promise.all([
      page.waitForResponse(
        (response) =>
          response.request().method() === 'PUT' && response.url().includes(`/api/v1/days/${today}`)
      ),
      dayForm.evaluate((node) => {
        if (node instanceof HTMLFormElement) {
          node.requestSubmit();
        }
      }),
    ]);

    // Persistence: reopening the day editor shows the positive radio selected.
    const savedForm = await openCalendarDayEditor(page, today);
    await expect(savedForm.locator('input[name="pregnancy_test"][value="positive"]')).toBeChecked();

    // Pause: the owner dashboard surfaces the pregnancy-paused explainer.
    // Assert the stable explainer key (locale-independent) rather than copy,
    // and that the segmented cycle hero is suppressed.
    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);
    const explainer = page.locator('[data-dashboard-prediction-explainer]');
    await expect(explainer).toBeVisible();
    await expect(explainer).toHaveAttribute('data-explainer-key', 'prediction.explainer.pregnancy_paused');
    await expect(page.locator('[data-dashboard-cycle-hero]')).toHaveCount(0);
  });
});
