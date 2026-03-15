import { expect, test, type Page } from '@playwright/test';
import {
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  readRecoveryCode,
  registerOwnerViaUI,
} from './support/auth-helpers';
import { expectElementAboveMobileTabbar } from './support/mobile-layout-helpers';
import { ensureNotesFieldVisible } from './support/note-helpers';
import { setRequestTimezoneFromBrowser } from './support/timezone-helpers';

async function registerOwnerOnDashboard(page: Page, prefix: string): Promise<void> {
  const creds = createCredentials(prefix);

  await registerOwnerViaUI(page, creds);
  await expectInlineRegisterRecoveryStep(page);

  await readRecoveryCode(page);
  await continueFromRecoveryCode(page);
  await completeOnboardingIfPresent(page);

  await setRequestTimezoneFromBrowser(page);
  await page.goto('/dashboard');
  await expect(page).toHaveURL(/\/dashboard$/);
}

async function saveToday(page: Page): Promise<void> {
  await page.locator('button[data-save-button]').first().click();
  await expect(page.locator('#save-status .status-ok')).toBeVisible();
}

async function enableBBTTracking(page: Page): Promise<void> {
  await page.goto('/settings');
  await expect(page).toHaveURL(/\/settings$/);

  const trackingSection = page.locator('#settings-tracking');
  await expect(trackingSection).toBeVisible();

  const trackBBT = trackingSection.locator('input[name="track_bbt"]');
  if (!(await trackBBT.isChecked())) {
    await trackBBT.check();
  }

  await trackingSection.locator('button[data-save-button]').click();
  await expect(page.locator('#settings-tracking-status .status-ok')).toBeVisible();

  await page.goto('/dashboard');
  await expect(page).toHaveURL(/\/dashboard$/);
}

function todaySaveForm(page: Page) {
  return page.locator('[data-dashboard-save-form]');
}

function manualCycleStartButton(page: Page) {
  return page.locator('[data-dashboard-cycle-start-button]');
}

function clearTodayButton(page: Page) {
  return page.locator('[data-dashboard-clear-button]');
}

async function todaySavePath(page: Page): Promise<string> {
  const action = await todaySaveForm(page).first().getAttribute('hx-post');
  expect(action).toMatch(/^\/api\/days\/\d{4}-\d{2}-\d{2}$/);
  return String(action);
}

async function waitForDashboardAutosave(
  page: Page,
  savePath?: string,
  options?: { expectIndicator?: boolean }
): Promise<void> {
  const path = savePath ?? (await todaySavePath(page));
  await page.waitForResponse((response) => {
    return response.request().method() === 'POST' && response.url().includes(path);
  });
  if (options?.expectIndicator === false) {
    return;
  }
  await expect(page.locator('[data-dashboard-autosave-indicator]')).toHaveAttribute('data-autosave-state', 'saved');
}

function todaySymptomOptions(page: Page) {
  return page.locator('fieldset[data-dashboard-section="symptoms"] label.choice-option');
}

function symptomInputForOption(option: ReturnType<typeof todaySymptomOptions>) {
  return option.locator('input[name="symptom_ids"]');
}

function symptomChipForOption(option: ReturnType<typeof todaySymptomOptions>) {
  return option.locator('.check-chip');
}

async function openTodayNotes(page: Page): Promise<void> {
  await ensureNotesFieldVisible(page, '#today-notes');
}

async function clientLocalISODate(page: Page): Promise<string> {
  return page.evaluate(() => {
    const now = new Date();
    const yyyy = now.getFullYear();
    const mm = String(now.getMonth() + 1).padStart(2, '0');
    const dd = String(now.getDate()).padStart(2, '0');
    return `${yyyy}-${mm}-${dd}`;
  });
}

