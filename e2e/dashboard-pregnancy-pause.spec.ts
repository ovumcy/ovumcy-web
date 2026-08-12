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
import {
  openCalendarDayEditor,
  saveDayEditorForm,
  todayISOFromDashboard,
} from './support/stats-helpers';
import { localeText } from './support/locale-helpers';

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
  test('positive pregnancy test persists, pauses predictions and stays removable', async ({
    page,
  }) => {
    await registerOnboardedOwner(page, 'pregnancy-pause');

    const today = await todayISOFromDashboard(page);

    // pregnancy_test is a free, always-shown day field (no tracking toggle to
    // enable, unlike cervical mucus / BBT). An untested day is absent data: the
    // control offers the two results only, neither of them filled.
    const dayForm = await openCalendarDayEditor(page, today);
    const control = dayForm.locator('[data-pregnancy-test]');
    await expect(control).toHaveAttribute('data-pregnancy-test-state', 'absent');
    await expect(control.locator('[data-pregnancy-test-option]')).toHaveCount(2);
    await expect(control.locator('[data-pregnancy-test-option="none"]')).toHaveCount(0);
    await expect(control.locator('[data-pregnancy-test-empty]')).toBeVisible();
    await expect(control.locator('[data-pregnancy-test-remove]')).toHaveCount(0);

    await dayForm
      .locator('label.choice-option:has(input[name="pregnancy_test"][value="positive"])')
      .click();
    await saveDayEditorForm(page, today, dayForm);

    // Persistence: reopening the day editor shows the positive radio selected,
    // and the saved result now offers its own removal.
    const savedForm = await openCalendarDayEditor(page, today);
    await expect(savedForm.locator('input[name="pregnancy_test"][value="positive"]')).toBeChecked();
    const savedControl = savedForm.locator('[data-pregnancy-test]');
    await expect(savedControl).toHaveAttribute('data-pregnancy-test-state', 'recorded');
    const remove = savedControl.locator('[data-pregnancy-test-remove]');
    await expect(remove).toBeVisible();
    await expect(remove).toHaveText(localeText('en', 'dashboard.pregnancy_test.remove'));

    // Pause: the owner dashboard surfaces the pregnancy-paused explainer.
    // Assert the stable explainer key (locale-independent) rather than copy,
    // and that the ring drops its phase segments while predictions are paused.
    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);
    const explainer = page.locator('[data-dashboard-prediction-explainer]');
    await expect(explainer).toBeVisible();
    await expect(explainer).toHaveAttribute('data-explainer-key', 'prediction.explainer.pregnancy_paused');
    await expect(page.locator('[data-dashboard-cycle-ring]')).toHaveAttribute(
      'data-cycle-ring-segmented',
      'false'
    );

    // Removal: the result the owner recorded is theirs to take back. Removing
    // it clears the field to absent data and lifts the pause with it.
    const removalForm = await openCalendarDayEditor(page, today);
    await removalForm.locator('[data-pregnancy-test-remove]').click();
    await saveDayEditorForm(page, today, removalForm);

    const clearedForm = await openCalendarDayEditor(page, today);
    const clearedControl = clearedForm.locator('[data-pregnancy-test]');
    await expect(clearedControl).toHaveAttribute('data-pregnancy-test-state', 'absent');
    await expect(clearedControl.locator('[data-pregnancy-test-empty]')).toBeVisible();
    await expect(
      clearedForm.locator('input[name="pregnancy_test"][value="positive"]')
    ).not.toBeChecked();

    await page.goto('/dashboard');
    await expect(
      page.locator('[data-explainer-key="prediction.explainer.pregnancy_paused"]')
    ).toHaveCount(0);
  });
});
