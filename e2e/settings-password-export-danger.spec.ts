import { expect, test, type Locator, type Page } from '@playwright/test';
import { clearDateField, fillDateField } from './support/date-field-helpers';
import { ensureNotesFieldVisible } from './support/note-helpers';
import { setRequestTimezoneFromBrowser } from './support/timezone-helpers';
import {
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  createCredentials,
  expectDedicatedRecoveryPage,
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

function isoDaysAgo(days: number): string {
  return toISODate(new Date(Date.now() - days * 24 * 60 * 60 * 1000));
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

async function registerOwnerAndOpenSettings(page: Page, prefix: string) {
  const creds = createCredentials(prefix);

  await registerOwnerViaUI(page, creds);
  await expectInlineRegisterRecoveryStep(page);

  const recoveryCode = await readRecoveryCode(page);
  await continueFromRecoveryCode(page);
  await completeOnboardingIfPresent(page);

  await page.goto('/settings');
  await expect(page).toHaveURL(/\/settings$/);

  return { ...creds, recoveryCode };
}

async function readCSRFToken(page: Page): Promise<string> {
  const csrfToken = await page.locator('meta[name="csrf-token"]').getAttribute('content');
  expect(csrfToken).toBeTruthy();
  return csrfToken ?? '';
}

async function todayISOFromCalendar(page: Page): Promise<string> {
  const todayButton = page.locator('button[data-day]:has(.calendar-today-pill)').first();
  await expect(todayButton).toBeVisible();
  const todayISO = await todayButton.getAttribute('data-day');
  expect(todayISO).toMatch(/^\d{4}-\d{2}-\d{2}$/);
  return todayISO!;
}

async function openCalendarDayEditor(page: Page, isoDate: string): Promise<Locator> {
  const month = isoDate.slice(0, 7);
  await page.goto(`/calendar?month=${month}&day=${isoDate}`);
  await expect(page).toHaveURL(new RegExp(`/calendar\\?month=${month}&day=${isoDate}`));

  const editButton = page.locator(`[data-day-editor-open="${isoDate}"]`).first();
  await expect(editButton).toBeVisible();
  await editButton.click();

  const form = page.locator(`[data-day-editor-form][data-day-editor-date="${isoDate}"]`);
  await expect(form).toBeVisible();
  return form;
}

async function openCalendarNotes(form: Locator): Promise<void> {
  await ensureNotesFieldVisible(form, '#calendar-notes');
}

async function saveTodayEntry(page: Page, note: string): Promise<void> {
  await page.goto('/dashboard');
  await expect(page).toHaveURL(/\/dashboard$/);

  await page.locator('input[name="is_period"]').check();
  await page.locator('input[name="flow"][value="medium"]').check({ force: true });
  await ensureNotesFieldVisible(page, '#today-notes');
  await page.locator('#today-notes').fill(note);

  await page.locator('[data-dashboard-save-form] button[data-save-button]').click();
  await expect(page.locator('#save-status .status-ok')).toBeVisible();
}

async function createCustomSymptom(page: Page, name: string): Promise<void> {
  const section = page.locator('#settings-symptoms-section');
  const form = section.locator('[data-symptom-create-form]');

  await form.locator('#settings-new-symptom-name').fill(name);
  await form.locator('[data-icon-option]').first().click();
  await form.locator('button[type="submit"]').click();

  await expect(section.locator(`[data-custom-symptom-row][data-symptom-name="${name}"]`)).toBeVisible();
}

test.describe('Settings: password, export, clear data, delete account', () => {
  test('change password success rotates credentials: old password rejected, new password works', async ({
    page,
  }) => {
    const creds = await registerOwnerAndOpenSettings(page, 'settings-password-rotate');
    const newPassword = 'NewStrongPass2';

    await page.locator('#settings-current-password').fill(creds.password);
    await page.locator('#settings-new-password').fill(newPassword);
    await page.locator('#settings-confirm-password').fill(newPassword);
    await page
      .locator('form[action="/api/settings/change-password"] button[type="submit"]')
      .click();

    await expect(page).toHaveURL(/\/settings$/);
    await expect(page.locator('#settings-change-password-status .status-ok').first()).toBeVisible();

    await logoutViaAPI(page);
    await loginViaUI(page, creds);
    await expect(page).toHaveURL(/\/login$/);
    await expect(page.locator('.status-error')).toBeVisible();

    await loginViaUI(page, { email: creds.email, password: newPassword });
    await expect(page).toHaveURL(/\/(dashboard|onboarding)(?:\?.*)?$/);
  });

  test('change password validation: wrong current, mismatch, weak password', async ({ page }) => {
    const creds = await registerOwnerAndOpenSettings(page, 'settings-password-validation');
    let passwordRequests = 0;

    page.on('request', (request) => {
      if (
        request.method() === 'POST' &&
        request.url().includes('/api/settings/change-password')
      ) {
        passwordRequests += 1;
      }
    });

    const submit = page.locator('form[action="/api/settings/change-password"] button[type="submit"]');
    const checklist = page.locator('#settings-change-password-form [data-password-guidance]');

    await expect(checklist.locator('[data-password-rule-item="length"]')).toHaveAttribute(
      'data-met',
      'false'
    );

    await page.locator('#settings-current-password').fill('WrongPass1');
    await page.locator('#settings-new-password').fill('ValidStrong2');
    await page.locator('#settings-confirm-password').fill('ValidStrong2');
    await submit.click();
    await expect(page.locator('#settings-change-password-status .status-error')).toBeVisible();
    await expect.poll(() => passwordRequests).toBe(1);

    await expect(checklist.locator('[data-password-rule-item="upper"]')).toHaveAttribute(
      'data-met',
      'true'
    );
    await expect(checklist.locator('[data-password-rule-item="lower"]')).toHaveAttribute(
      'data-met',
      'true'
    );
    await expect(checklist.locator('[data-password-rule-item="digit"]')).toHaveAttribute(
      'data-met',
      'true'
    );

    await page.locator('#settings-current-password').fill(creds.password);
    await page.locator('#settings-new-password').fill('ValidStrong2');
    await page.locator('#settings-confirm-password').fill('DifferentStrong3');
    await submit.click();
    await expect(page.locator('#settings-change-password-status .status-error')).toBeVisible();
    await expect.poll(() => passwordRequests).toBe(1);
    await expect(page.locator('#settings-new-password')).toHaveValue('ValidStrong2');
    await expect(page.locator('#settings-confirm-password')).toHaveValue('DifferentStrong3');

    await page.locator('#settings-current-password').fill(creds.password);
    await page.locator('#settings-new-password').fill('weakpass');
    await page.locator('#settings-confirm-password').fill('weakpass');
    await submit.click();
    await expect(page.locator('#settings-change-password-status .status-error')).toBeVisible();
    await expect.poll(() => passwordRequests).toBe(1);
    await expect(checklist.locator('[data-password-rule-item="length"]')).toHaveAttribute(
      'data-met',
      'true'
    );
    await expect(checklist.locator('[data-password-rule-item="upper"]')).toHaveAttribute(
      'data-met',
      'false'
    );
    await expect(checklist.locator('[data-password-rule-item="digit"]')).toHaveAttribute(
      'data-met',
      'false'
    );
  });

  test('recovery code regeneration uses dedicated recovery page and returns to settings', async ({
    page,
  }) => {
    const state = await registerOwnerAndOpenSettings(page, 'settings-recovery-regenerate');
    const cycleForm = page.locator('section#settings-cycle form[action="/settings/cycle"]');

    await expect(cycleForm).toBeVisible();
    await setRangeValue(page.locator('#settings-cycle-length'), 28);
    await setRangeValue(page.locator('#settings-period-length'), 5);
    await page.locator('input[name="unpredictable_cycle"]').uncheck();
    await cycleForm.locator('button[data-save-button]').click();
    await expect(page.locator('#settings-cycle-status .status-ok')).toBeVisible();

    await page
      .locator('form[action="/api/settings/regenerate-recovery-code"] button[type="submit"]')
      .click();
    await expect(page.locator('#confirm-modal')).toBeVisible();
    await page.locator('#confirm-modal-accept').click();

    await expectDedicatedRecoveryPage(page);
    await expect(page.locator('form[action="/settings"]')).toBeVisible();

    const regeneratedRecoveryCode = await readRecoveryCode(page);
    expect(regeneratedRecoveryCode).not.toBe(state.recoveryCode);

    await page.locator('#recovery-code-saved').check();
    await page.locator('form[action="/settings"] button[type="submit"]').click();

    await expect(page).toHaveURL(/\/settings(?:\?.*)?$/);
    await expect(page.locator('.recovery-code-box')).toHaveCount(0);
    await expect(page.locator('#settings-period-length')).toHaveValue('5');
    await expect(page.locator('input[name="unpredictable_cycle"]')).not.toBeChecked();
  });

  test('export CSV, JSON, and PDF from settings return attachment responses with expected structure', async ({
    page,
  }) => {
    await registerOwnerAndOpenSettings(page, 'settings-export');

    const exportNote = `export-note-${Date.now()}`;
    await saveTodayEntry(page, exportNote);

    await page.goto('/settings');
    await expect(page).toHaveURL(/\/settings$/);
    const csrfToken = await readCSRFToken(page);

    const csvResponse = await page.request.post('/api/export/csv', {
      form: { csrf_token: csrfToken },
    });

    expect(csvResponse.status()).toBe(200);
    expect(csvResponse.headers()['content-type'] || '').toContain('text/csv');
    expect(csvResponse.headers()['content-disposition'] || '').toContain('attachment;');
    expect(await csvResponse.text()).toContain(exportNote);

    const jsonResponse = await page.request.post('/api/export/json', {
      form: { csrf_token: csrfToken },
    });

    expect(jsonResponse.status()).toBe(200);
    expect(jsonResponse.headers()['content-type'] || '').toContain('application/json');
    expect(jsonResponse.headers()['content-disposition'] || '').toContain('attachment;');

    const payload = (await jsonResponse.json()) as {
      exported_at?: unknown;
      entries?: Array<{ notes?: string }>;
    };

    expect(typeof payload.exported_at).toBe('string');
    expect(Array.isArray(payload.entries)).toBe(true);
    expect(payload.entries?.some((entry) => String(entry.notes || '') === exportNote)).toBe(true);

    const pdfResponse = await page.request.post('/api/export/pdf', {
      form: { csrf_token: csrfToken },
    });

    expect(pdfResponse.status()).toBe(200);
    expect(pdfResponse.headers()['content-type'] || '').toContain('application/pdf');
    expect(pdfResponse.headers()['content-disposition'] || '').toContain('attachment;');

    const pdfBody = await pdfResponse.body();
    expect(pdfBody.subarray(0, 4).toString()).toBe('%PDF');
  });

  test('export date range defaults to browser today even when future entries exist', async ({ page }) => {
    await registerOwnerAndOpenSettings(page, 'settings-export-defaults');
    await setRequestTimezoneFromBrowser(page);

    await page.goto('/calendar');
    const todayISO = await todayISOFromCalendar(page);
    const futureISO = shiftISODate(todayISO, 4);

    const dayEditorForm = await openCalendarDayEditor(page, futureISO);
    await dayEditorForm.locator('input[name="is_period"]').check();
    await openCalendarNotes(dayEditorForm);
    await dayEditorForm.locator('#calendar-notes').fill(`future-export-${Date.now()}`);
    await dayEditorForm.locator('button[data-save-button]').click();

    await page.goto('/settings');
    await expect(page).toHaveURL(/\/settings$/);
    await expect(page.locator('#export-to')).toHaveValue(todayISO);
    await expect(page.locator('#export-to')).toHaveAttribute('max', futureISO);
  });

  test('export range fields stay stable while editing instead of snapping back to bounds', async ({
    page,
  }) => {
    await registerOwnerAndOpenSettings(page, 'settings-export-stable-range');

    await page.goto('/settings');
    await expect(page).toHaveURL(/\/settings$/);

    const exportTo = page.locator('#export-to');
    const exportButtons = page.locator('button[data-export-action]');
    const initialValue = String(await exportTo.inputValue());

    await clearDateField(exportTo);
    await expect(exportTo).toHaveValue('');
    await expect(exportButtons.first()).toBeDisabled();

    await fillDateField(exportTo, initialValue);
    await expect(exportTo).toHaveValue(initialValue);
    await expect(exportButtons.first()).toBeEnabled();
  });

  test('export invalid date range is blocked before download', async ({ page }) => {
    await registerOwnerAndOpenSettings(page, 'settings-export-invalid-range');

    await page.goto('/settings');
    await expect(page).toHaveURL(/\/settings$/);

    const exportFrom = page.locator('#export-from');
    const exportTo = page.locator('#export-to');
    const exportButton = page.locator('button[data-export-action]').first();
    const maxValue = String(await exportTo.inputValue());
    const laterFrom = shiftISODate(maxValue, -1);
    const earlierTo = shiftISODate(maxValue, -3);

    await expect(exportButton).toBeEnabled();
    await fillDateField(exportFrom, laterFrom);
    await fillDateField(exportTo, earlierTo);
    await expect(exportTo).toHaveValue(earlierTo);
    await expect(exportButton).toBeDisabled();
  });

  test('export presets stay ordered and anchor to browser today even with future entries', async ({
    page,
  }) => {
    await registerOwnerAndOpenSettings(page, 'settings-export-presets-ordered');
    await setRequestTimezoneFromBrowser(page);

    await page.goto('/calendar');
    const todayISO = await todayISOFromCalendar(page);
    const futureISO = shiftISODate(todayISO, 4);

    const dayEditorForm = await openCalendarDayEditor(page, futureISO);
    await dayEditorForm.locator('input[name="is_period"]').check();
    await openCalendarNotes(dayEditorForm);
    await dayEditorForm.locator('#calendar-notes').fill(`future-preset-${Date.now()}`);
    await dayEditorForm.locator('button[data-save-button]').click();

    await page.goto('/settings');
    await expect(page).toHaveURL(/\/settings$/);

    await page.locator('button[data-export-preset="365"]').click();

    const exportFrom = page.locator('#export-from');
    const exportTo = page.locator('#export-to');
    const fromValue = await exportFrom.inputValue();
    const toValue = await exportTo.inputValue();

    expect(fromValue <= toValue).toBeTruthy();
    expect(toValue).toBe(todayISO);
  });

  test('clear data removes tracked entry and resets cycle defaults', async ({ page }) => {
    const creds = await registerOwnerAndOpenSettings(page, 'settings-clear-data');

    const dangerZone = page.locator('section.settings-danger-zone');
    await expect(dangerZone.locator('form[action="/api/settings/clear-data"]')).toHaveCount(1);
    await expect(page.locator('#settings-data form[action="/api/settings/clear-data"]')).toHaveCount(0);

    await setRangeValue(page.locator('#settings-cycle-length'), 35);
    await setRangeValue(page.locator('#settings-period-length'), 7);
    await fillDateField(page.locator('#settings-last-period-start'), isoDaysAgo(12));
    await page.locator('section#settings-cycle input[name="auto_period_fill"]').uncheck();
    await page
      .locator('section#settings-cycle form[action="/settings/cycle"] button[data-save-button]')
      .click();
    await expect(page.locator('#settings-cycle-status .status-ok')).toBeVisible();

    const clearNote = `clear-note-${Date.now()}`;
    await saveTodayEntry(page, clearNote);

    await page.goto('/settings');
    await expect(page).toHaveURL(/\/settings$/);
    await createCustomSymptom(page, 'Reset me');
    const symptomSection = page.locator('#settings-symptoms-section');
    await expect(symptomSection).not.toContainText('Shown in new entries.');
    await expect(symptomSection).not.toContainText('No custom symptoms yet.');
    await expect(symptomSection).not.toContainText('Kept in history and export.');
    await expect(symptomSection).not.toContainText('Built-in symptoms always stay available.');

    await dangerZone.locator('#settings-clear-data-password').fill('WrongPass1');
    await dangerZone.locator('form[action="/api/settings/clear-data"] button[type="submit"]').click();
    await expect(page.locator('#confirm-modal')).toBeHidden();
    await expect(page.locator('#settings-clear-data-status .status-error')).toBeVisible();

    await dangerZone.locator('#settings-clear-data-password').fill(creds.password);
    await dangerZone.locator('form[action="/api/settings/clear-data"] button[type="submit"]').click();
    await expect(page.locator('#confirm-modal')).toBeVisible();
    await page.locator('#confirm-modal-accept').click();

    await expect(page).toHaveURL(/\/settings$/);

    await page.reload();
    await expect(page).toHaveURL(/\/settings$/);

    await expect(page.locator('#settings-cycle-length')).toHaveValue('28');
    await expect(page.locator('#settings-period-length')).toHaveValue('5');
    await expect(page.locator('section#settings-cycle input[name="auto_period_fill"]')).toBeChecked();
    await expect(page.locator('#settings-last-period-start')).toHaveValue('');

    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);
    await expect(page.locator('#today-notes')).toHaveValue('');
    await expect(page.locator('input[name="symptom_ids"]:checked')).toHaveCount(0);

    await page.goto('/settings');
    await expect(page.locator('[data-export-summary-total]')).toContainText('0');
    await expect(page.locator('#settings-symptoms-section [data-custom-symptom-row]')).toHaveCount(0);
  });

  test('delete account requires valid password and removes account on success', async ({ page }) => {
    const creds = await registerOwnerAndOpenSettings(page, 'settings-delete-account');

    const deleteForm = page.locator('form[hx-delete="/api/settings/delete-account"]');

    await deleteForm.locator('#settings-delete-password').fill('WrongPass1');
    await deleteForm.locator('button[type="submit"]').click();
    await expect(page.locator('#confirm-modal')).toBeVisible();
    await page.locator('#confirm-modal-accept').click();

    await expect(page).toHaveURL(/\/settings$/);
    await expect(page.locator('#delete-account-feedback .status-error')).toBeVisible();

    await deleteForm.locator('#settings-delete-password').fill(creds.password);
    await deleteForm.locator('button[type="submit"]').click();
    await expect(page.locator('#confirm-modal')).toBeVisible();
    await page.locator('#confirm-modal-accept').click();

    await expect(page).toHaveURL(/\/login$/);

    await loginViaUI(page, creds);
    await expect(page).toHaveURL(/\/login$/);
    await expect(page.locator('.status-error')).toBeVisible();
  });
});