test.describe('Dashboard: today editor', () => {
  test('uses request-local timezone date in today save endpoint', async ({ page }) => {
    await registerOwnerOnDashboard(page, 'dashboard-timezone');

    const todayForm = todaySaveForm(page);
    await expect(todayForm).toBeVisible();

    const action = await todayForm.getAttribute('hx-post');
    expect(action).toMatch(/^\/api\/days\/\d{4}-\d{2}-\d{2}$/);

    const serverToday = action!.replace('/api/days/', '');
    const clientToday = await clientLocalISODate(page);

    expect(serverToday).toBe(clientToday);
  });

  test('notes field uses a disclosure and reopens once a note exists', async ({
    page,
  }) => {
    await registerOwnerOnDashboard(page, 'dashboard-note-disclosure');

    const noteDisclosure = page.locator('details.note-disclosure');
    const noteSummary = noteDisclosure.locator('summary');
    const notes = page.locator('#today-notes');
    const notesCounter = page.locator('[data-dashboard-notes-field-group] [data-dashboard-notes-count]').first();
    await expect(noteDisclosure).toHaveCount(1);
    await expect(noteDisclosure).not.toHaveAttribute('open', '');
    await expect(noteSummary).toContainText(/Add note|Добавить заметку|Agregar nota/);
    await expect(notes).toBeHidden();

    await noteSummary.click();
    await expect(noteDisclosure).toHaveAttribute('open', '');
    await expect(noteSummary).toContainText(/Hide note|Скрыть заметку|Ocultar nota/);
    await expect(notes).toBeVisible();
    await expect(notes).toHaveAttribute('rows', '2');
    await expect(notes).toHaveAttribute('maxlength', '2000');
    await expect(notes).toHaveAttribute('placeholder', /.+/);
    await expect(notesCounter).toHaveText('0/2000');

    const noteText = Array.from({ length: 60 }, (_, index) => `toggle-note-${index}-${Date.now()}`).join('\n');
    await notes.fill(noteText);
    const filledNoteText = await notes.inputValue();
    await expect(notesCounter).toHaveText(`${Array.from(filledNoteText).length}/2000`);
    const notesHeight = await notes.evaluate((node) => Math.round(node.getBoundingClientRect().height));
    expect(notesHeight).toBeLessThanOrEqual(340);
    await saveToday(page);

    await page.reload();
    await expect(noteDisclosure).toHaveAttribute('open', '');
    await expect(noteSummary).toContainText(/Hide note|Скрыть заметку|Ocultar nota/);
    await expect(page.locator('#today-notes')).toHaveValue(filledNoteText);
  });

  test('BBT blur validation blocks autosave with form guidance copy', async ({ page }) => {
    await registerOwnerOnDashboard(page, 'dashboard-bbt-inline');
    await enableBBTTracking(page);

    const bbtInput = page.locator('#dashboard-bbt');
    const autosaveIndicator = page.locator('[data-dashboard-autosave-indicator]');

    await expect(bbtInput).toBeVisible();
    await bbtInput.fill('33.99');
    await bbtInput.blur();

    await expect(bbtInput).toHaveAttribute('aria-invalid', 'true');
    await expect
      .poll(async () => bbtInput.evaluate((node) => (node as HTMLInputElement).validationMessage))
      .not.toBe('');

    await expect(autosaveIndicator).toHaveAttribute('data-autosave-state', 'invalid');
    const indicatorText = String((await autosaveIndicator.textContent()) || '').trim();
    expect([
      'Fix the form errors to save',
      'Исправьте ошибки в форме для сохранения',
      'Corrige los errores del formulario para guardar',
    ]).toContain(indicatorText);
  });

  test('period/flow/symptoms/notes save and persist after reload; flow is single-select', async ({ page }) => {
    await registerOwnerOnDashboard(page, 'dashboard-save-persist');

    const periodToggle = page.locator('input[name="is_period"]');
    const flowMedium = page.locator('input[name="flow"][value="medium"]');
    const flowHeavy = page.locator('input[name="flow"][value="heavy"]');
    const notes = page.locator('#today-notes');
    const firstSymptom = todaySymptomOptions(page).nth(0);
    const secondSymptom = todaySymptomOptions(page).nth(1);

    await periodToggle.check();
    await expect(flowMedium).toBeEnabled();

    await flowMedium.check({ force: true });
    await expect(flowMedium).toBeChecked();

    await flowHeavy.check({ force: true });
    await expect(flowHeavy).toBeChecked();
    await expect(flowMedium).not.toBeChecked();

    await expect(firstSymptom).toBeVisible();
    await expect(secondSymptom).toBeVisible();
    const firstSymptomValue = await symptomInputForOption(firstSymptom).getAttribute('value');
    const secondSymptomValue = await symptomInputForOption(secondSymptom).getAttribute('value');

    expect(firstSymptomValue).toBeTruthy();
    expect(secondSymptomValue).toBeTruthy();

    await symptomChipForOption(firstSymptom).click();
    await symptomChipForOption(secondSymptom).click();
    await expect(symptomInputForOption(firstSymptom)).toBeChecked();
    await expect(symptomInputForOption(secondSymptom)).toBeChecked();

    await symptomChipForOption(secondSymptom).click();
    await expect(symptomInputForOption(firstSymptom)).toBeChecked();
    await expect(symptomInputForOption(secondSymptom)).not.toBeChecked();

    const noteText = `dashboard-note-${Date.now()}`;
    await openTodayNotes(page);
    await notes.fill(noteText);

    await saveToday(page);

    await page.reload();
    await expect(page).toHaveURL(/\/dashboard$/);

    await expect(periodToggle).toBeChecked();
    await expect(flowHeavy).toBeChecked();
    await expect(flowMedium).not.toBeChecked();
    await expect(page.locator(`label.choice-option:has(input[name="symptom_ids"][value="${firstSymptomValue}"]) input[name="symptom_ids"]`)).toBeChecked();
    await expect(page.locator(`label.choice-option:has(input[name="symptom_ids"][value="${secondSymptomValue}"]) input[name="symptom_ids"]`)).not.toBeChecked();
    await expect(notes).toHaveValue(noteText);
  });

  test('mobile dashboard symptom chips remain clickable above the bottom tabbar', async ({ page }) => {
    await registerOwnerOnDashboard(page, 'dashboard-mobile-symptoms');
    await page.setViewportSize({ width: 390, height: 844 });
    await page.reload();
    await expect(page).toHaveURL(/\/dashboard$/);

    const lastSymptom = page
      .locator('fieldset[data-dashboard-section="symptoms"] label.choice-option:visible')
      .last();
    await expect(lastSymptom).toBeVisible();
    await lastSymptom.scrollIntoViewIfNeeded();
    await symptomChipForOption(lastSymptom).click();
    await expect(symptomInputForOption(lastSymptom)).toBeChecked();
  });

  test('mobile dashboard keeps lower actions scrollable above the bottom tabbar', async ({ page }) => {
    await registerOwnerOnDashboard(page, 'dashboard-mobile-safe-area');
    await page.setViewportSize({ width: 390, height: 844 });
    await page.reload();
    await expect(page).toHaveURL(/\/dashboard$/);

    const clearButton = clearTodayButton(page);
    await clearButton.scrollIntoViewIfNeeded();
    await expectElementAboveMobileTabbar(page, clearButton);
  });

  test('switching Period day off keeps symptoms but clears flow for saved state', async ({ page }) => {
    await registerOwnerOnDashboard(page, 'dashboard-period-off');

    const periodToggle = page.locator('input[name="is_period"]');
    const flowLight = page.locator('input[name="flow"][value="light"]');
    const firstSymptom = todaySymptomOptions(page).nth(0);

    await periodToggle.check();
    await flowLight.check({ force: true });
    await symptomChipForOption(firstSymptom).click();
    await expect(symptomInputForOption(firstSymptom)).toBeChecked();

    await saveToday(page);
    await page.reload();

    await expect(periodToggle).toBeChecked();
    await periodToggle.uncheck();

    await expect(symptomInputForOption(firstSymptom)).toBeChecked();
    await expect(flowLight).toBeDisabled();

    await saveToday(page);
    await page.reload();

    await expect(periodToggle).not.toBeChecked();
    await expect(symptomInputForOption(firstSymptom)).toBeChecked();
    await expect(flowLight).not.toBeChecked();
  });

  test('clear today entry resets dashboard fields', async ({ page }) => {
    await registerOwnerOnDashboard(page, 'dashboard-clear');

    const periodToggle = page.locator('input[name="is_period"]');
    const flowMedium = page.locator('input[name="flow"][value="medium"]');
    const firstSymptom = todaySymptomOptions(page).nth(0);
    const notes = page.locator('#today-notes');

    await periodToggle.check();
    await flowMedium.check({ force: true });
    await symptomChipForOption(firstSymptom).click();
    await openTodayNotes(page);
    await notes.fill(`to-clear-${Date.now()}`);
    await saveToday(page);

    await page.reload();

    await expect(manualCycleStartButton(page)).toContainText('cycle');

    const clearButton = clearTodayButton(page);
    await expect(clearButton).toBeVisible();

    await clearButton.click();
    await expect(page.locator('#confirm-modal')).toBeVisible();
    await page.locator('#confirm-modal-accept').click();

    await expect(page).toHaveURL(/\/dashboard$/);

    await expect(periodToggle).not.toBeChecked();
    await expect(flowMedium).not.toBeChecked();
    await expect(notes).toHaveValue('');
    await expect(page.locator('input[name="symptom_ids"]:checked')).toHaveCount(0);
  });

  test('saved dashboard entry is reflected in calendar day panel', async ({ page }) => {
    await registerOwnerOnDashboard(page, 'dashboard-calendar-sync');

    const todayForm = todaySaveForm(page).first();
    const todayAction = await todayForm.getAttribute('hx-post');
    expect(todayAction).toMatch(/^\/api\/days\/\d{4}-\d{2}-\d{2}$/);

    const todayISO = String(todayAction).replace('/api/days/', '');
    const month = todayISO.slice(0, 7);
    const periodToggle = page.locator('input[name="is_period"]');
    const flowMedium = page.locator('input[name="flow"][value="medium"]');
    const notes = page.locator('#today-notes');
    const noteText = `dashboard-calendar-sync-${Date.now()}`;

    await periodToggle.check();
    await flowMedium.check({ force: true });
    await openTodayNotes(page);
    await notes.fill(noteText);
    await saveToday(page);

    await page.goto(`/calendar?month=${month}&day=${todayISO}`);
    await expect(page.locator('#day-editor')).toContainText(noteText);
    await page.locator(`[data-day-editor-open="${todayISO}"]`).click();
    const dayEditorForm = page.locator(`[data-day-editor-form][data-day-editor-date="${todayISO}"]`);
    await expect(dayEditorForm).toBeVisible();
    await expect(dayEditorForm.locator('input[name="is_period"]')).toBeChecked();
    await expect(dayEditorForm.locator('input[name="flow"][value="medium"]')).toBeChecked();
    await expect(dayEditorForm.locator('#calendar-notes')).toHaveValue(noteText);
  });

  test('mood and symptoms persist from dashboard into the calendar day summary and editor', async ({
    page,
  }) => {
    await registerOwnerOnDashboard(page, 'dashboard-mood-symptoms-sync');

    const moodFour = page.locator('input[name="mood"][value="4"]');
    const firstSymptom = todaySymptomOptions(page).nth(0);
    const firstSymptomValue = await symptomInputForOption(firstSymptom).getAttribute('value');
    const firstSymptomLabel = await firstSymptom.locator('.check-chip').getAttribute('title');

    expect(firstSymptomValue).toBeTruthy();
    expect(firstSymptomLabel).toBeTruthy();

    await moodFour.check({ force: true });
    await symptomChipForOption(firstSymptom).click();
    await saveToday(page);

    const todayAction = await todaySaveForm(page).first().getAttribute('hx-post');
    expect(todayAction).toMatch(/^\/api\/days\/\d{4}-\d{2}-\d{2}$/);

    const todayISO = String(todayAction).replace('/api/days/', '');
    const month = todayISO.slice(0, 7);
    await page.goto(`/calendar?month=${month}&day=${todayISO}`);

    const daySummary = page.locator('#day-editor');
    await expect(daySummary).toContainText('4/5');
    await expect(daySummary).toContainText(String(firstSymptomLabel));

    await page.locator(`[data-day-editor-open="${todayISO}"]`).click();
    const dayEditorForm = page.locator(`[data-day-editor-form][data-day-editor-date="${todayISO}"]`);
    await expect(dayEditorForm).toBeVisible();
    await expect(dayEditorForm.locator('input[name="mood"][value="4"]')).toBeChecked();
    await expect(dayEditorForm.locator(`input[name="symptom_ids"][value="${firstSymptomValue}"]`)).toBeChecked();
  });

  test('manual cycle start on dashboard marks today as period and survives reload', async ({ page }) => {
    await registerOwnerOnDashboard(page, 'dashboard-manual-cycle-start');

    const manualStartButton = manualCycleStartButton(page);
    await expect(manualStartButton).toBeVisible();
    await manualStartButton.click();
    await expect(page.locator('#confirm-modal')).toBeVisible();
    await page.locator('#confirm-modal-cancel').click();
    await expect(page.locator('#confirm-modal')).toBeHidden();
    await expect(page.locator('#save-status .status-error')).toHaveCount(0);

    await Promise.all([
      page.waitForResponse((response) => {
        return (
          response.request().method() === 'POST' &&
          response.url().includes('/cycle-start?source=dashboard')
        );
      }),
      (async () => {
        await manualStartButton.click();
        await expect(page.locator('#confirm-modal')).toBeVisible();
        await page.locator('#confirm-modal-accept').click();
      })(),
    ]);

    const periodToggle = page.locator('input[name="is_period"]');
    await expect(periodToggle).toBeChecked();

    await page.reload();
    await expect(periodToggle).toBeChecked();
  });

  test('quick period action toggles and persists via autosave', async ({ page }) => {
    await registerOwnerOnDashboard(page, 'dashboard-quick-period');

    const savePath = await todaySavePath(page);
    const periodToggle = page.locator('input[name="is_period"]');
    const autosaveResponse = waitForDashboardAutosave(page, savePath);
    const initiallyChecked = await periodToggle.isChecked();

    await page.locator('[data-quick-action="period"]').click();
    if (initiallyChecked) {
      await expect(periodToggle).not.toBeChecked();
    } else {
      await expect(periodToggle).toBeChecked();
    }

    await autosaveResponse;

    await page.reload();
    if (initiallyChecked) {
      await expect(periodToggle).not.toBeChecked();
    } else {
      await expect(periodToggle).toBeChecked();
    }
  });

  test('notes autosave after idle without pressing update', async ({ page }) => {
    await registerOwnerOnDashboard(page, 'dashboard-autosave-idle');

    const savePath = await todaySavePath(page);
    await openTodayNotes(page);
    const notes = page.locator('#today-notes');
    const noteText = `dashboard-autosave-${Date.now()}`;
    const autosaveResponse = waitForDashboardAutosave(page, savePath);

    await notes.fill(noteText);
    await autosaveResponse;

    await page.reload();
    await expect(notes).toHaveValue(noteText);
  });

  test('closing the page runs beforeunload autosave for dirty notes', async ({ page, context }) => {
    await registerOwnerOnDashboard(page, 'dashboard-autosave-beforeunload');

    const noteText = `dashboard-beforeunload-${Date.now()}`;

    await openTodayNotes(page);
    await page.locator('#today-notes').fill(noteText);
    await page.close({ runBeforeUnload: true });

    const replacementPage = await context.newPage();
    await replacementPage.goto('/dashboard');
    await expect(replacementPage).toHaveURL(/\/dashboard$/);
    await replacementPage.waitForTimeout(1000);
    await replacementPage.reload();
    await expect(replacementPage.locator('#today-notes')).toHaveValue(noteText);
  });
});
