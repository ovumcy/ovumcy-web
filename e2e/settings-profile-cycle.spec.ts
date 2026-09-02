import { expect, test, type Locator, type Page } from './support/fixtures';
import { fillDateField } from './support/date-field-helpers';
import {
  dashboardNextPeriodText,
  openDashboardMoreFields,
  saveDashboardEntry,
} from './support/dashboard-helpers';
import { selectOnboardingStartDate } from './support/onboarding-helpers';
import { checkStyledControl } from './support/form-helpers';
import {
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  readRecoveryCode,
  registerOwnerViaUI,
} from './support/auth-helpers';
import { assertNoHorizontalOverflow } from './support/mobile-layout-helpers';
import { openCalendarDayEditor } from './support/stats-helpers';
import {
  cancelConfirmDialog,
  expectConfirmDialogCaptions,
  mutatingRequestsDuring,
} from './support/confirm-dialog-helpers';
import { localeText } from './support/locale-helpers';

// The duplicate-name rejection is asserted on three surfaces in this file (the
// create form twice, an archived row once). Source it once from the catalogue
// so a copy edit lands in all three without a search-and-replace.
const DUPLICATE_SYMPTOM_NAME_ERROR = localeText('en', 'settings.symptoms.error.duplicate_name');

function toISODate(date: Date): string {
  const copy = new Date(date);
  copy.setHours(0, 0, 0, 0);
  const yyyy = copy.getFullYear();
  const mm = String(copy.getMonth() + 1).padStart(2, '0');
  const dd = String(copy.getDate()).padStart(2, '0');
  return `${yyyy}-${mm}-${dd}`;
}

function isoDaysAgo(days: number): string {
  return toISODate(new Date(Date.now() - days * 24 * 60 * 60 * 1000));
}

function isoDaysFromNow(days: number): string {
  return toISODate(new Date(Date.now() + days * 24 * 60 * 60 * 1000));
}

async function isoDaysAgoInBrowser(page: Page, days: number): Promise<string> {
  return page.evaluate((offset) => {
    const date = new Date();
    date.setHours(0, 0, 0, 0);
    date.setDate(date.getDate() - offset);

    const yyyy = date.getFullYear();
    const mm = String(date.getMonth() + 1).padStart(2, '0');
    const dd = String(date.getDate()).padStart(2, '0');
    return `${yyyy}-${mm}-${dd}`;
  }, days);
}

