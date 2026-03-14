import { expect, test, type Locator, type Page } from '@playwright/test';
import { clearDateField, dateFieldRoot, fillDateField } from './support/date-field-helpers';
import {
  continueFromRecoveryCode,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  loginViaUI,
  logoutViaAPI,
  readRecoveryCode,
  registerOwnerViaUI,
} from './support/auth-helpers';

function toISODate(date: Date): string {
  const copy = new Date(date);
  copy.setHours(0, 0, 0, 0);
  const yyyy = copy.getFullYear();
  const mm = String(copy.getMonth() + 1).padStart(2, '0');
  const dd = String(copy.getDate()).padStart(2, '0');
  return `${yyyy}-${mm}-${dd}`;
}

function shiftISODate(iso: string, days: number): string {
  const [y, m, d] = iso.split('-').map((part) => Number(part));
  const date = new Date(y, m - 1, d);
  date.setDate(date.getDate() + days);
  return toISODate(date);
}

async function setRangeValue(locator: Locator, value: number): Promise<void> {
  await locator.evaluate((element, rawValue) => {
    const input = element as HTMLInputElement;
    input.value = String(rawValue);
    input.dispatchEvent(new Event('input', { bubbles: true }));
    input.dispatchEvent(new Event('change', { bubbles: true }));
  }, value);
}

async function ensureOnboardingStepOneVisible(page: Page): Promise<void> {
  await expect(page).toHaveURL(/\/onboarding(?:\?.*)?$/);

  const stepOneDateInput = page.locator('#last-period-start');
  await expect(dateFieldRoot(stepOneDateInput)).toBeVisible();
}

async function registerAndOpenOnboarding(page: Page, emailPrefix: string) {
  const creds = createCredentials(emailPrefix);

  await registerOwnerViaUI(page, creds);
  await expectInlineRegisterRecoveryStep(page);

  await readRecoveryCode(page);
  await page.locator('#recovery-code-saved').check();
  await page.locator('form[action] button[type="submit"]').click();

  await ensureOnboardingStepOneVisible(page);
  return creds;
}

async function submitStepOne(page: Page, dateISO: string): Promise<void> {
  const input = page.locator('#last-period-start');
  await fillDateField(input, dateISO);
  await page.locator('form[hx-post="/onboarding/step1"] button[type="submit"]').click();
  await expect(page.locator('form[hx-post="/onboarding/step2"]')).toBeVisible();
}

async function submitStepTwo(page: Page): Promise<void> {
  await page.locator('form[hx-post="/onboarding/step2"] button[type="submit"]').click();
  await expect(page).toHaveURL(/\/dashboard$/);
}

async function currentDashboardNextPeriodText(page: Page): Promise<string> {
  const value = await page.locator('[data-dashboard-next-period]').textContent();

  return String(value || '').trim();
}

