import { expect, test, type Locator, type Page } from '@playwright/test';
import { fillDateField } from './support/date-field-helpers';
import {
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  createCredentials,
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
  await expect(page).toHaveURL(/\/recovery-code$/);

  const recoveryCode = await readRecoveryCode(page);
  await continueFromRecoveryCode(page);
  await completeOnboardingIfPresent(page);

  await page.goto('/settings');
  await expect(page).toHaveURL(/\/settings$/);

  return { ...creds, recoveryCode };
}

async function saveTodayEntry(page: Page, note: string): Promise<void> {
  await page.goto('/dashboard');
  await expect(page).toHaveURL(/\/dashboard$/);

  await page.locator('input[name="is_period"]').check();
  await page.locator('input[name="flow"][value="medium"]').check({ force: true });
  const disclosure = page.locator('details.note-disclosure');
  const isOpen = await disclosure.evaluate((element) => element.hasAttribute('open'));
  if (!isOpen) {
    await disclosure.locator('summary').click();
  }
  await expect(page.locator('#today-notes')).toBeVisible();
  await page.locator('#today-notes').fill(note);

  await page.locator('button[data-save-button]').first().click();
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
    await expect(page.locator('.status-ok')).toBeVisible();

    await logoutViaAPI(page);
    await loginViaUI(page, creds);
    await expect(page).toHaveURL(/\/login$/);
    await expect(page.locator('.status-error')).toBeVisible();

    await loginViaUI(page, { email: creds.email, password: newPassword });
    await expect(page).toHaveURL(/\/(dashboard|onboarding)(?:\?.*)?$/);
  });

  test('change password validation: wrong current, mismatch, weak password', async ({ page }) => {
    const creds = await registerOwnerAndOpenSettings(page, 'settings-password-validation');

    const submit = page.locator('form[action="/api/settings/change-password"] button[type="submit"]');

    await page.locator('#settings-current-password').fill('WrongPass1');
    await page.locator('#settings-new-password').fill('ValidStrong2');
    await page.locator('#settings-confirm-password').fill('ValidStrong2');
    await submit.click();
    await expect(page.locator('#settings-change-password-status .status-error')).toBeVisible();

    await page.locator('#settings-current-password').fill(creds.password);
    await page.locator('#settings-new-password').fill('ValidStrong2');
    await page.locator('#settings-confirm-password').fill('DifferentStrong3');
    await submit.click();
    await expect(page.locator('#settings-change-password-status .status-error')).toBeVisible();

    await page.locator('#settings-current-password').fill(creds.password);
    await page.locator('#settings-new-password').fill('weakpass');
    await page.locator('#settings-confirm-password').fill('weakpass');
    await submit.click();
    await expect(page.locator('#settings-change-password-status .status-error')).toBeVisible();
  });

  test('recovery code regeneration uses dedicated recovery page and returns to settings', async ({
    page,
  }) => {
    const state = await registerOwnerAndOpenSettings(page, 'settings-recovery-regenerate');

    const advancedSecurity = page.locator('details.security-advanced');
    const isAdvancedOpen = await advancedSecurity.evaluate((element) => element.hasAttribute('open'));
    if (!isAdvancedOpen) {
      await advancedSecurity.locator('summary').click();
    }

    await page
      .locator('form[action="/api/settings/regenerate-recovery-code"] button[type="submit"]')
      .click();
    await expect(page.locator('#confirm-modal')).toBeVisible();
    await page.locator('#confirm-modal-accept').click();

    await expect(page).toHaveURL(/\/recovery-code$/);
    await expect(page.locator('form[action="/settings"]')).toBeVisible();

    const regeneratedRecoveryCode = await readRecoveryCode(page);
    expect(regeneratedRecoveryCode).not.toBe(state.recoveryCode);

    await page.locator('#recovery-code-saved').check();
    await page.locator('form[action="/settings"] button[type="submit"]').click();

    await expect(page).toHaveURL(/\/settings(?:\?.*)?$/);
    await expect(page.locator('.recovery-code-box')).toHaveCount(0);
  });

  test('export CSV, JSON, and PDF from settings return attachment responses with expected structure', async ({
    page,
  }) => {
    await registerOwnerAndOpenSettings(page, 'settings-export');

    const exportNote = `export-note-${Date.now()}`;
    await saveTodayEntry(page, exportNote);

    await page.goto('/settings');
    await expect(page).toHaveURL(/\/settings$/);

    const csvResponsePromise = page.waitForResponse(
      (response) => response.url().includes('/api/export/csv') && response.request().method() === 'GET'
    );
    await page.locator('a[data-export-link][data-export-type="csv"]').click();
    const csvResponse = await csvResponsePromise;

    expect(csvResponse.status()).toBe(200);
    expect(csvResponse.headers()['content-type'] || '').toContain('text/csv');
    expect(csvResponse.headers()['content-disposition'] || '').toContain('attachment;');
    expect(await csvResponse.text()).toContain(exportNote);

    const jsonResponsePromise = page.waitForResponse(
      (response) => response.url().includes('/api/export/json') && response.request().method() === 'GET'
    );
    await page.locator('a[data-export-link][data-export-type="json"]').click();
    const jsonResponse = await jsonResponsePromise;

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

    const pdfResponsePromise = page.waitForResponse(
      (response) => response.url().includes('/api/export/pdf') && response.request().method() === 'GET'
    );
    await page.locator('a[data-export-link][data-export-type="pdf"]').click();
    const pdfResponse = await pdfResponsePromise;

    expect(pdfResponse.status()).toBe(200);
    expect(pdfResponse.headers()['content-type'] || '').toContain('application/pdf');
    expect(pdfResponse.headers()['content-disposition'] || '').toContain('attachment;');

    const pdfBody = await pdfResponse.body();
    expect(pdfBody.subarray(0, 4).toString()).toBe('%PDF');
  });

  test('clear data removes tracked entry and resets cycle defaults', async ({ page }) => {
    await registerOwnerAndOpenSettings(page, 'settings-clear-data');

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

    await page.locator('form[action="/api/settings/clear-data"] button[type="submit"]').click();
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