async function browserTimezone(page: Page): Promise<string> {
  return page.evaluate(() => {
    try {
      return String(Intl.DateTimeFormat().resolvedOptions().timeZone || '').trim();
    } catch {
      return '';
    }
  });
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

async function selectSymptomIcon(root: Locator, icon: string): Promise<void> {
  const control = root.locator('[data-icon-control]');
  await control.locator(`[data-icon-option="${icon}"]`).click();
  await expect(control.locator('[data-icon-value]')).toHaveValue(icon);
}

async function assertSelectedSymptomChipHasNoTrailingMarker(chip: Locator): Promise<void> {
  const afterContent = await chip.evaluate((node) => window.getComputedStyle(node, '::after').content);
  expect(['none', 'normal', ''].includes(afterContent.replace(/"/g, ''))).toBe(true);
}

async function ensureSymptomInputVisible(root: Locator, symptomName: string): Promise<Locator> {
  const input = root.locator(`input[name="symptom_ids"][data-symptom-name="${symptomName}"]`);
  const visibleChip = root.locator(
    `label.choice-option:has(input[name="symptom_ids"][data-symptom-name="${symptomName}"])`
  );
  const visible = await visibleChip.isVisible().catch(() => false);
  if (!visible) {
    const moreSummary = root.locator('[data-symptom-more-toggle]');
    if (await moreSummary.isVisible().catch(() => false)) {
      await moreSummary.click();
    }
  }
  await expect(visibleChip).toBeVisible();
  return input;
}

function dashboardSaveForm(page: Page): Locator {
  return page.locator('[data-dashboard-save-form]').first();
}

function navIdentityChip(page: Page): Locator {
  return page.locator('#nav-user-chip-desktop');
}

async function registerOwnerAndOpenSettings(page: Page, prefix: string) {
  const creds = createCredentials(prefix);

  await registerOwnerViaUI(page, creds);
  await expectInlineRegisterRecoveryStep(page);

  await readRecoveryCode(page);
  await continueFromRecoveryCode(page);
  await completeOnboardingIfPresent(page);

  await page.goto('/settings');
  await expect(page).toHaveURL(/\/settings$/);

  return creds;
}

function customSymptomRow(root: Locator, name: string, state: 'active' | 'archived'): Locator {
  return root.locator(`[data-custom-symptom-row][data-symptom-name="${name}"][data-symptom-state="${state}"]`);
}

async function createCustomSymptom(symptomSection: Locator, name: string, icon: string): Promise<void> {
  const createForm = symptomSection.locator('[data-symptom-create-form]');
  await createForm.locator('#settings-new-symptom-name').fill(name);
  await selectSymptomIcon(createForm, icon);
  await createForm.locator('button[type="submit"]').click();
  await expect(symptomSection.locator('.status-ok')).toBeVisible();
}

async function archiveCustomSymptom(page: Page, row: Locator): Promise<void> {
  await row.locator('form[hx-delete^="/api/v1/symptoms/"] button[type="submit"]').click();
  await expect(page.locator('#confirm-modal')).toBeVisible();
  await page.locator('#confirm-modal-accept').click();
}

async function saveTodayWithSymptom(page: Page, symptomName: string): Promise<string> {
  await page.goto('/dashboard');
  await expect(page).toHaveURL(/\/dashboard$/);

  await saveDashboardEntry(page, async () => {
    await page.locator('input[name="is_period"]').check();
    const customSymptom = await ensureSymptomInputVisible(dashboardSaveForm(page), symptomName);
    await checkStyledControl(customSymptom);
  });

  const todayAction = await page
    .locator('[data-dashboard-save-form]')
    .first()
    .getAttribute('hx-put');
  expect(todayAction).toMatch(/^\/api\/v1\/days\/\d{4}-\d{2}-\d{2}$/);
  return String(todayAction).replace('/api/v1/days/', '');
}

async function completeOnboardingWithStartDate(page: Page, startDate: string): Promise<void> {
  await selectOnboardingStartDate(page, startDate);
  await page.locator('form[hx-post="/api/v1/onboarding/steps/1"] button[type="submit"]').click();

  const stepTwoForm = page.locator('form[hx-post="/api/v1/onboarding/steps/2"]');
  await expect(stepTwoForm).toBeVisible();
  await stepTwoForm.locator('[data-onboarding-step2-submit]').click();
  await expect(page).toHaveURL(/\/dashboard(?:\?.*)?$/);
}

test.describe('Settings: profile and cycle', () => {
  test('profile name persists, rejects invalid markup and long values, and empty clears without header fallback', async ({
    page,
  }) => {
    const creds = await registerOwnerAndOpenSettings(page, 'settings-profile');

    // The email is displayed, never editable. Assert that structurally: the
    // panel renders the address as static text and carries no editable control
    // at all. The previous negated-copy checks ("Cannot be changed." and its ru
    // variant) passed just as well when the copy was reworded or dropped —
    // which is exactly what happened, the string no longer renders anywhere.
    const profileAccountPanel = page.locator('[data-profile-email-panel]');
    await expect(profileAccountPanel).toContainText(creds.email);
    await expect(profileAccountPanel.locator('input, textarea, select, [contenteditable]')).toHaveCount(0);
    await expect(page.locator('#settings-account input#settings-profile-email')).toHaveCount(0);

    const displayNameInput = page.locator('#settings-display-name');
    await expect(displayNameInput).toHaveAttribute('maxlength', '64');
    const saveProfileButton = page.locator(
      'form[action="/api/v1/users/current/profile"] button[data-save-button]'
    );

    const newName = `Profile-${Date.now()}-ABCDEFGHIJKLMNOPQRSTUVWXYZ-1234567890`.slice(0, 64);
    await displayNameInput.fill(newName);
    await saveProfileButton.click();
    await expect(page.locator('#settings-profile-status .status-ok')).toBeVisible();

    await page.reload();
    await expect(page).toHaveURL(/\/settings$/);
    await expect(displayNameInput).toHaveValue(newName);
    await expect(navIdentityChip(page)).toContainText(newName);
    await expect(navIdentityChip(page)).not.toContainText(creds.email);
    await expect(navIdentityChip(page)).not.toContainText(creds.email.split('@')[0]);
    const identityTextStyles = await navIdentityChip(page)
      .locator('[data-current-user-identity]')
      .evaluate((node) => {
        const styles = window.getComputedStyle(node);
        return {
          overflow: styles.overflow,
          textOverflow: styles.textOverflow,
          whiteSpace: styles.whiteSpace,
        };
      });
    expect(identityTextStyles.overflow).toBe('hidden');
    expect(identityTextStyles.textOverflow).toBe('ellipsis');
    expect(identityTextStyles.whiteSpace).toBe('nowrap');

    await displayNameInput.fill("<script>alert('xss')</script>");
    await saveProfileButton.click();
    await expect(page.locator('#settings-profile-status .status-error')).toBeVisible();

    await page.reload();
    await expect(page).toHaveURL(/\/settings$/);
    await expect(displayNameInput).toHaveValue(newName);
    await expect(navIdentityChip(page)).toContainText(newName);

    await displayNameInput.evaluate((el) => {
      (el as HTMLInputElement).value = 'X'.repeat(80);
    });
    await saveProfileButton.click();
    await expect(page.locator('#settings-profile-status .status-error')).toBeVisible();

    await displayNameInput.fill('');
    await saveProfileButton.click();
    await expect(page.locator('#settings-profile-status .status-ok')).toBeVisible();

    await page.reload();
    await expect(displayNameInput).toHaveValue('');
    // With no display name the chip falls back to the profile-name hint. Pin
    // the key's rendered value from the catalogue rather than an English
    // literal — the chip is localized like everything else.
    await expect(navIdentityChip(page)).toHaveAttribute(
      'title',
      localeText('en', 'nav.profile_name_hint')
    );
    await expect(navIdentityChip(page)).not.toContainText(creds.email);
    await expect(navIdentityChip(page)).not.toContainText(creds.email.split('@')[0]);
  });

  test('cycle settings persist, affect dashboard predictions, and reject future last-period date', async ({
    page,
  }) => {
    await registerOwnerAndOpenSettings(page, 'settings-cycle');

    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);
    const nextPeriodBefore = await dashboardNextPeriodText(page);

    await page.goto('/settings');
    await expect(page).toHaveURL(/\/settings$/);

    const cycleForm = page.locator('#settings-cycle form[action="/api/v1/users/current/cycle"]');
    await expect(cycleForm).toBeVisible();

    const cycleLength = cycleForm.locator('#settings-cycle-length');
    const periodLength = cycleForm.locator('#settings-period-length');
    const lastPeriodStart = cycleForm.locator('#settings-last-period-start');
    const autoFill = cycleForm.locator('input[name="auto_period_fill"]');

    const targetCycleLength = 35;
    const targetPeriodLength = 6;
    const targetStart = isoDaysAgo(20);

    await setRangeValue(cycleLength, targetCycleLength);
    await setRangeValue(periodLength, targetPeriodLength);
    await fillDateField(lastPeriodStart, targetStart);
    await autoFill.uncheck();

    await cycleForm.locator('button[data-save-button]').click();
    await expect(page.locator('#settings-cycle-status .status-ok')).toBeVisible();

    await page.reload();
    await expect(page).toHaveURL(/\/settings$/);

    await expect(page.locator('#settings-cycle-length')).toHaveValue(String(targetCycleLength));
    await expect(page.locator('#settings-period-length')).toHaveValue(String(targetPeriodLength));
    await expect(page.locator('#settings-last-period-start')).toHaveValue(targetStart);
    await expect(page.locator('#settings-cycle input[name="auto_period_fill"]')).not.toBeChecked();

    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);
    const nextPeriodAfter = await dashboardNextPeriodText(page);
    expect(nextPeriodAfter).not.toBe(nextPeriodBefore);

    await page.goto('/calendar');
    await expect(page).toHaveURL(/\/calendar(?:\?.*)?$/);
    await expect(page.locator('#calendar-grid-panel')).toBeVisible();

    await page.goto('/settings');
    await expect(page).toHaveURL(/\/settings$/);

    await fillDateField(page.locator('#settings-last-period-start'), isoDaysFromNow(1));
    await page
      .locator('#settings-cycle form[action="/api/v1/users/current/cycle"] button[data-save-button]')
      .click();

    await expect(page.locator('#settings-cycle-status .status-error')).toBeVisible();
  });

  test('irregular cycle toggle switches dashboard prediction to a pending-cycles estimate', async ({ page }) => {
    await registerOwnerAndOpenSettings(page, 'settings-irregular-cycle');

    const cycleForm = page.locator('#settings-cycle form[action="/api/v1/users/current/cycle"]');
    await expect(cycleForm).toBeVisible();

    const irregularToggle = cycleForm.locator('input[name="irregular_cycle"]');
    await irregularToggle.check();
    await cycleForm.locator('button[data-save-button]').click();
    await expect(page.locator('#settings-cycle-status .status-ok')).toBeVisible();

    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);

    // A freshly onboarded owner has fewer than 3 completed cycles, so irregular
    // mode renders the "around <date>" estimate, not a range:
    // dashboardNeedsNextPeriodData short-circuits before the range is applied.
    const nextPeriodText = await dashboardNextPeriodText(page);
    expect(nextPeriodText).toContain(
      localeText('en', 'dashboard.next_period_estimate').replace('%s', '').trim()
    );
    expect(nextPeriodText).toContain(localeText('en', 'dashboard.next_period_need_cycles'));
    // Sparse irregular mode, asserted on the explainer key rather than by
    // forbidding the exact-date separator this state must not render.
    await expect(page.locator('[data-dashboard-prediction-explainer]')).toHaveAttribute(
      'data-explainer-key',
      'prediction.explainer.irregular_sparse'
    );
  });

  test('tracking toggles and BBT unit persist and change the owner day form', async ({ page }) => {
    await registerOwnerAndOpenSettings(page, 'settings-tracking');

    const trackingSection = page.locator('#settings-tracking');
    await expect(trackingSection).toBeVisible();

    const trackBBT = trackingSection.locator('input[name="track_bbt"]');
    const trackCervicalMucus = trackingSection.locator('input[name="track_cervical_mucus"]');
    const showSexChip = trackingSection.locator('input[name="show_sex_chip"]');
    const trackBBTToggle = trackingSection.locator('[data-tracking-setting="track-bbt"]');
    const trackCervicalMucusToggle = trackingSection.locator('[data-tracking-setting="track-cervical-mucus"]');
    const showSexChipToggle = trackingSection.locator('[data-tracking-setting="show-sex-chip"]');
    const temperatureUnitFahrenheit = trackingSection.locator('input[name="temperature_unit"][value="f"]');
    const saveTrackingButton = trackingSection.locator('button[data-save-button]');

    // Every toggle in this section is phrased positively: checked means the
    // field is shown. The intimacy section is visible by default, so its box
    // starts checked while the two opt-in fields start unchecked.
    //
    // The state each toggle is in is read off `data-active`, which the same
    // handler writes: the sentence that used to restate it under every toggle
    // ("Currently visible in …") is no longer rendered — a switch showing its
    // own state does not need a line saying so.
    //
    // Nor is the helper line that followed it: it opened by restating the
    // toggle's own label and closed by repeating, word for word, the section
    // subtitle's promise that turning a field off never removes what is
    // already logged. What each toggle must still carry is its LABEL, and it
    // is read from the catalogue — this block previously re-typed the helper
    // sentences as English literals, so the copy could be deleted from all six
    // catalogues with every key-based search coming back clean.
    await expect(trackBBT).not.toBeChecked();
    await expect(trackCervicalMucus).not.toBeChecked();
    await expect(showSexChip).toBeChecked();
    await expect(trackBBTToggle).toHaveAttribute('data-active', 'false');
    await expect(trackCervicalMucusToggle).toHaveAttribute('data-active', 'false');
    await expect(showSexChipToggle).toHaveAttribute('data-active', 'true');
    await expect(trackBBTToggle).toContainText(localeText('en', 'settings.tracking.track_bbt'));
    await expect(trackCervicalMucusToggle).toContainText(
      localeText('en', 'settings.tracking.track_cervical_mucus')
    );
    await expect(showSexChipToggle).toContainText(
      localeText('en', 'settings.tracking.show_sex_chip')
    );

    await trackBBT.check();
    await trackCervicalMucus.check();
    await showSexChip.uncheck();
    await expect(trackBBTToggle).toHaveAttribute('data-active', 'true');
    await expect(trackCervicalMucusToggle).toHaveAttribute('data-active', 'true');
    await expect(showSexChipToggle).toHaveAttribute('data-active', 'false');
    await checkStyledControl(temperatureUnitFahrenheit);
    await saveTrackingButton.click();
    await expect(page.locator('#settings-tracking-status .status-ok')).toBeVisible();

    await page.reload();
    await expect(page).toHaveURL(/\/settings$/);
    await expect(trackBBT).toBeChecked();
    await expect(trackCervicalMucus).toBeChecked();
    await expect(showSexChip).not.toBeChecked();
    await expect(trackBBTToggle).toHaveAttribute('data-active', 'true');
    await expect(trackCervicalMucusToggle).toHaveAttribute('data-active', 'true');
    await expect(showSexChipToggle).toHaveAttribute('data-active', 'false');
    await expect(temperatureUnitFahrenheit).toBeChecked();

    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);
    const dashboardForm = page.locator('[data-dashboard-save-form]').first();
    // BBT, cervical mucus and intimacy sit behind the journal's "More"
    // disclosure; a day with none of them recorded renders it closed.
    await openDashboardMoreFields(page);
    // Address the field by its stable id, not its localized label ("BBT" is
    // «БТТ» in ru), the way every other field in this spec is addressed.
    const bbtInput = dashboardForm.locator('#dashboard-bbt');
    await expect(bbtInput).toBeVisible();
    await expect(dashboardForm.locator('.measurement-field-unit')).toContainText('°F');
    await expect(dashboardForm).toContainText('93.2-109.4 °F');
    await bbtInput.fill('150.0');
    await bbtInput.blur();
    const invalidState = await bbtInput.evaluate((node) => {
      const input = node as HTMLInputElement;
      return {
        valid: input.checkValidity(),
        validationMessage: input.validationMessage,
      };
    });
    expect(invalidState.valid).toBe(false);
    expect(invalidState.validationMessage).not.toBe('');
    await expect(page.locator('[data-dashboard-save-form] input[name="cervical_mucus"][value="dry"]')).toBeVisible();
    await expect(page.locator('[data-dashboard-save-form] [data-sex-activity-details]')).toHaveCount(0);
    await saveDashboardEntry(page, async () => {
      await bbtInput.fill('98.6');
      await bbtInput.blur();
    });
    await expect(bbtInput).toHaveValue('98.60');

    const todayAction = await dashboardForm.getAttribute('hx-put');
    expect(todayAction).toMatch(/^\/api\/v1\/days\/\d{4}-\d{2}-\d{2}$/);
    const todayISO = String(todayAction).replace('/api/v1/days/', '');
    const dayEditorForm = await openCalendarDayEditor(page, todayISO);
    await expect(dayEditorForm.locator('#calendar-bbt')).toHaveValue('98.60');
    await expect(dayEditorForm.locator('.measurement-field-unit')).toContainText('°F');

    await page.goto('/settings');
    await expect(page).toHaveURL(/\/settings$/);
    await trackBBT.uncheck();
    await trackCervicalMucus.uncheck();
    await showSexChip.check();
    await expect(trackBBTToggle).toHaveAttribute('data-active', 'false');
    await expect(trackCervicalMucusToggle).toHaveAttribute('data-active', 'false');
    await expect(showSexChipToggle).toHaveAttribute('data-active', 'true');
    await saveTrackingButton.click();
    await expect(page.locator('#settings-tracking-status .status-ok')).toBeVisible();
    await expect(trackBBTToggle).toHaveAttribute('data-active', 'false');

    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);
    await expect(page.locator('[data-dashboard-save-form] input[name="bbt"]')).toHaveCount(0);
    await expect(page.locator('[data-dashboard-save-form] input[name="cervical_mucus"][value="dry"]')).toHaveCount(0);
    await openDashboardMoreFields(page);
    await expect(page.locator('[data-dashboard-save-form] [data-sex-activity-details]')).toBeVisible();
  });

  test('show-cycle-factors, show-notes-field, and show-historical-phases toggles persist and change the owner day form', async ({
    page,
  }) => {
    await registerOwnerAndOpenSettings(page, 'settings-tracking-extra');

    const trackingSection = page.locator('#settings-tracking');
    await expect(trackingSection).toBeVisible();

    const showCycleFactors = trackingSection.locator('input[name="show_cycle_factors"]');
    const showNotesField = trackingSection.locator('input[name="show_notes_field"]');
    const showHistoricalPhases = trackingSection.locator('input[name="show_historical_phases"]');
    const showCycleFactorsToggle = trackingSection.locator('[data-tracking-setting="show-cycle-factors"]');
    const showNotesFieldToggle = trackingSection.locator('[data-tracking-setting="show-notes-field"]');
    const showHistoricalPhasesToggle = trackingSection.locator('[data-tracking-setting="show-historical-phases"]');
    const saveTrackingButton = trackingSection.locator('button[data-save-button]');

    // Cycle factors and notes are visible by default, so their positive
    // toggles start checked; historical phases are opt-in.
    await expect(showCycleFactors).toBeChecked();
    await expect(showNotesField).toBeChecked();
    await expect(showHistoricalPhases).not.toBeChecked();
    await expect(showCycleFactorsToggle).toHaveAttribute('data-active', 'true');
    await expect(showNotesFieldToggle).toHaveAttribute('data-active', 'true');
    await expect(showHistoricalPhasesToggle).toHaveAttribute('data-active', 'false');

    await showCycleFactors.uncheck();
    await showNotesField.uncheck();
    await showHistoricalPhases.check();
    await expect(showCycleFactorsToggle).toHaveAttribute('data-active', 'false');
    await expect(showNotesFieldToggle).toHaveAttribute('data-active', 'false');
    await expect(showHistoricalPhasesToggle).toHaveAttribute('data-active', 'true');
    await saveTrackingButton.click();
    await expect(page.locator('#settings-tracking-status .status-ok')).toBeVisible();

    await page.reload();
    await expect(page).toHaveURL(/\/settings$/);
    await expect(showCycleFactors).not.toBeChecked();
    await expect(showNotesField).not.toBeChecked();
    await expect(showHistoricalPhases).toBeChecked();
    await expect(showCycleFactorsToggle).toHaveAttribute('data-active', 'false');
    await expect(showNotesFieldToggle).toHaveAttribute('data-active', 'false');
    await expect(showHistoricalPhasesToggle).toHaveAttribute('data-active', 'true');

    // Dashboard effect: an unchecked show-cycle-factors removes the
    // cycle-factor fieldset and an unchecked show-notes-field removes the notes
    // disclosure from the owner's day form, mirroring how the sibling tracking
    // toggles above assert show_sex_chip / track_cervical_mucus. The stored
    // columns stay inverted (hide_cycle_factors / hide_notes_field), so this
    // reload is also the browser-side proof that the settings render and the
    // day form agree on one conversion. show_historical_phases has no
    // dashboard/day-form surface (it only affects past-month calendar
    // rendering), so persistence above is its end-to-end coverage here; the
    // clear-data reset invariant is covered separately.
    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);
    await expect(page.locator('[data-dashboard-save-form] input[name="cycle_factor_keys"]')).toHaveCount(0);
    await expect(page.locator('[data-dashboard-save-form] [data-note-disclosure]')).toHaveCount(0);

    await page.goto('/settings');
    await expect(page).toHaveURL(/\/settings$/);
    await showCycleFactors.check();
    await showNotesField.check();
    await showHistoricalPhases.uncheck();
    await expect(showCycleFactorsToggle).toHaveAttribute('data-active', 'true');
    await expect(showNotesFieldToggle).toHaveAttribute('data-active', 'true');
    await expect(showHistoricalPhasesToggle).toHaveAttribute('data-active', 'false');
    await saveTrackingButton.click();
    await expect(page.locator('#settings-tracking-status .status-ok')).toBeVisible();
    await expect(showCycleFactorsToggle).toHaveAttribute('data-active', 'true');
    await expect(showNotesFieldToggle).toHaveAttribute('data-active', 'true');
    await expect(showHistoricalPhasesToggle).toHaveAttribute('data-active', 'false');

    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);
    await openDashboardMoreFields(page);
    await expect(page.locator('[data-dashboard-save-form] input[name="cycle_factor_keys"]').first()).toBeVisible();
    await expect(page.locator('[data-dashboard-save-form] [data-note-disclosure]')).toBeVisible();
  });

  test('cycle and tracking drafts discard unsaved changes before navigation', async ({ page }) => {
    await registerOwnerAndOpenSettings(page, 'settings-drafts');

    const cycleForm = page.locator('#settings-cycle form[data-settings-draft-form="cycle"]');
    const trackingForm = page.locator('#settings-tracking form[data-settings-draft-form="tracking"]');
    const cycleLength = cycleForm.locator('#settings-cycle-length');
    const cycleSave = cycleForm.locator('[data-settings-cycle-save]');
    const cycleDiscard = cycleForm.locator('[data-settings-cycle-discard]');
    const trackingSave = trackingForm.locator('[data-settings-tracking-save]');
    const trackingDiscard = trackingForm.locator('[data-settings-tracking-discard]');
    const trackBBT = trackingForm.locator('input[name="track_bbt"]');

    await expect(cycleSave).toBeDisabled();
    await expect(cycleDiscard).toBeDisabled();
    await expect(trackingSave).toBeDisabled();
    await expect(trackingDiscard).toBeDisabled();

    const initialCycleLength = Number(await cycleLength.inputValue());
    await setRangeValue(cycleLength, initialCycleLength + 4);
    await expect(cycleSave).toBeEnabled();
    await expect(cycleDiscard).toBeEnabled();

    await trackBBT.check();
    await expect(trackingSave).toBeEnabled();
    await expect(trackingDiscard).toBeEnabled();

    await page.locator('a[href="/calendar"]').first().click();
    await expect(page.locator('#confirm-modal')).toBeVisible();
    // Compare the dialog against the prompt the form itself declared — the same
    // pattern expectConfirmDialogCaptions uses for the accept/cancel captions.
    // Asserting the declaration is non-empty first stops an empty attribute
    // from making the comparison pass against an empty dialog.
    const declaredUnsavedPrompt = (
      (await cycleForm.getAttribute('data-settings-unsaved-prompt')) ?? ''
    ).trim();
    expect(
      declaredUnsavedPrompt,
      'the draft form must declare data-settings-unsaved-prompt'
    ).not.toBe('');
    await expect(page.locator('#confirm-modal-message')).toContainText(declaredUnsavedPrompt);
    await page.locator('#confirm-modal-cancel').click();
    await expect(page).toHaveURL(/\/settings$/);
    await expect(cycleLength).toHaveValue(String(initialCycleLength + 4));
    await expect(trackBBT).toBeChecked();

    await cycleDiscard.click();
    await expect(cycleLength).toHaveValue(String(initialCycleLength));
    await expect(cycleSave).toBeDisabled();
    await expect(cycleDiscard).toBeDisabled();
    await expect(trackingSave).toBeEnabled();
    await expect(trackingDiscard).toBeEnabled();

    await page.locator('a[href="/calendar"]').first().click();
    await expect(page.locator('#confirm-modal')).toBeVisible();
    await page.locator('#confirm-modal-accept').click();
    await expect(page).toHaveURL(/\/calendar(?:\?.*)?$/);

    await page.goto('/settings');
    await expect(page).toHaveURL(/\/settings$/);
    await expect(page.locator('#settings-cycle-length')).toHaveValue(String(initialCycleLength));
    await expect(page.locator('#settings-tracking input[name="track_bbt"]')).not.toBeChecked();
    await expect(page.locator('[data-settings-tracking-save]')).toBeDisabled();
    await expect(page.locator('[data-settings-tracking-discard]')).toBeDisabled();
  });

  test('onboarding selected start date persists into settings cycle field', async ({ page }) => {
    const creds = createCredentials('settings-onboarding-date');

    await registerOwnerViaUI(page, creds);
    await expectInlineRegisterRecoveryStep(page);

    await readRecoveryCode(page);
    await continueFromRecoveryCode(page);
    await expect(page).toHaveURL(/\/onboarding(?:\?.*)?$/);

    const selectedStart = await isoDaysAgoInBrowser(page, 9);
    await completeOnboardingWithStartDate(page, selectedStart);

    const expectedTimezone = await browserTimezone(page);
    const timezoneCookie = (await page.context().cookies()).find((cookie) => cookie.name === 'ovumcy_tz');
    expect(timezoneCookie?.value || '').toBe(expectedTimezone);

    await page.goto('/settings');
    await expect(page).toHaveURL(/\/settings$/);
    await expect(page.locator('#settings-last-period-start')).toHaveValue(selectedStart);
  });

  test('new custom symptoms stay visible in dashboard and calendar pickers without forcing extra expansion', async ({
    page,
  }) => {
    await registerOwnerAndOpenSettings(page, 'settings-custom-symptom-primary');

    const symptomSection = page.locator('#settings-symptoms');
    await createCustomSymptom(symptomSection, 'Joint stiffness', '✨');

    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);
    const dashboardSymptom = dashboardSaveForm(page).locator(
      'label.choice-option:has(input[name="symptom_ids"][data-symptom-name="Joint stiffness"])'
    );
    await expect(dashboardSymptom).toBeVisible();

    const todayAction = await dashboardSaveForm(page).getAttribute('hx-put');
    expect(todayAction).toMatch(/^\/api\/v1\/days\/\d{4}-\d{2}-\d{2}$/);
    const todayISO = String(todayAction).replace('/api/v1/days/', '');

    const dayEditorForm = await openCalendarDayEditor(page, todayISO);
    await expect(
      dayEditorForm.locator(
        'label.choice-option:has(input[name="symptom_ids"][data-symptom-name="Joint stiffness"])'
      )
    ).toBeVisible();
  });

  test('archiving a custom symptom keeps old entries while hiding it from new days', async ({
    page,
  }) => {
    await registerOwnerAndOpenSettings(page, 'settings-custom-symptoms');

    const symptomSection = page.locator('#settings-symptoms');
    await expect(symptomSection).toBeVisible();

    const createForm = symptomSection.locator('[data-symptom-create-form]');
    await expect(createForm.locator('[data-color-control]')).toHaveCount(0);
    await createCustomSymptom(symptomSection, 'Joint stiffness', '✨');
    await expect(customSymptomRow(symptomSection, 'Joint stiffness', 'active')).toBeVisible();

    const todayISO = await saveTodayWithSymptom(page, 'Joint stiffness');
    const otherISO = shiftISODate(todayISO, 3);

    await page.goto('/settings');
    await expect(page).toHaveURL(/\/settings$/);

    const activeRow = customSymptomRow(symptomSection, 'Joint stiffness', 'active');
    const saveButtonBox = await activeRow.locator('[data-symptom-edit-form] button[type="submit"]').boundingBox();
    const hideButtonBox = await activeRow.locator('form[hx-delete^="/api/v1/symptoms/"] button[type="submit"]').boundingBox();
    expect(saveButtonBox).not.toBeNull();
    expect(hideButtonBox).not.toBeNull();
    expect(hideButtonBox!.y).toBeGreaterThan(saveButtonBox!.y + 4);

    await archiveCustomSymptom(page, activeRow);
    await expect(customSymptomRow(symptomSection, 'Joint stiffness', 'archived').locator('.status-ok')).toBeVisible();
    await expect(symptomSection.locator('[data-symptom-empty-state="active"]')).toContainText(
      'No visible custom symptoms right now. Restore one below or add a new one above.'
    );
    await expect(symptomSection.locator('[data-symptom-group="archived"]')).toContainText(
      'Past logs keep them. Restore one when you want it back in the picker.'
    );

    await page.goto('/dashboard');
    const archivedDashboardSymptom = await ensureSymptomInputVisible(
      dashboardSaveForm(page),
      'Joint stiffness'
    );
    await expect(archivedDashboardSymptom).toBeChecked();

    await openCalendarDayEditor(page, otherISO);
    await expect(
      page.locator(`[data-day-editor-form][data-day-editor-date="${otherISO}"] input[name="symptom_ids"][data-symptom-name="Joint stiffness"]`)
    ).toHaveCount(0);
  });

  // Cancelling the confirmation must be inert. This is a real regression, not a
  // hypothetical: while the hide control carried `data-confirm`, htmx issued the
  // DELETE from its own listener on the form before the document-level
  // interceptor ever ran, so the dialog was decorative and Cancel archived the
  // symptom anyway. The control now uses `hx-confirm`, which htmx gates on. The
  // template-level guard against the whole class is
  // TestNoTemplateElementMixesHTMXRequestWithDataConfirm; the sibling
  // cancel-is-inert tests cover the other gated surfaces in calendar.spec.ts,
  // settings-calendar-feed.spec.ts, and settings-webhook.spec.ts.
  test('cancelling the hide confirmation keeps the custom symptom active', async ({ page }) => {
    await registerOwnerAndOpenSettings(page, 'settings-custom-symptom-hide-cancel');

    const symptomSection = page.locator('#settings-symptoms');
    await createCustomSymptom(symptomSection, 'Joint stiffness', '✨');

    const activeRow = customSymptomRow(symptomSection, 'Joint stiffness', 'active');
    await expect(activeRow).toBeVisible();
    const symptomID = await activeRow.getAttribute('data-symptom-id');
    expect(symptomID).toMatch(/^\d+$/);

    const hideForm = activeRow.locator('form[hx-delete^="/api/v1/symptoms/"]');
    const isSymptomMutation = (pathname: string) =>
      pathname.endsWith(`/api/v1/symptoms/${symptomID}`);

    const cancelledRequests = await mutatingRequestsDuring(page, isSymptomMutation, async () => {
      await hideForm.locator('button[type="submit"]').click();

      // The dialog must show the captions this surface declared, not a
      // hardcoded fallback: backend coverage only pins that they are declared.
      const captions = await expectConfirmDialogCaptions(page, hideForm);
      // This surface names its own action instead of reusing the layout-wide
      // delete wording, so the rendered caption also proves the per-control
      // attribute — not the `data-confirm-delete` fallback — drove the button.
      const layoutFallback = (
        (await page.locator('body').getAttribute('data-confirm-delete')) ?? ''
      ).trim();
      expect(layoutFallback).not.toBe('');
      expect(captions.accept).not.toBe(layoutFallback);

      await cancelConfirmDialog(page);

      // A reload is the concrete signal that any request the click was going to
      // issue has had its chance — no arbitrary timeout involved.
      await page.reload();
      await expect(page).toHaveURL(/\/settings$/);
    });
    expect(cancelledRequests, 'cancelling the hide must issue no request').toEqual([]);

    // Still active after the dialog and the reload: no archived group appeared,
    // so nothing was hidden behind the owner's back.
    await expect(customSymptomRow(symptomSection, 'Joint stiffness', 'active')).toBeVisible();
    await expect(customSymptomRow(symptomSection, 'Joint stiffness', 'archived')).toHaveCount(0);
    await expect(symptomSection.locator('[data-symptom-group="archived"]')).toHaveCount(0);

    // And still offered for new entries — the behaviour hiding would have taken
    // away, measured where the owner would notice it.
    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);
    await ensureSymptomInputVisible(dashboardSaveForm(page), 'Joint stiffness');

    // Positive anchor: the same control, accepted, really does hide it — so the
    // assertions above prove Cancel is inert, not that the control is dead.
    await page.goto('/settings');
    await expect(page).toHaveURL(/\/settings$/);
    await archiveCustomSymptom(page, customSymptomRow(symptomSection, 'Joint stiffness', 'active'));
    await expect(customSymptomRow(symptomSection, 'Joint stiffness', 'archived')).toBeVisible();
    await expect(customSymptomRow(symptomSection, 'Joint stiffness', 'active')).toHaveCount(0);
  });

  test('archived custom symptoms can be renamed, reject duplicates, and restore cleanly', async ({
    page,
  }) => {
    await registerOwnerAndOpenSettings(page, 'settings-custom-symptoms-restore');

    const symptomSection = page.locator('#settings-symptoms');
    await expect(symptomSection).toBeVisible();

    await createCustomSymptom(symptomSection, 'Joint stiffness', '✨');
    await createCustomSymptom(symptomSection, 'Joint support', '🔥');

    const todayISO = await saveTodayWithSymptom(page, 'Joint stiffness');
    const otherISO = shiftISODate(todayISO, 3);

    await page.goto('/settings');
    await archiveCustomSymptom(page, customSymptomRow(symptomSection, 'Joint stiffness', 'active'));

    const archivedRow = customSymptomRow(symptomSection, 'Joint stiffness', 'archived');
    await archivedRow.locator('input[name="name"]').fill('Joint support');
    await selectSymptomIcon(archivedRow.locator('[data-symptom-edit-form]'), '⚡');
    await archivedRow.locator('[data-symptom-edit-form] button[type="submit"]').click();
    await expect(archivedRow.locator('[data-symptom-row-error]')).toContainText(
      DUPLICATE_SYMPTOM_NAME_ERROR
    );
    await expect(archivedRow.locator('input[name="name"]')).toHaveValue('Joint support');

    await archivedRow.locator('input[name="name"]').fill('Joint ease');
    await selectSymptomIcon(archivedRow.locator('[data-symptom-edit-form]'), '💧');
    await archivedRow.locator('[data-symptom-edit-form] button[type="submit"]').click();

    const renamedArchivedRow = customSymptomRow(symptomSection, 'Joint ease', 'archived');
    await expect(renamedArchivedRow).toBeVisible();
    await expect(renamedArchivedRow.locator('.status-ok')).toBeVisible();
    await renamedArchivedRow.locator('form[action$="/restore"] button[type="submit"]').click();
    await expect(
      customSymptomRow(symptomSection, 'Joint ease', 'active').locator('.status-ok')
    ).toBeVisible();
    await expect(customSymptomRow(symptomSection, 'Joint support', 'active')).toBeVisible();

    await openCalendarDayEditor(page, otherISO);
    await expect(
      page.locator(`[data-day-editor-form][data-day-editor-date="${otherISO}"] input[name="symptom_ids"][data-symptom-name="Joint ease"]`)
    ).toBeVisible();
  });

  test('custom symptom validation blocks duplicate, built-in, and invalid markup; maxlength guards long names', async ({
      page,
    }) => {
    await registerOwnerAndOpenSettings(page, 'settings-custom-symptom-validation');

    const symptomSection = page.locator('#settings-symptoms');
    const createForm = symptomSection.locator('[data-symptom-create-form]');

    await createCustomSymptom(symptomSection, 'Joint stiffness', '✨');
    await expect(customSymptomRow(symptomSection, 'Joint stiffness', 'active')).toBeVisible();

    // The create form has its own error container ([data-symptom-create-error]),
    // a sibling of the per-row [data-symptom-row-error]. Addressing it directly
    // — rather than the bare `.status-error` class shared with every row and
    // section on the page — is what makes "the CREATE form rejected this"
    // distinguishable from "something on this page errored".
    const createError = createForm.locator('[data-symptom-create-error]');

    await createForm.locator('#settings-new-symptom-name').fill(' joint STIFFNESS ');
    await selectSymptomIcon(createForm, '🔥');
    await createForm.locator('button[type="submit"]').click();
    await expect(createError).toContainText(DUPLICATE_SYMPTOM_NAME_ERROR);
    await expect(symptomSection.locator('[data-custom-symptom-row][data-symptom-name="Joint stiffness"]')).toHaveCount(1);

    // The first rejection left its error node on the page, and this attempt
    // expects the very same message — so the assertion below is satisfied by the
    // previous submit's output even if this one never leaves the browser. Bind
    // it to this click's own POST and its response, the way the save helpers do,
    // so the rejection has to be re-earned against the server.
    await createForm.locator('#settings-new-symptom-name').fill('Усталость');
    const [builtInDuplicateRequest] = await Promise.all([
      page.waitForRequest(
        (candidate) =>
          candidate.method() === 'POST' && new URL(candidate.url()).pathname === '/api/v1/symptoms',
      ),
      createForm.locator('button[type="submit"]').click(),
    ]);
    const builtInDuplicateResponse = await builtInDuplicateRequest.response();
    expect(builtInDuplicateResponse, 'expected a response for POST /api/v1/symptoms').not.toBeNull();
    await expect(createError).toContainText(DUPLICATE_SYMPTOM_NAME_ERROR);

    await createForm.locator('#settings-new-symptom-name').fill('<script>alert(1)</script>');
    await createForm.locator('button[type="submit"]').click();
    await expect(createError).toContainText(
      'Use plain text only. Tags and angle brackets are not allowed.'
    );

    const tooLongName = '12345678901234567890123456789012345678901';
    await createForm.locator('#settings-new-symptom-name').fill(tooLongName);
    await expect(createForm.locator('#settings-new-symptom-name')).toHaveValue(tooLongName.slice(0, 40));
    await expect(createForm.locator('[data-symptom-name-count]')).toHaveText('40/40');
  });

  test('long custom symptom names stay usable without layout overflow', async ({
      page,
    }) => {
    await registerOwnerAndOpenSettings(page, 'settings-custom-symptom-overflow');

    const symptomSection = page.locator('#settings-symptoms');
    const createForm = symptomSection.locator('[data-symptom-create-form]');
    const longButAllowedName = 'Long symptom after evening workout';
    await createForm.locator('#settings-new-symptom-name').fill(longButAllowedName);
    await selectSymptomIcon(createForm, '⚡');
    await createForm.locator('button[type="submit"]').click();
    await expect(symptomSection.locator('.status-ok')).toBeVisible();
    await expect(
      symptomSection.locator(`[data-custom-symptom-row][data-symptom-name="${longButAllowedName}"][data-symptom-state="active"]`)
    ).toBeVisible();

    await assertNoHorizontalOverflow(page);

    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);
    await page.locator('input[name="is_period"]').check();
    const longSymptomInput = await ensureSymptomInputVisible(
      dashboardSaveForm(page),
      longButAllowedName
    );
    await checkStyledControl(longSymptomInput);
    await assertSelectedSymptomChipHasNoTrailingMarker(
      page.locator(
        `label.choice-option:has(input[name="symptom_ids"][data-symptom-name="${longButAllowedName}"]:checked) .chip-lead`
      )
    );
    await assertNoHorizontalOverflow(page);

    await page.goto('/calendar');
    await expect(page).toHaveURL(/\/calendar(?:\?.*)?$/);
    await expect(page.locator('#calendar-grid-panel')).toBeVisible();
    await assertNoHorizontalOverflow(page);

    await page.goto('/settings');
    const activeRow = page.locator(
      `[data-custom-symptom-row][data-symptom-name="${longButAllowedName}"][data-symptom-state="active"]`
    );
    const editTooLongName = '12345678901234567890123456789012345678901';
    await activeRow.locator('input[name="name"]').fill(editTooLongName);
    await expect(activeRow.locator('input[name="name"]')).toHaveValue(editTooLongName.slice(0, 40));
    await expect(activeRow.locator('[data-symptom-name-count]')).toHaveText('40/40');
    await assertNoHorizontalOverflow(page);
  });

  test('custom symptom empty states explain what happens next until rows exist', async ({ page }) => {
    await registerOwnerAndOpenSettings(page, 'settings-custom-symptom-empty-groups');

    // Group presence is addressed through [data-symptom-group], not through the
    // localized heading: a getByText('Active custom symptoms').toHaveCount(0)
    // is satisfied by any rewording or by switching language, so it could not
    // tell an absent group from a renamed heading.
    const symptomSection = page.locator('#settings-symptoms');
    await expect(symptomSection.locator('[data-symptom-empty-state="empty"]')).toContainText(
      'No custom symptoms yet. Add one above to make it available in new entries.'
    );
    await expect(symptomSection.locator('[data-symptom-group="active"]')).toHaveCount(0);
    await expect(symptomSection.locator('[data-symptom-group="archived"]')).toHaveCount(0);

    const createForm = symptomSection.locator('[data-symptom-create-form]');
    await createForm.locator('#settings-new-symptom-name').fill('Joint stiffness');
    await selectSymptomIcon(createForm, '✨');
    await createForm.locator('button[type="submit"]').click();

    await expect(symptomSection.locator('[data-symptom-group="active"]')).toBeVisible();
    // The group is proved by the row it now holds. Its helper line ("These
    // appear in dashboard and calendar day editing.") is no longer rendered —
    // it restated the heading it sat under — so the empty-to-populated
    // transition is asserted on the row, which is the fact the test is about.
    await expect(
      symptomSection.locator('[data-symptom-group="active"] [data-custom-symptom-row]')
    ).toHaveCount(1);
    await expect(symptomSection.locator('[data-symptom-group="archived"]')).toHaveCount(0);
    await expect(symptomSection.locator('[data-symptom-empty-state="empty"]')).toHaveCount(0);
  });
});
