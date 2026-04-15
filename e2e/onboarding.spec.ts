import { expect, test, type Locator, type Page } from '@playwright/test';
import { clearDateField, dateFieldRoot, fillDateField } from './support/date-field-helpers';
import { dashboardNextPeriodText } from './support/dashboard-helpers';
import {
  continueFromRecoveryCode,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  loginViaUI,
  logoutViaAPI,
  readRecoveryCode,
  registerOwnerViaUI,
} from './support/auth-helpers';
import { switchPublicLanguage } from './support/language-helpers';

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

function onboardingStepOneForm(page: Page): Locator {
  return page.locator('form[data-onboarding-form-step="1"]');
}

function onboardingStepTwoForm(page: Page): Locator {
  return page.locator('form[data-onboarding-form-step="2"]');
}

function onboardingStepOneSubmit(page: Page): Locator {
  return onboardingStepOneForm(page).locator('button[type="submit"]');
}

function onboardingStepTwoSubmit(page: Page): Locator {
  return page.locator('[data-onboarding-step2-submit]');
}

function onboardingQuickPickButtons(page: Page): Locator {
  return page.locator('[data-onboarding-day-options] button[data-onboarding-day-option]');
}

async function activateOnboardingQuickPick(page: Page, quickPick: Locator): Promise<void> {
  await quickPick.focus();
  await page.keyboard.press('Enter');
}

function onboardingStepTwoBackButton(page: Page): Locator {
  return page.locator('[data-onboarding-go-step="1"]');
}

async function registerAndOpenOnboarding(page: Page, emailPrefix: string) {
  const creds = createCredentials(emailPrefix);

  await registerOwnerViaUI(page, creds);
  await expectInlineRegisterRecoveryStep(page);

  await readRecoveryCode(page);
  await continueFromRecoveryCode(page);

  await ensureOnboardingStepOneVisible(page);
  return creds;
}

async function submitStepOne(page: Page, dateISO: string): Promise<void> {
  const input = page.locator('#last-period-start');
  await fillDateField(input, dateISO);
  await onboardingStepOneSubmit(page).click();
  await expect(onboardingStepTwoForm(page)).toBeVisible();
}