test.describe('Onboarding flow', () => {
  test('onboarding appears on first login only, then redirects to dashboard', async ({ page }) => {
    const creds = await registerAndOpenOnboarding(page, 'onboarding-first-login');

    const startDate = toISODate(new Date(Date.now() - 3 * 24 * 60 * 60 * 1000));
    await submitStepOne(page, startDate);
    await submitStepTwo(page);

    await logoutViaAPI(page);
    await loginViaUI(page, creds);

    await expect(page).toHaveURL(/\/dashboard$/);
    await page.goto('/onboarding');
    await expect(page).toHaveURL(/\/dashboard$/);
  });

  test('step 1 quick-pick sets date and empty submit is blocked by validation', async ({ page }) => {
    await registerAndOpenOnboarding(page, 'onboarding-step1-quickpick');

    const dateInput = page.locator('#last-period-start');
    await clearDateField(dateInput);

    await page.locator('form[hx-post="/onboarding/step1"] button[type="submit"]').click();
    await expect(page).toHaveURL(/\/onboarding(?:\?.*)?$/);
    await expect(page.locator('#onboarding-step1-status .status-error')).toBeVisible();

    const stepTwoForm = page.locator('form[hx-post="/onboarding/step2"]');
    const stepTwoVisible = await stepTwoForm.isVisible().catch(() => false);
    if (stepTwoVisible) {
      await stepTwoForm.locator('button.btn-secondary[type="button"]').click();
      await expect(dateFieldRoot(dateInput)).toBeVisible();
    } else {
      await expect(stepTwoForm).not.toBeVisible();
    }

    const quickPickButtons = page.locator(
      'form[hx-post="/onboarding/step1"] .grid button[data-onboarding-day-option]'
    );
    const firstQuickPick = quickPickButtons.first();
    await expect(firstQuickPick).toBeVisible();
    await expect(firstQuickPick).toContainText('Today');
    await expect(quickPickButtons.nth(1)).toContainText('Yesterday');
    await expect(quickPickButtons.nth(2)).toContainText('2 days ago');

    const firstQuickPickValue = await firstQuickPick.getAttribute('data-onboarding-day-value');
    expect(firstQuickPickValue).toMatch(/^\d{4}-\d{2}-\d{2}$/);
    await expect(firstQuickPick).toHaveAttribute('aria-pressed', 'false');

    await firstQuickPick.click();

    await expect(dateInput).toHaveValue(String(firstQuickPickValue));
    await expect(firstQuickPick).toHaveAttribute('aria-pressed', 'true');
    await expect(firstQuickPick).toHaveClass(/choice-chip-active/);
    await page.locator('form[hx-post="/onboarding/step1"] button[type="submit"]').click();
    await expect(page.locator('form[hx-post="/onboarding/step2"]')).toBeVisible();
    await expect(page.locator('form[hx-post="/onboarding/step2"]')).toContainText(/21.?35/);
  });

  test('today quick-pick keeps the exact selected date through onboarding completion', async ({
    page,
  }) => {
    await registerAndOpenOnboarding(page, 'onboarding-step1-today-persist');

    const todayQuickPick = page
      .locator('form[hx-post="/onboarding/step1"] button[data-onboarding-day-option]')
      .first();
    const selectedValue = await todayQuickPick.getAttribute('data-onboarding-day-value');
    expect(selectedValue).toMatch(/^\d{4}-\d{2}-\d{2}$/);

    await todayQuickPick.click();
    await page.locator('form[hx-post="/onboarding/step1"] button[type="submit"]').click();
    await expect(page.locator('form[hx-post="/onboarding/step2"]')).toBeVisible();
    await submitStepTwo(page);

    await page.goto('/settings');
    await expect(page).toHaveURL(/\/settings$/);
    await expect(page.locator('#settings-last-period-start')).toHaveValue(String(selectedValue));
  });

  test('step 1 rejects out-of-range manual dates instead of clamping them', async ({ page }) => {
    await registerAndOpenOnboarding(page, 'onboarding-step1-bounds');

    const input = page.locator('#last-period-start');
    const min = await input.getAttribute('min');
    const max = await input.getAttribute('max');

    expect(min).toMatch(/^\d{4}-\d{2}-\d{2}$/);
    expect(max).toMatch(/^\d{4}-\d{2}-\d{2}$/);
    expect(min! <= max!).toBe(true);

    const tooOldDate = shiftISODate(min!, -1);
    const futureDate = shiftISODate(max!, 1);
    const stepTwoForm = page.locator('form[hx-post="/onboarding/step2"]');
    const submitButton = page.locator('form[hx-post="/onboarding/step1"] button[type="submit"]');
    const stepOneStatus = page.locator('#onboarding-step1-status');

    await fillDateField(input, tooOldDate);
    await expect(input).toHaveValue(tooOldDate);
    await submitButton.click();
    await expect(page).toHaveURL(/\/onboarding(?:\?.*)?$/);
    await expect(stepTwoForm).not.toBeVisible();
    await expect(stepOneStatus.locator('.status-error')).toBeVisible();

    await fillDateField(input, futureDate);
    await expect(input).toHaveValue(futureDate);
    await submitButton.click();
    await expect(page).toHaveURL(/\/onboarding(?:\?.*)?$/);
    await expect(stepTwoForm).not.toBeVisible();
    await expect(stepOneStatus.locator('.status-error')).toBeVisible();
  });

  test('step 2 sliders and auto-fill toggle update state, and Back preserves values', async ({ page }) => {
    await registerAndOpenOnboarding(page, 'onboarding-step2-state');

    const selectedDate = toISODate(new Date(Date.now() - 5 * 24 * 60 * 60 * 1000));
    await submitStepOne(page, selectedDate);

    const cycleSlider = page.locator('#cycle-length');
    const periodSlider = page.locator('#period-length');
    const autoFillCheckbox = page.locator('form[hx-post="/onboarding/step2"] input[name="auto_period_fill"]');
    const irregularCheckbox = page.locator('form[hx-post="/onboarding/step2"] input[name="irregular_cycle"]');
    const autoFillToggle = page.locator('form[hx-post="/onboarding/step2"] label[data-binary-toggle]:has(input[name="auto_period_fill"])');
    const irregularToggle = page.locator('form[hx-post="/onboarding/step2"] label[data-binary-toggle]:has(input[name="irregular_cycle"])');
    const finishButtonShell = page.locator('[data-onboarding-step2-submit-shell]');

    await expect(finishButtonShell).toBeVisible();
    expect(
      await finishButtonShell.evaluate((node) => window.getComputedStyle(node).overflow)
    ).toBe('hidden');

    await setRangeValue(cycleSlider, 35);
    await setRangeValue(periodSlider, 6);
    await autoFillCheckbox.uncheck();

    await expect(cycleSlider).toHaveValue('35');
    await expect(periodSlider).toHaveValue('6');
    await expect(autoFillCheckbox).not.toBeChecked();
    await expect(irregularCheckbox).not.toBeChecked();
    await expect(autoFillToggle).toHaveAttribute('data-active', 'false');
    await expect(irregularToggle).toHaveAttribute('data-active', 'false');

    await page.locator('form[hx-post="/onboarding/step2"] button.btn-secondary[type="button"]').click();

    const stepOneInput = page.locator('#last-period-start');
    await expect(dateFieldRoot(stepOneInput)).toBeVisible();
    await expect(stepOneInput).toHaveValue(selectedDate);

    await page.locator('form[hx-post="/onboarding/step1"] button[type="submit"]').click();
    await expect(page.locator('form[hx-post="/onboarding/step2"]')).toBeVisible();

    await expect(cycleSlider).toHaveValue('35');
    await expect(periodSlider).toHaveValue('6');
    await expect(autoFillCheckbox).not.toBeChecked();
    await expect(autoFillToggle).toHaveAttribute('data-active', 'false');

    await submitStepTwo(page);
    await expect(page).toHaveURL(/\/dashboard$/);
  });

  test('step query is preserved by the language route and keeps step 2 visible', async ({ page }) => {
    const creds = createCredentials('onboarding-step-query');

    await registerOwnerViaUI(page, creds);
    await expectInlineRegisterRecoveryStep(page);

    await readRecoveryCode(page);
    await continueFromRecoveryCode(page);
    await expect(page).toHaveURL(/\/onboarding(?:\?.*)?$/);

    await page.goto('/onboarding?step=2');
    await expect(page.locator('form[hx-post="/onboarding/step2"]')).toBeVisible();

    await page.goto(`/lang/ru?next=${encodeURIComponent('/onboarding?step=2')}`);
    await expect(page.locator('html')).toHaveAttribute('lang', 'ru');

    const currentURL = new URL(page.url());
    expect(currentURL.pathname).toBe('/onboarding');
    expect(currentURL.searchParams.get('step')).toBe('2');
    await expect(page.locator('form[hx-post="/onboarding/step2"]')).toBeVisible();
  });

  test('reload during onboarding keeps progress or resets gracefully without blocking completion', async ({
    page,
  }) => {
    await registerAndOpenOnboarding(page, 'onboarding-reload');

    const startDate = toISODate(new Date(Date.now() - 7 * 24 * 60 * 60 * 1000));
    await submitStepOne(page, startDate);

    const cycleSlider = page.locator('#cycle-length');
    await setRangeValue(cycleSlider, 32);
    await expect(cycleSlider).toHaveValue('32');

    await page.reload();
    await expect(page).toHaveURL(/\/onboarding(?:\?.*)?$/);

    const stepTwoVisible = await page.locator('form[hx-post="/onboarding/step2"]').isVisible().catch(() => false);
    if (stepTwoVisible) {
      await submitStepTwo(page);
      return;
    }

    await ensureOnboardingStepOneVisible(page);
    await fillDateField(page.locator('#last-period-start'), startDate);
    await submitStepOne(page, startDate);
    await submitStepTwo(page);
  });

  test('step 2 irregular checkbox carries through to dashboard range prediction', async ({ page }) => {
    await registerAndOpenOnboarding(page, 'onboarding-irregular');

    const selectedDate = toISODate(new Date(Date.now() - 5 * 24 * 60 * 60 * 1000));
    await submitStepOne(page, selectedDate);

    const irregularCheckbox = page.locator('form[hx-post="/onboarding/step2"] input[name="irregular_cycle"]');
    await irregularCheckbox.check();
    await submitStepTwo(page);

    const nextPeriodText = await currentDashboardNextPeriodText(page);
    expect(nextPeriodText).toContain('around');
    expect(nextPeriodText).toContain('3 cycles are needed');
    expect(nextPeriodText).not.toContain(' - ');
  });
});
