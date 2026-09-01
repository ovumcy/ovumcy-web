import { expect, test, type Locator, type Page } from './support/fixtures';
import { dashboardNextPeriodText } from './support/dashboard-helpers';
import {
  apiOriginHeader,
  continueFromRecoveryCode,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  loginViaUI,
  logoutViaAPI,
  readRecoveryCode,
  registerOwnerViaUI,
} from './support/auth-helpers';
import { switchPublicLanguage } from './support/language-helpers';
import { localeText } from './support/locale-helpers';
import { assertNoHorizontalOverflow } from './support/mobile-layout-helpers';
import { applyTheme, expectTextContrastAA } from './support/contrast-helpers';
import {
  onboardingDayCell,
  onboardingPicker,
  onboardingShortcut,
  selectOnboardingStartDate,
} from './support/onboarding-helpers';

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
  await expect(onboardingPicker(page)).toBeVisible();
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

async function activateOnboardingShortcut(page: Page, name: 'today' | 'yesterday'): Promise<void> {
  await onboardingShortcut(page, name).focus();
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
  await selectOnboardingStartDate(page, dateISO);
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

  test('step 1 blocks an empty submit and the today shortcut fills the date', async ({
    browserErrors,
    page,
  }) => {
    // The empty submit below is meant to reach the server and be refused, so
    // htmx logs the 400 on the console. That log is the subject here, not a
    // fault: the assertion two lines down is that the refusal rendered.
    browserErrors.allow(
      /^console\.error: Response Status Error Code 400 from \/api\/v1\/onboarding\/steps\/1/,
      'this test submits step 1 empty on purpose and asserts the rejection is rendered'
    );

    await registerAndOpenOnboarding(page, 'onboarding-step1-quickpick');

    const transport = page.locator('#last-period-start');
    // A fresh owner has no baseline, so the picker opens with nothing selected.
    await expect(transport).toHaveValue('');

    await onboardingStepOneSubmit(page).click();
    await expect(page).toHaveURL(/\/onboarding(?:\?.*)?$/);
    // Client guard and server answer carry the same key, so the assertion holds
    // whichever of the two rendered this status.
    await expect(page.locator('#onboarding-step1-status .status-error')).toHaveText(
      localeText('en', 'onboarding.error.date_required')
    );
    await expect(onboardingStepTwoForm(page)).not.toBeVisible();

    const today = String(await transport.getAttribute('max'));
    expect(today).toMatch(/^\d{4}-\d{2}-\d{2}$/);

    const todayShortcut = onboardingShortcut(page, 'today');
    await expect(todayShortcut).toBeVisible();
    await expect(todayShortcut).toHaveAttribute('aria-pressed', 'false');

    await activateOnboardingShortcut(page, 'today');

    await expect(transport).toHaveValue(today);
    await expect(todayShortcut).toHaveAttribute('aria-pressed', 'true');
    await expect(onboardingDayCell(page, today)).toHaveAttribute('aria-pressed', 'true');
    await expect(onboardingDayCell(page, today)).toHaveClass(/onboarding-day-cell-selected/);

    await onboardingStepOneSubmit(page).click();
    await expect(onboardingStepTwoForm(page)).toBeVisible();
    await expect(onboardingStepTwoForm(page)).toContainText(/21.?35/);
  });

  test('step 1 offers one month picker and no second date input', async ({ page }) => {
    await registerAndOpenOnboarding(page, 'onboarding-step1-one-picker');

    const panel = page.locator('[data-onboarding-panel="1"]');
    const picker = onboardingPicker(page);
    const transport = page.locator('#last-period-start');

    // One value, one mechanism: the segmented DD/MM/YYYY field that used to sit
    // next to the day grid is gone, and the only element carrying the submitted
    // name is the picker's transport input.
    await expect(panel.locator('[data-date-field]')).toHaveCount(0);
    await expect(panel.locator('[data-date-field-part]')).toHaveCount(0);
    await expect(panel.locator('input[name="last_period_start"]')).toHaveCount(1);
    await expect(picker).toBeVisible();

    const today = String(await transport.getAttribute('max'));
    expect(today).toMatch(/^\d{4}-\d{2}-\d{2}$/);

    // A period cannot start in the future: today is offered, nothing after it
    // is. Enumerating the enabled cells proves both halves at once.
    const selectableDays = await picker
      .locator('[data-onboarding-day-option]:not([disabled])')
      .evaluateAll((nodes) => nodes.map((node) => node.getAttribute('data-onboarding-day-value')));
    expect(selectableDays).toContain(today);
    expect(selectableDays.filter((value) => String(value) > today)).toEqual([]);

    // Shortcut buttons, read from the catalogue rather than re-typed.
    await expect(onboardingShortcut(page, 'today')).toHaveText(
      localeText('en', 'onboarding.step1.today')
    );
    await expect(onboardingShortcut(page, 'yesterday')).toHaveText(
      localeText('en', 'onboarding.step1.yesterday')
    );

    // Grid cells are real buttons with the date as their accessible name and a
    // tap target well above the 24 px floor.
    //
    // This runs BEFORE the yesterday shortcut on purpose. Selecting a date moves
    // the grid to that date's month, so on the first of a month "yesterday" is
    // the previous month and today's cell leaves the DOM entirely — the grid
    // renders one month, without the neighbouring days. Asserted after the
    // shortcut, this block passed on 30 or 31 days of the month and failed on
    // the first, which is how it went green for months and then broke on
    // 2026-09-01 with no code change behind it.
    const todayCell = onboardingDayCell(page, today);
    expect(await todayCell.evaluate((node) => node.tagName)).toBe('BUTTON');
    const expectedName = await page.evaluate(
      (value) =>
        new Intl.DateTimeFormat('en', { day: 'numeric', month: 'long', year: 'numeric' }).format(
          new Date(`${value}T00:00:00`)
        ),
      today
    );
    await expect(todayCell).toHaveAttribute('aria-label', expectedName);

    const cellBox = await todayCell.boundingBox();
    expect(cellBox!.width).toBeGreaterThanOrEqual(24);
    expect(cellBox!.height).toBeGreaterThanOrEqual(24);

    const yesterday = shiftISODate(today, -1);
    await onboardingShortcut(page, 'yesterday').focus();
    await page.keyboard.press('Enter');
    await expect(transport).toHaveValue(yesterday);
    await expect(onboardingDayCell(page, yesterday)).toHaveAttribute('aria-pressed', 'true');
    // The grid followed the selection: on the first of a month that is the
    // previous month, and the shortcut must still land on the right day.
    await expect(picker).toHaveAttribute('data-onboarding-visible-month', yesterday.slice(0, 7));

    await selectOnboardingStartDate(page, today);
    await onboardingStepOneSubmit(page).click();
    await expect(onboardingStepTwoForm(page)).toBeVisible();
  });

  test('step 1 selected day paints a solid fill that clears AA in both themes', async ({ page }) => {
    await registerAndOpenOnboarding(page, 'onboarding-step1-selected-contrast');
    const today = String(await page.locator('#last-period-start').getAttribute('max'));

    for (const theme of ['light', 'dark'] as const) {
      // applyTheme reloads, and an unsubmitted selection does not survive that,
      // so the date is re-picked inside each theme rather than once up front.
      await applyTheme(page, theme);
      await selectOnboardingStartDate(page, today);

      const [selected] = await expectTextContrastAA(
        page,
        '.onboarding-day-cell-selected',
        `onboarding selected day cell (${theme})`
      );
      // A selected state is neither a progression nor an uncertainty, so it
      // carries no gradient — and a background-image is exactly what automated
      // engines report as incomplete rather than as a violation.
      expect(selected.backgroundImage, `selected day cell (${theme})`).toBe('none');
    }
  });

  test('step 1 picker fits a 390 px viewport without covering Next', async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 });
    await registerAndOpenOnboarding(page, 'onboarding-step1-mobile-fit');

    const picker = onboardingPicker(page);
    const nextButton = onboardingStepOneSubmit(page);
    await expect(picker).toBeVisible();
    await expect(nextButton).toBeVisible();
    await expect(nextButton).toBeEnabled();

    const pickerBox = (await picker.boundingBox())!;
    const nextBox = (await nextButton.boundingBox())!;

    // Fits the viewport horizontally — the shared helper names the offending
    // element instead of only reporting the picker's own box — is not clipped
    // vertically, and the Next button starts below it, not on top of it.
    await assertNoHorizontalOverflow(page);
    expect(nextBox.y).toBeGreaterThanOrEqual(pickerBox.y + pickerBox.height);
    expect(
      await picker.evaluate((node) => node.scrollHeight - node.clientHeight)
    ).toBeLessThanOrEqual(1);

    // Playwright's actionability check hits the real element under the pointer,
    // so a Next covered by the picker would fail this click rather than pass it.
    const today = String(await page.locator('#last-period-start').getAttribute('max'));
    await selectOnboardingStartDate(page, shiftISODate(today, -2));
    await nextButton.click();
    await expect(onboardingStepTwoForm(page)).toBeVisible();
  });

  test('today shortcut keeps the exact selected date through onboarding completion', async ({
    page,
  }) => {
    await registerAndOpenOnboarding(page, 'onboarding-step1-today-persist');

    const selectedValue = await page.locator('#last-period-start').getAttribute('max');
    expect(selectedValue).toMatch(/^\d{4}-\d{2}-\d{2}$/);

    await activateOnboardingShortcut(page, 'today');
    await expect(page.locator('#last-period-start')).toHaveValue(String(selectedValue));
    await onboardingStepOneSubmit(page).click();
    await expect(onboardingStepTwoForm(page)).toBeVisible();
    await submitStepTwo(page);

    await page.goto('/settings');
    await expect(page).toHaveURL(/\/settings$/);
    await expect(page.locator('#settings-last-period-start')).toHaveValue(String(selectedValue));
  });

  test('russian onboarding shortcuts stay localized', async ({
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

    // Russian copy IS this test's subject, so the strings are asserted — but
    // read from ru.json rather than re-typed here, so a translation edit syncs
    // itself instead of failing a test that only looks like a UI regression.
    await expect(onboardingShortcut(page, 'today')).toHaveText(
      localeText('ru', 'onboarding.step1.today')
    );
    await expect(onboardingShortcut(page, 'yesterday')).toHaveText(
      localeText('ru', 'onboarding.step1.yesterday')
    );
    await expect(onboardingPicker(page).locator('[data-onboarding-month-prev]')).toHaveAttribute(
      'aria-label',
      localeText('ru', 'onboarding.step1.previous_month')
    );

    // Weekday headers and the month title come from the browser's locale data
    // for the page language, so they must not read as English.
    const monthTitle = await onboardingPicker(page)
      .locator('[data-onboarding-month-title]')
      .textContent();
    expect(String(monthTitle)).toMatch(/[а-яА-Я]/);
  });

  test('step 1 offers no out-of-range date and the server still rejects one', async ({ page }) => {
    await registerAndOpenOnboarding(page, 'onboarding-step1-bounds');

    const input = page.locator('#last-period-start');
    const min = String(await input.getAttribute('min'));
    const max = String(await input.getAttribute('max'));

    expect(min).toMatch(/^\d{4}-\d{2}-\d{2}$/);
    expect(max).toMatch(/^\d{4}-\d{2}-\d{2}$/);
    expect(min <= max).toBe(true);

    // The picker cannot page past the months the range covers, and the cells
    // outside it are rendered inert rather than omitted, so an owner can never
    // hand the server a date it would reject.
    const picker = onboardingPicker(page);
    for (const direction of ['prev', 'next'] as const) {
      const monthButton = picker.locator(`[data-onboarding-month-${direction}]`);
      for (let step = 0; step < 6; step += 1) {
        if (!(await monthButton.isEnabled())) {
          break;
        }
        await monthButton.click();
      }
      await expect(monthButton).toBeDisabled();

      const selectable = await picker
        .locator('[data-onboarding-day-option]:not([disabled])')
        .evaluateAll((nodes) => nodes.map((node) => node.getAttribute('data-onboarding-day-value')));
      expect(selectable.filter((value) => String(value) < min || String(value) > max)).toEqual([]);
    }
    await expect(
      page.locator(
        `[data-onboarding-day-option][data-onboarding-day-value="${shiftISODate(max, 1)}"]:not([disabled])`
      )
    ).toHaveCount(0);

    // The server bound is the one that matters, and no browser UI is between it
    // and this call: a forced out-of-range post is answered 400, twice.
    const csrfToken = String(await page.locator('meta[name="csrf-token"]').getAttribute('content'));
    for (const outOfRange of [shiftISODate(min, -1), shiftISODate(max, 1)]) {
      const response = await page.request.post('/api/v1/onboarding/steps/1', {
        headers: { ...apiOriginHeader(page), 'HX-Request': 'true' },
        form: { csrf_token: csrfToken, last_period_start: outOfRange },
        maxRedirects: 0,
      });
      expect(response.status(), `expected 400 for ${outOfRange}`).toBe(400);
    }

    await page.reload();
    await expect(onboardingPicker(page)).toBeVisible();
    await expect(page.locator('#last-period-start')).toHaveValue('');
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
    // Auto-fill now ships OFF, so the state this test has to prove the toggle
    // reaches — and that Back preserves — is ON. Unchecking an already-unchecked
    // box asserts nothing: every expectation below would stay green with the
    // toggle's wiring removed entirely.
    await autoFillCheckbox.check();

    await expect(cycleSlider).toHaveValue('35');
    await expect(periodSlider).toHaveValue('6');
    await expect(autoFillCheckbox).toBeChecked();
    await expect(irregularCheckbox).not.toBeChecked();
    await expect(autoFillToggle).toHaveAttribute('data-active', 'true');
    await expect(irregularToggle).toHaveAttribute('data-active', 'false');

    await onboardingStepTwoBackButton(page).click();

    const stepOneInput = page.locator('#last-period-start');
    await expect(onboardingPicker(page)).toBeVisible();
    await expect(stepOneInput).toHaveValue(selectedDate);
    await expect(onboardingDayCell(page, selectedDate)).toHaveAttribute('aria-pressed', 'true');

    await onboardingStepOneSubmit(page).click();
    await expect(onboardingStepTwoForm(page)).toBeVisible();

    await expect(cycleSlider).toHaveValue('35');
    await expect(periodSlider).toHaveValue('6');
    await expect(autoFillCheckbox).toBeChecked();
    await expect(autoFillToggle).toHaveAttribute('data-active', 'true');

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

  test('reload during onboarding keeps submitted progress and resets the unsaved step 2 draft', async ({
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

    // Both halves of the reload behaviour are server-decided, so neither is
    // adaptive. Step 1 posted its date, so BuildOnboardingViewState reopens the
    // flow on step 2 with that date still filled. Step 2 has no draft store of
    // any kind — the slider re-renders from the persisted user record, so the
    // unsubmitted 32 is deliberately discarded back to the default 28.
    await expect(onboardingStepTwoForm(page)).toBeVisible();
    await expect(page.locator('#last-period-start')).toHaveValue(startDate);
    await expect(cycleSlider).toHaveValue('28');

    await submitStepTwo(page);
  });

  // The name says "sparse estimate", not "range", because that is what the
  // assertions below prove. Irregular mode shows a range only once the account
  // has observed cycles to bound it; a freshly onboarded owner has none, so the
  // dashboard falls back to the "around <date>" estimate plus the
  // needs-more-cycles note. A name promising a range over assertions checking
  // the sparse fallback is the desync that let its twin in
  // settings-profile-cycle.spec.ts assert an ASCII "-" a locale renders as "—".
  test('step 2 irregular checkbox carries through to the dashboard sparse estimate', async ({
    page,
  }) => {
    await registerAndOpenOnboarding(page, 'onboarding-irregular');

    const selectedDate = toISODate(new Date(Date.now() - 5 * 24 * 60 * 60 * 1000));
    await submitStepOne(page, selectedDate);

    const irregularCheckbox = onboardingStepTwoForm(page).locator('input[name="irregular_cycle"]');
    await irregularCheckbox.check();
    await submitStepTwo(page);

    // Sparse irregular mode: an "around <date>" estimate plus the
    // needs-more-cycles note, both sourced from the catalogue rather than
    // re-typed here (the same strings are asserted in three specs).
    const nextPeriodText = await dashboardNextPeriodText(page);
    expect(nextPeriodText).toContain(localeText('en', 'dashboard.next_period_estimate').replace('%s', '').trim());
    expect(nextPeriodText).toContain(localeText('en', 'dashboard.next_period_need_cycles'));
    await expect(page.locator('[data-dashboard-prediction-explainer]')).toHaveAttribute(
      'data-explainer-key',
      'prediction.explainer.irregular_sparse'
    );
  });

  test('step 2 asks no age question and the mode question is skippable in one visible action', async ({
    page,
  }) => {
    await registerAndOpenOnboarding(page, 'onboarding-step2-skip-mode');

    const selectedDate = toISODate(new Date(Date.now() - 4 * 24 * 60 * 60 * 1000));
    await submitStepOne(page, selectedDate);

    const stepTwo = onboardingStepTwoForm(page);
    // Age left onboarding entirely; the mode question stayed.
    await expect(stepTwo.locator('input[name="age_group"]')).toHaveCount(0);
    await expect(stepTwo.locator('input[name="usage_goal"]')).toHaveCount(3);

    const skip = stepTwo.locator('[data-onboarding-usage-goal-skip]');
    await expect(skip).toBeVisible();
    await expect(skip).toHaveText(localeText('en', 'onboarding.step2.usage_goal_skip'));

    // An answer is picked and then abandoned: skipping must submit no mode at
    // all, not the last radio that happened to be selected.
    await stepTwo
      .locator('label.choice-option:has(input[name="usage_goal"][value="avoid_pregnancy"])')
      .click();
    await expect(stepTwo.locator('input[name="usage_goal"][value="avoid_pregnancy"]')).toBeChecked();

    await Promise.all([page.waitForURL(/\/dashboard$/, { timeout: 15000 }), skip.click()]);

    await expect(page.locator('[data-usage-goal-summary]')).toHaveAttribute(
      'data-usage-goal-label-key',
      'settings.goal.health'
    );
  });

  test('step 1 surfaces the day-1 spotting clarification tip above the date field', async ({
    page,
  }) => {
    // onboarding.step1.day1_tip is rendered unconditionally between the
    // subtitle and the privacy line inside [data-onboarding-panel="1"]. Scope
    // the assertion to that panel so a future cross-step rewrite cannot let
    // the tip silently migrate to step 2.
    await registerAndOpenOnboarding(page, 'onboarding-day1-tip');

    const stepOnePanel = page.locator('[data-onboarding-panel="1"]');
    await expect(stepOnePanel).toBeVisible();
    await expect(stepOnePanel).toContainText(localeText('en', 'onboarding.step1.day1_tip'));
  });
});
