import { expect, test, type Page } from '@playwright/test';
import {
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  readRecoveryCode,
  registerOwnerViaUI,
} from './support/auth-helpers';
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

function todaySaveForm(page: Page) {
  return page.locator('[data-dashboard-save-form]');
}

async function openTodayNotes(page: Page): Promise<void> {
  const disclosure = page.locator('details.note-disclosure');
  const isOpen = await disclosure.evaluate((element) => element.hasAttribute('open'));
  if (!isOpen) {
    await disclosure.locator('summary').click();
  }
  await expect(page.locator('#today-notes')).toBeVisible();
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

    const dateLabel = page.locator('section.journal-card p.journal-muted').first();
    await expect(dateLabel).toBeVisible();
    await expect(dateLabel).not.toHaveText(/^$/);
  });

  test('note disclosure toggles between add, hide, and edit labels and can collapse again', async ({
    page,
  }) => {
    await registerOwnerOnDashboard(page, 'dashboard-note-disclosure');

    const disclosure = page.locator('details.note-disclosure');
    const summary = disclosure.locator('summary');
    const label = disclosure.locator('[data-note-disclosure-label]');
    const emptyText = String(await disclosure.getAttribute('data-note-empty-text'));
    const openText = String(await disclosure.getAttribute('data-note-open-text'));
    const filledText = String(await disclosure.getAttribute('data-note-filled-text'));

    await expect(label).toHaveText(emptyText);
    await expect(summary).toHaveAttribute('aria-expanded', 'false');

    await summary.click();
    await expect(disclosure).toHaveAttribute('open', '');
    await expect(summary).toHaveAttribute('aria-expanded', 'true');
    await expect(label).toHaveText(openText);

    await page.locator('#today-notes').fill(`toggle-note-${Date.now()}`);
    await summary.click();
    await expect(disclosure).not.toHaveAttribute('open', '');
    await expect(summary).toHaveAttribute('aria-expanded', 'false');
    await expect(label).toHaveText(filledText);
  });

  test('period/flow/symptoms/notes save and persist after reload; flow is single-select', async ({ page }) => {
    await registerOwnerOnDashboard(page, 'dashboard-save-persist');

    const periodToggle = page.locator('input[name="is_period"]');
    const flowMedium = page.locator('input[name="flow"][value="medium"]');
    const flowHeavy = page.locator('input[name="flow"][value="heavy"]');
    const notes = page.locator('#today-notes');
    const symptoms = page.locator('input[name="symptom_ids"]');

    await periodToggle.check();
    await expect(flowMedium).toBeEnabled();

    await flowMedium.check({ force: true });
    await expect(flowMedium).toBeChecked();

    await flowHeavy.check({ force: true });
    await expect(flowHeavy).toBeChecked();
    await expect(flowMedium).not.toBeChecked();

    await expect(symptoms.first()).toBeEnabled();
    const firstSymptomValue = await symptoms.nth(0).getAttribute('value');
    const secondSymptomValue = await symptoms.nth(1).getAttribute('value');

    expect(firstSymptomValue).toBeTruthy();
    expect(secondSymptomValue).toBeTruthy();

    await symptoms.nth(0).check({ force: true });
    await symptoms.nth(1).check({ force: true });
    await expect(symptoms.nth(0)).toBeChecked();
    await expect(symptoms.nth(1)).toBeChecked();

    await symptoms.nth(1).uncheck({ force: true });
    await expect(symptoms.nth(0)).toBeChecked();
    await expect(symptoms.nth(1)).not.toBeChecked();

    const noteText = `dashboard-note-${Date.now()}`;
    await openTodayNotes(page);
    await notes.fill(noteText);

    await saveToday(page);

    await page.reload();
    await expect(page).toHaveURL(/\/dashboard$/);

    await expect(periodToggle).toBeChecked();
    await expect(flowHeavy).toBeChecked();
    await expect(flowMedium).not.toBeChecked();
    await expect(page.locator(`input[name="symptom_ids"][value="${firstSymptomValue}"]`)).toBeChecked();
    await expect(page.locator(`input[name="symptom_ids"][value="${secondSymptomValue}"]`)).not.toBeChecked();
    await expect(notes).toHaveValue(noteText);
  });

  test('switching Period day off keeps symptoms but clears flow for saved state', async ({ page }) => {
    await registerOwnerOnDashboard(page, 'dashboard-period-off');

    const periodToggle = page.locator('input[name="is_period"]');
    const flowLight = page.locator('input[name="flow"][value="light"]');
    const symptoms = page.locator('input[name="symptom_ids"]');

    await periodToggle.check();
    await flowLight.check({ force: true });
    await symptoms.nth(0).check({ force: true });
    await expect(symptoms.nth(0)).toBeChecked();

    await saveToday(page);
    await page.reload();

    await expect(periodToggle).toBeChecked();
    await periodToggle.uncheck();

    await expect(symptoms.nth(0)).toBeChecked();
    await expect(flowLight).toBeDisabled();

    await saveToday(page);
    await page.reload();

    await expect(periodToggle).not.toBeChecked();
    await expect(symptoms.nth(0)).toBeChecked();
    await expect(flowLight).not.toBeChecked();
  });

  test('clear today entry resets dashboard fields', async ({ page }) => {
    await registerOwnerOnDashboard(page, 'dashboard-clear');

    const periodToggle = page.locator('input[name="is_period"]');
    const flowMedium = page.locator('input[name="flow"][value="medium"]');
    const symptoms = page.locator('input[name="symptom_ids"]');
    const notes = page.locator('#today-notes');

    await periodToggle.check();
    await flowMedium.check({ force: true });
    await symptoms.nth(0).check({ force: true });
    await openTodayNotes(page);
    await notes.fill(`to-clear-${Date.now()}`);
    await saveToday(page);

    await page.reload();

    const secondaryActions = page.locator('.dashboard-secondary-actions');
    const dangerActions = page.locator('.dashboard-danger-actions');
    await expect(secondaryActions.locator('button[type="submit"]')).toContainText('cycle');

    const clearButton = dangerActions.locator('[data-dashboard-clear-button]');
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
    const symptoms = page.locator('input[name="symptom_ids"]');
    const firstSymptomValue = await symptoms.nth(0).getAttribute('value');
    const firstSymptomLabel = await symptoms.nth(0).evaluate((input) => {
      const label = input.closest('label');
      const chip = label ? label.querySelector('.check-chip') : null;
      return chip ? chip.getAttribute('title') : null;
    });

    expect(firstSymptomValue).toBeTruthy();
    expect(firstSymptomLabel).toBeTruthy();

    await moodFour.check({ force: true });
    await symptoms.nth(0).check({ force: true });
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

    const manualStartButton = page.locator('.dashboard-secondary-form button[type="submit"]');
    await expect(manualStartButton).toBeVisible();
    await Promise.all([
      page.waitForResponse((response) => {
        return (
          response.request().method() === 'POST' &&
          response.url().includes('/cycle-start?source=dashboard')
        );
      }),
      manualStartButton.click(),
    ]);

    const periodToggle = page.locator('input[name="is_period"]');
    await expect(periodToggle).toBeChecked();

    await page.reload();
    await expect(periodToggle).toBeChecked();
  });
});
