import { expect, test, type Locator, type Page } from '@playwright/test';
import { dateFieldRoot, fillDateField } from './support/date-field-helpers';
import {
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  createCredentials,
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

async function assertNoHorizontalOverflow(page: Page): Promise<void> {
  const hasOverflow = await page.evaluate(() => {
    const root = document.documentElement;
    return root.scrollWidth > root.clientWidth + 1;
  });
  expect(hasOverflow).toBe(false);
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
  const visible = await input.isVisible().catch(() => false);
  if (!visible) {
    const moreSummary = root.locator('details.symptom-more-details summary');
    if (await moreSummary.isVisible().catch(() => false)) {
      await moreSummary.click();
    }
  }
  await expect(input).toBeVisible();
  return input;
}

async function registerOwnerAndOpenSettings(page: Page, prefix: string) {
  const creds = createCredentials(prefix);

  await registerOwnerViaUI(page, creds);
  await expect(page).toHaveURL(/\/recovery-code$/);

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
  await row.locator('form[action$="/archive"] button[type="submit"]').click();
  await expect(page.locator('#confirm-modal')).toBeVisible();
  await page.locator('#confirm-modal-accept').click();
}

async function saveTodayWithSymptom(page: Page, symptomName: string): Promise<string> {
  await page.goto('/dashboard');
  await expect(page).toHaveURL(/\/dashboard$/);

  await page.locator('input[name="is_period"]').check();
  const customSymptom = await ensureSymptomInputVisible(
    page.locator('form[hx-post^="/api/days/"]').first(),
    symptomName
  );
  await customSymptom.check({ force: true });
  await page.locator('button[data-save-button]').first().click();
  await expect(page.locator('#save-status .status-ok')).toBeVisible();

  const todayAction = await page.locator('form[hx-post^="/api/days/"]').first().getAttribute('hx-post');
  expect(todayAction).toMatch(/^\/api\/days\/\d{4}-\d{2}-\d{2}$/);
  return String(todayAction).replace('/api/days/', '');
}

async function openCalendarDayEditor(page: Page, isoDate: string): Promise<Locator> {
  const month = isoDate.slice(0, 7);
  await page.goto(`/calendar?month=${month}&day=${isoDate}`);
  await expect(page).toHaveURL(new RegExp(`/calendar\\?month=${month}&day=${isoDate}`));

  const editButton = page.locator(`#day-editor button[hx-get="/calendar/day/${isoDate}?mode=edit"]`).first();
  await expect(editButton).toBeVisible();
  await editButton.click();

  const form = page.locator(`form.calendar-day-editor-form[hx-post="/api/days/${isoDate}"]`);
  await expect(form).toBeVisible();
  return form;
}

async function completeOnboardingWithStartDate(page: Page, startDate: string): Promise<void> {
  const startDateInput = page.locator('#last-period-start');
  await expect(dateFieldRoot(startDateInput)).toBeVisible();

  const startDateOption = page.locator(`[data-onboarding-day-option][data-onboarding-day-value="${startDate}"]`);
  if ((await startDateOption.count()) > 0) {
    await startDateOption.first().click();
  } else {
    await fillDateField(startDateInput, startDate);
  }

  await page.locator('form[hx-post="/onboarding/step1"] button[type="submit"]').click();

  const stepTwoForm = page.locator('form[hx-post="/onboarding/step2"]');
  await expect(stepTwoForm).toBeVisible();
  await stepTwoForm.locator('button[type="submit"]').click();
  await expect(page).toHaveURL(/\/dashboard(?:\?.*)?$/);
}

async function currentNextPeriodText(page: Page): Promise<string> {
  const value = await page
    .locator('.dashboard-status-line .dashboard-status-item')
    .nth(2)
    .textContent();

  return String(value || '').trim();
}

test.describe('Settings: profile and cycle', () => {
  test('profile name persists, long value is rejected, and empty clears without header fallback', async ({
    page,
  }) => {
    const creds = await registerOwnerAndOpenSettings(page, 'settings-profile');

    const profileAccountPanel = page.locator('#settings-account .journal-panel').filter({
      hasText: creds.email,
    });
    await expect(profileAccountPanel).toContainText(creds.email);
    await expect(profileAccountPanel).toContainText('Cannot be changed.');
    await expect(page.locator('#settings-account input#settings-profile-email')).toHaveCount(0);

    const displayNameInput = page.locator('#settings-display-name');
    const saveProfileButton = page.locator(
      'form[action="/api/settings/profile"] button[data-save-button]'
    );

    const newName = `Profile-${Date.now()}`;
    await displayNameInput.fill(newName);
    await saveProfileButton.click();
    await expect(page.locator('#settings-profile-status .status-ok')).toBeVisible();

    await page.reload();
    await expect(page).toHaveURL(/\/settings$/);
    await expect(displayNameInput).toHaveValue(newName);
    await expect(page.locator('.nav-user-chip')).toHaveCount(0);

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
    await expect(page.locator('.nav-user-chip')).toHaveCount(0);
  });

  test('cycle settings persist, affect dashboard predictions, and reject future last-period date', async ({
    page,
  }) => {
    await registerOwnerAndOpenSettings(page, 'settings-cycle');

    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);
    const nextPeriodBefore = await currentNextPeriodText(page);

    await page.goto('/settings');
    await expect(page).toHaveURL(/\/settings$/);

    const cycleForm = page.locator('section#settings-cycle form[action="/settings/cycle"]');
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
    await expect(page.locator('section#settings-cycle input[name="auto_period_fill"]')).not.toBeChecked();

    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);
    const nextPeriodAfter = await currentNextPeriodText(page);
    expect(nextPeriodAfter).not.toBe(nextPeriodBefore);

    await page.goto('/calendar');
    await expect(page).toHaveURL(/\/calendar(?:\?.*)?$/);
    await expect(page.locator('#calendar-grid-panel')).toBeVisible();

    await page.goto('/settings');
    await expect(page).toHaveURL(/\/settings$/);

    await fillDateField(page.locator('#settings-last-period-start'), isoDaysFromNow(1));
    await page
      .locator('section#settings-cycle form[action="/settings/cycle"] button[data-save-button]')
      .click();

    await expect(page.locator('#settings-cycle-status .status-error')).toBeVisible();
  });

  test('tracking toggles persist and change the owner day form', async ({ page }) => {
    await registerOwnerAndOpenSettings(page, 'settings-tracking');

    const trackingSection = page.locator('#settings-tracking');
    await expect(trackingSection).toBeVisible();

    const trackBBT = trackingSection.locator('input[name="track_bbt"]');
    const trackCervicalMucus = trackingSection.locator('input[name="track_cervical_mucus"]');
    const hideSexChip = trackingSection.locator('input[name="hide_sex_chip"]');
    const saveTrackingButton = trackingSection.locator('button[data-save-button]');

    await expect(trackBBT).not.toBeChecked();
    await expect(trackCervicalMucus).not.toBeChecked();
    await expect(hideSexChip).not.toBeChecked();

    await trackBBT.check();
    await trackCervicalMucus.check();
    await hideSexChip.check();
    await saveTrackingButton.click();
    await expect(page.locator('#settings-tracking-status .status-ok')).toBeVisible();

    await page.reload();
    await expect(page).toHaveURL(/\/settings$/);
    await expect(trackBBT).toBeChecked();
    await expect(trackCervicalMucus).toBeChecked();
    await expect(hideSexChip).toBeChecked();

    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);
    await expect(page.locator('form[hx-post^="/api/days/"] input[name="bbt"]')).toBeVisible();
    await expect(page.locator('form[hx-post^="/api/days/"] input[name="cervical_mucus"][value="dry"]')).toBeVisible();
    await expect(page.locator('form[hx-post^="/api/days/"] details.sex-activity-details')).toHaveCount(0);

    await page.goto('/settings');
    await expect(page).toHaveURL(/\/settings$/);
    await trackBBT.uncheck();
    await trackCervicalMucus.uncheck();
    await hideSexChip.uncheck();
    await saveTrackingButton.click();
    await expect(page.locator('#settings-tracking-status .status-ok')).toBeVisible();

    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);
    await expect(page.locator('form[hx-post^="/api/days/"] input[name="bbt"]')).toHaveCount(0);
    await expect(page.locator('form[hx-post^="/api/days/"] input[name="cervical_mucus"][value="dry"]')).toHaveCount(0);
    await expect(page.locator('form[hx-post^="/api/days/"] details.sex-activity-details')).toBeVisible();
  });

  test('onboarding selected start date persists into settings cycle field', async ({ page }) => {
    const creds = createCredentials('settings-onboarding-date');

    await registerOwnerViaUI(page, creds);
    await expect(page).toHaveURL(/\/recovery-code$/);

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

  test('archiving a custom symptom keeps old entries while hiding it from new days', async ({
    page,
  }) => {
    await registerOwnerAndOpenSettings(page, 'settings-custom-symptoms');

    const symptomSection = page.locator('#settings-symptoms-section');
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
    const hideButtonBox = await activeRow.locator('form[action$="/archive"] button[type="submit"]').boundingBox();
    expect(saveButtonBox).not.toBeNull();
    expect(hideButtonBox).not.toBeNull();
    expect(hideButtonBox!.y).toBeGreaterThan(saveButtonBox!.y + 4);

    await archiveCustomSymptom(page, activeRow);
    await expect(customSymptomRow(symptomSection, 'Joint stiffness', 'archived').locator('[data-symptom-row-success]')).toBeVisible();

    await page.goto('/dashboard');
    const archivedDashboardSymptom = await ensureSymptomInputVisible(
      page.locator('form[hx-post^="/api/days/"]').first(),
      'Joint stiffness'
    );
    await expect(archivedDashboardSymptom).toBeChecked();

    await openCalendarDayEditor(page, otherISO);
    await expect(
      page.locator(`form.calendar-day-editor-form[hx-post="/api/days/${otherISO}"] input[name="symptom_ids"][data-symptom-name="Joint stiffness"]`)
    ).toHaveCount(0);
  });

  test('archived custom symptoms can be renamed, reject duplicates, and restore cleanly', async ({
    page,
  }) => {
    await registerOwnerAndOpenSettings(page, 'settings-custom-symptoms-restore');

    const symptomSection = page.locator('#settings-symptoms-section');
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
      'That symptom name already exists in your list.'
    );
    await expect(archivedRow.locator('input[name="name"]')).toHaveValue('Joint support');

    await archivedRow.locator('input[name="name"]').fill('Joint ease');
    await selectSymptomIcon(archivedRow.locator('[data-symptom-edit-form]'), '💧');
    await archivedRow.locator('[data-symptom-edit-form] button[type="submit"]').click();

    const renamedArchivedRow = customSymptomRow(symptomSection, 'Joint ease', 'archived');
    await expect(renamedArchivedRow).toBeVisible();
    await expect(renamedArchivedRow.locator('[data-symptom-row-success]')).toBeVisible();
    await renamedArchivedRow.locator('form[action$="/restore"] button[type="submit"]').click();
    await expect(
      customSymptomRow(symptomSection, 'Joint ease', 'active').locator('[data-symptom-row-success]')
    ).toBeVisible();
    await expect(customSymptomRow(symptomSection, 'Joint support', 'active')).toBeVisible();

    await openCalendarDayEditor(page, otherISO);
    await expect(
      page.locator(`form.calendar-day-editor-form[hx-post="/api/days/${otherISO}"] input[name="symptom_ids"][data-symptom-name="Joint ease"]`)
    ).toBeVisible();
  });

  test('custom symptom validation blocks duplicate, built-in, invalid markup, and too-long names', async ({
      page,
    }) => {
    await registerOwnerAndOpenSettings(page, 'settings-custom-symptom-validation');

    const symptomSection = page.locator('#settings-symptoms-section');
    const createForm = symptomSection.locator('[data-symptom-create-form]');

    await createCustomSymptom(symptomSection, 'Joint stiffness', '✨');
    await expect(customSymptomRow(symptomSection, 'Joint stiffness', 'active')).toBeVisible();

    await createForm.locator('#settings-new-symptom-name').fill(' joint STIFFNESS ');
    await selectSymptomIcon(createForm, '🔥');
    await createForm.locator('button[type="submit"]').click();
    await expect(symptomSection.locator('.status-error')).toContainText('That symptom name already exists in your list.');
    await expect(symptomSection.locator('[data-custom-symptom-row][data-symptom-name="Joint stiffness"]')).toHaveCount(1);

    await createForm.locator('#settings-new-symptom-name').fill('Усталость');
    await createForm.locator('button[type="submit"]').click();
    await expect(symptomSection.locator('.status-error')).toContainText('That symptom name already exists in your list.');

    await createForm.locator('#settings-new-symptom-name').fill('<script>alert(1)</script>');
    await createForm.locator('button[type="submit"]').click();
    await expect(symptomSection.locator('.status-error')).toContainText(
      'Use plain text only. Tags and angle brackets are not allowed.'
    );

    const tooLongName = '12345678901234567890123456789012345678901';
    await createForm.locator('#settings-new-symptom-name').fill(tooLongName);
    await createForm.locator('button[type="submit"]').click();
    await expect(symptomSection.locator('.status-error')).toContainText(
      'Use 40 characters or fewer. For longer details, use notes.'
    );
    await expect(createForm.locator('#settings-new-symptom-name')).toHaveValue('');
  });

  test('long custom symptom names stay usable without layout overflow', async ({
      page,
    }) => {
    await registerOwnerAndOpenSettings(page, 'settings-custom-symptom-overflow');

    const symptomSection = page.locator('#settings-symptoms-section');
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
      page.locator('form[hx-post^="/api/days/"]').first(),
      longButAllowedName
    );
    await longSymptomInput.check({ force: true });
    await assertSelectedSymptomChipHasNoTrailingMarker(
      page.locator(
        `label.choice-option:has(input[name="symptom_ids"][data-symptom-name="${longButAllowedName}"]:checked) .check-chip`
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
    await activeRow.locator('input[name="name"]').fill('12345678901234567890123456789012345678901');
    await activeRow.locator('[data-symptom-edit-form] button[type="submit"]').click();
    await expect(activeRow.locator('[data-symptom-row-error]')).toContainText(
      'Use 40 characters or fewer. For longer details, use notes.'
    );
    await expect(activeRow.locator('input[name="name"]')).toHaveValue(longButAllowedName);
  });

  test('empty custom symptom groups stay hidden until they have rows', async ({ page }) => {
    await registerOwnerAndOpenSettings(page, 'settings-custom-symptom-empty-groups');

    const symptomSection = page.locator('#settings-symptoms-section');
    await expect(symptomSection.getByText('Active custom symptoms')).toHaveCount(0);
    await expect(symptomSection.getByText('Hidden custom symptoms')).toHaveCount(0);

    const createForm = symptomSection.locator('[data-symptom-create-form]');
    await createForm.locator('#settings-new-symptom-name').fill('Joint stiffness');
    await selectSymptomIcon(createForm, '✨');
    await createForm.locator('button[type="submit"]').click();

    await expect(symptomSection.getByText('Active custom symptoms')).toBeVisible();
    await expect(symptomSection.getByText('Hidden custom symptoms')).toHaveCount(0);
  });
});