async function submitStepTwo(page: Page): Promise<void> {
  await onboardingStepTwoSubmit(page).click();
  await expect(page).toHaveURL(/\/dashboard$/);
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

    await onboardingStepOneSubmit(page).click();
    await expect(page).toHaveURL(/\/onboarding(?:\?.*)?$/);
    await expect(page.locator('#onboarding-step1-status .status-error')).toBeVisible();

    const stepTwoForm = onboardingStepTwoForm(page);
    const stepTwoVisible = await stepTwoForm.isVisible().catch(() => false);
    if (stepTwoVisible) {
      await onboardingStepTwoBackButton(page).click();
      await expect(dateFieldRoot(dateInput)).toBeVisible();
    } else {
      await expect(stepTwoForm).not.toBeVisible();
    }

    const quickPickButtons = onboardingQuickPickButtons(page);
    const firstQuickPick = quickPickButtons.first();
    await expect(firstQuickPick).toBeVisible();
    await expect(firstQuickPick).toContainText('Today');
    await expect(quickPickButtons.nth(1)).toContainText('Yesterday');
    await expect(quickPickButtons.nth(2)).toContainText('2 days ago');

    const firstQuickPickValue = await firstQuickPick.getAttribute('data-onboarding-day-value');
    expect(firstQuickPickValue).toMatch(/^\d{4}-\d{2}-\d{2}$/);
    await expect(firstQuickPick).toHaveAttribute('aria-pressed', 'false');

    await activateOnboardingQuickPick(page, firstQuickPick);

    await expect(dateInput).toHaveValue(String(firstQuickPickValue));
    await expect(firstQuickPick).toHaveAttribute('aria-pressed', 'true');
    await expect(firstQuickPick).toHaveClass(/choice-chip-active/);
    await onboardingStepOneSubmit(page).click();
    await expect(onboardingStepTwoForm(page)).toBeVisible();
    await expect(onboardingStepTwoForm(page)).toContainText(/21.?35/);
  });

  test('today quick-pick keeps the exact selected date through onboarding completion', async ({
    page,
  }) => {
    await registerAndOpenOnboarding(page, 'onboarding-step1-today-persist');

    const todayQuickPick = onboardingQuickPickButtons(page).first();
    const selectedValue = await todayQuickPick.getAttribute('data-onboarding-day-value');
    expect(selectedValue).toMatch(/^\d{4}-\d{2}-\d{2}$/);

    await activateOnboardingQuickPick(page, todayQuickPick);
    await onboardingStepOneSubmit(page).click();
    await expect(onboardingStepTwoForm(page)).toBeVisible();
    await submitStepTwo(page);

    await page.goto('/settings');
    await expect(page).toHaveURL(/\/settings$/);
    await expect(page.locator('#settings-last-period-start')).toHaveValue(String(selectedValue));
  });

  test('russian onboarding quick-picks stay localized even if server labels are missing', async ({
    page,
  }) => {
    const creds = createCredentials('onboarding-step1-ru-localized');
    await registerOwnerViaUI(page, creds);
    await expectInlineRegisterRecoveryStep(page);
    await readRecoveryCode(page);
    await continueFromRecoveryCode(page);

    await switchPublicLanguage(page, 'ru');
    await expect(page).toHaveURL(/\/onboarding(?:\?.*)?$/);
    await expect(page.locator('html')).toHaveAttribute('lang', 'ru');

    const quickPickButtons = onboardingQuickPickButtons(page);
    await expect(quickPickButtons.first()).toContainText('Сегодня');
    await expect(quickPickButtons.nth(1)).toContainText('Вчера');
    await expect(quickPickButtons.nth(2)).toContainText('2 дня назад');
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
    const stepTwoForm = onboardingStepTwoForm(page);
    const submitButton = onboardingStepOneSubmit(page);
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
    const autoFillCheckbox = page.locator('[data-onboarding-auto-period-fill]');
    const irregularCheckbox = onboardingStepTwoForm(page).locator('input[name="irregular_cycle"]');
    const autoFillToggle = onboardingStepTwoForm(page).locator('label[data-binary-toggle]:has(input[name="auto_period_fill"])');
    const irregularToggle = onboardingStepTwoForm(page).locator('label[data-binary-toggle]:has(input[name="irregular_cycle"])');
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

    await onboardingStepTwoBackButton(page).click();

    const stepOneInput = page.locator('#last-period-start');
    await expect(dateFieldRoot(stepOneInput)).toBeVisible();
    await expect(stepOneInput).toHaveValue(selectedDate);

    await onboardingStepOneSubmit(page).click();
    await expect(onboardingStepTwoForm(page)).toBeVisible();

    await expect(cycleSlider).toHaveValue('35');
    await expect(periodSlider).toHaveValue('6');
    await expect(autoFillCheckbox).not.toBeChecked();
    await expect(autoFillToggle).toHaveAttribute('data-active', 'false');

    await submitStepTwo(page);
    await expect(page).toHaveURL(/\/dashboard$/);
  });

  test('step query is preserved by the public language switch and keeps step 2 visible', async ({ page }) => {
    const creds = createCredentials('onboarding-step-query');

    await registerOwnerViaUI(page, creds);
    await expectInlineRegisterRecoveryStep(page);

    await readRecoveryCode(page);
    await continueFromRecoveryCode(page);
    await expect(page).toHaveURL(/\/onboarding(?:\?.*)?$/);

    await page.goto('/onboarding?step=2');
    await expect(onboardingStepTwoForm(page)).toBeVisible();

    await switchPublicLanguage(page, 'ru');
    await expect(page.locator('html')).toHaveAttribute('lang', 'ru');

    const currentURL = new URL(page.url());
    expect(currentURL.pathname).toBe('/onboarding');
    expect(currentURL.searchParams.get('step')).toBe('2');
    await expect(onboardingStepTwoForm(page)).toBeVisible();
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

    const stepTwoVisible = await onboardingStepTwoForm(page).isVisible().catch(() => false);
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

    const irregularCheckbox = onboardingStepTwoForm(page).locator('input[name="irregular_cycle"]');
    await irregularCheckbox.check();
    await submitStepTwo(page);

    const nextPeriodText = await dashboardNextPeriodText(page);
    expect(nextPeriodText).toContain('around');
    expect(nextPeriodText).toContain('3 cycles are needed');
    expect(nextPeriodText).not.toContain(' - ');
  });
});
