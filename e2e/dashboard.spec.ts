import { expect, test, type Page } from '@playwright/test';
import {
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  readRecoveryCode,
  registerOwnerViaUI,
} from './support/auth-helpers';
import { cancelConfirmDialog, mutatingRequestsDuring } from './support/confirm-dialog-helpers';
import {
  dashboardMoreDisclosure,
  dashboardStatusLine,
  openDashboardMoreFields,
  saveDashboardEntry,
} from './support/dashboard-helpers';
import { expectElementAboveMobileTabbar } from './support/mobile-layout-helpers';
import { ensureNotesFieldVisible } from './support/note-helpers';
import { setRequestTimezoneFromBrowser } from './support/timezone-helpers';
import { checkStyledControl } from './support/form-helpers';
import { everyLocaleText, localeText } from './support/locale-helpers';

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

/**
 * Makes the edits and lets the journal save itself.
 *
 * There is no save button any more: the dashboard is autosave-only, so a spec
 * hands its field interactions to the shared helper, which binds the wait to the
 * autosave request they trigger.
 */
async function saveToday(page: Page, edit: () => Promise<void>): Promise<void> {
  await saveDashboardEntry(page, edit);
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
  const action = await todaySaveForm(page).first().getAttribute('hx-put');
  expect(action).toMatch(/^\/api\/v1\/days\/\d{4}-\d{2}-\d{2}$/);
  return String(action);
}

async function waitForDashboardAutosave(
  page: Page,
  savePath?: string,
  options?: { expectIndicator?: boolean }
): Promise<void> {
  const path = savePath ?? (await todaySavePath(page));
  // Bind to the triggering request itself, not any matching response:
  // waitForResponse can resolve on a still-in-flight earlier request under
  // load (see saveDayEditorForm in calendar-autofill-clear.spec.ts). Callers
  // invoke this helper before performing the save action and await the
  // returned promise afterward, so the request listener below must be
  // registered synchronously (no prior await) — matching the original.
  const request = await page.waitForRequest((candidate) => {
    return candidate.method() === 'PUT' && candidate.url().includes(path);
  });
  const response = await request.response();
  expect(response, `expected a response for PUT ${path}`).not.toBeNull();
  expect(response!.ok(), `PUT ${path} failed with ${response!.status()}`).toBeTruthy();
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

function cycleFactorInput(page: Page, value: string) {
  return page.locator(`label.choice-option:has(input[name="cycle_factor_keys"][value="${value}"]) input[name="cycle_factor_keys"]`);
}

function cycleFactorChip(page: Page, value: string) {
  return page.locator(`label.choice-option:has(input[name="cycle_factor_keys"][value="${value}"]) .check-chip`);
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

    const action = await todayForm.getAttribute('hx-put');
    expect(action).toMatch(/^\/api\/v1\/days\/\d{4}-\d{2}-\d{2}$/);

    const serverToday = action!.replace('/api/v1/days/', '');
    const clientToday = await clientLocalISODate(page);

    expect(serverToday).toBe(clientToday);
  });

  test('notes field uses a disclosure and reopens once a note exists', async ({
    page,
  }) => {
    await registerOwnerOnDashboard(page, 'dashboard-note-disclosure');
    // The note disclosure lives inside the journal's "More" disclosure.
    await openDashboardMoreFields(page);

    const noteDisclosure = page.locator('details.note-disclosure');
    const noteSummary = noteDisclosure.locator('summary');
    const notes = page.locator('#today-notes');
    const notesCounter = page.locator('[data-dashboard-notes-field-group] [data-dashboard-notes-count]').first();
    // The `open` attribute IS the state, and the disclosure declares both label
    // variants as data attributes for the client script. Assert the rendered
    // label against the declaration instead of a three-language regex whose
    // alternation matched any of the three regardless of the page language.
    const closedLabel = ((await noteDisclosure.getAttribute('data-note-empty-text')) ?? '').trim();
    const openLabel = ((await noteDisclosure.getAttribute('data-note-open-text')) ?? '').trim();
    expect(closedLabel, 'the note disclosure must declare data-note-empty-text').not.toBe('');
    expect(openLabel, 'the note disclosure must declare data-note-open-text').not.toBe('');
    expect(openLabel).not.toBe(closedLabel);

    await expect(noteDisclosure).toHaveCount(1);
    await expect(noteDisclosure).not.toHaveAttribute('open', '');
    await expect(noteSummary).toContainText(closedLabel);
    await expect(notes).toBeHidden();

    await noteSummary.click();
    await expect(noteDisclosure).toHaveAttribute('open', '');
    await expect(noteSummary).toContainText(openLabel);
    await expect(notes).toBeVisible();
    await expect(notes).toHaveAttribute('rows', '2');
    await expect(notes).toHaveAttribute('maxlength', '2000');
    await expect(notes).toHaveAttribute('placeholder', /.+/);
    await expect(notesCounter).toHaveText('0/2000');

    const noteText = Array.from({ length: 60 }, (_, index) => `toggle-note-${index}-${Date.now()}`).join('\n');
    await saveToday(page, async () => {
      await notes.fill(noteText);
    });
    const filledNoteText = await notes.inputValue();
    await expect(notesCounter).toHaveText(`${Array.from(filledNoteText).length}/2000`);
    const notesHeight = await notes.evaluate((node) => Math.round(node.getBoundingClientRect().height));
    expect(notesHeight).toBeLessThanOrEqual(340);

    await page.reload();
    await expect(noteDisclosure).toHaveAttribute('open', '');
    await expect(noteSummary).toContainText(openLabel);
    await expect(page.locator('#today-notes')).toHaveValue(filledNoteText);
  });

  test('BBT blur validation blocks autosave with form guidance copy', async ({ page }) => {
    await registerOwnerOnDashboard(page, 'dashboard-bbt-inline');
    await enableBBTTracking(page);

    await openDashboardMoreFields(page);
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
    // The rendered language depends on the browser's Accept-Language, so the
    // expectation is every catalogue's value for the key — read from the
    // shipped catalogues, never re-typed here.
    expect(everyLocaleText('dashboard.autosave_invalid')).toContain(indicatorText);
  });

  test('period/flow/symptoms/notes save and persist after reload; flow is single-select', async ({ page }) => {
    await registerOwnerOnDashboard(page, 'dashboard-save-persist');

    const periodToggle = page.locator('input[name="is_period"]');
    const flowMedium = page.locator('input[name="flow"][value="medium"]');
    const flowHeavy = page.locator('input[name="flow"][value="heavy"]');
    const stressFactor = cycleFactorInput(page, 'stress');
    const travelFactor = cycleFactorInput(page, 'travel');
    const notes = page.locator('#today-notes');
    const firstSymptom = todaySymptomOptions(page).nth(0);
    const secondSymptom = todaySymptomOptions(page).nth(1);

    await expect(firstSymptom).toBeVisible();
    await expect(secondSymptom).toBeVisible();
    const firstSymptomValue = await symptomInputForOption(firstSymptom).getAttribute('value');
    const secondSymptomValue = await symptomInputForOption(secondSymptom).getAttribute('value');

    expect(firstSymptomValue).toBeTruthy();
    expect(secondSymptomValue).toBeTruthy();

    const noteText = `dashboard-note-${Date.now()}`;
    // Cycle factors and notes live behind the journal's "More" disclosure.
    await openDashboardMoreFields(page);
    await saveToday(page, async () => {
      await periodToggle.check();
      await expect(flowMedium).toBeEnabled();

      await checkStyledControl(flowMedium);
      await expect(flowMedium).toBeChecked();

      await checkStyledControl(flowHeavy);
      await expect(flowHeavy).toBeChecked();
      await expect(flowMedium).not.toBeChecked();

      await symptomChipForOption(firstSymptom).click();
      await symptomChipForOption(secondSymptom).click();
      await expect(symptomInputForOption(firstSymptom)).toBeChecked();
      await expect(symptomInputForOption(secondSymptom)).toBeChecked();

      await symptomChipForOption(secondSymptom).click();
      await expect(symptomInputForOption(firstSymptom)).toBeChecked();
      await expect(symptomInputForOption(secondSymptom)).not.toBeChecked();

      await cycleFactorChip(page, 'stress').click();
      await cycleFactorChip(page, 'travel').click();
      await expect(stressFactor).toBeChecked();
      await expect(travelFactor).toBeChecked();

      await openTodayNotes(page);
      await notes.fill(noteText);
    });

    await page.reload();
    await expect(page).toHaveURL(/\/dashboard$/);

    await expect(periodToggle).toBeChecked();
    await expect(flowHeavy).toBeChecked();
    await expect(flowMedium).not.toBeChecked();
    await expect(page.locator(`label.choice-option:has(input[name="symptom_ids"][value="${firstSymptomValue}"]) input[name="symptom_ids"]`)).toBeChecked();
    await expect(page.locator(`label.choice-option:has(input[name="symptom_ids"][value="${secondSymptomValue}"]) input[name="symptom_ids"]`)).not.toBeChecked();
    await expect(stressFactor).toBeChecked();
    await expect(travelFactor).toBeChecked();
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

  test('mobile symptom grid keeps two columns and real tap targets', async ({ page }) => {
    await registerOwnerOnDashboard(page, 'dashboard-mobile-symptom-grid');
    await page.setViewportSize({ width: 390, height: 844 });
    await page.reload();
    await expect(page).toHaveURL(/\/dashboard$/);

    const grid = page.locator('fieldset[data-dashboard-section="symptoms"] .symptom-grid').first();
    const columns = await grid.evaluate(
      (node) => window.getComputedStyle(node).gridTemplateColumns.split(' ').filter(Boolean).length
    );
    expect(columns, 'the symptom grid must show two columns at 390px').toBe(2);

    // Narrower chips must not buy their width from the tap target: the box
    // grows and the label wraps instead.
    const chips = await page
      .locator('fieldset[data-dashboard-section="symptoms"] label.choice-option:visible .check-chip')
      .all();
    expect(chips.length).toBeGreaterThan(1);
    for (const chip of chips) {
      const box = await chip.boundingBox();
      expect(box, 'a visible symptom chip must have a box').not.toBeNull();
      expect(box!.height, 'a symptom chip must stay at least 44px tall').toBeGreaterThanOrEqual(44);
    }
  });

  test('rare journal fields wait behind More, which opens for a day that holds one', async ({
    page,
  }) => {
    await registerOwnerOnDashboard(page, 'dashboard-more-disclosure');
    await enableBBTTracking(page);

    // Onboarding fills the current period, so today starts with data. Clear it:
    // the closed state is a claim about a day holding none of these fields.
    await clearTodayButton(page).click();
    await expect(page.locator('#confirm-modal')).toBeVisible();
    await page.locator('#confirm-modal-accept').click();
    await expect(page).toHaveURL(/\/dashboard$/);

    const more = dashboardMoreDisclosure(page);
    await expect(more).toHaveCount(1);
    await expect(more).not.toHaveAttribute('open', '');
    await expect(more.locator('> summary')).toContainText(localeText('en', 'dashboard.more_fields'));

    // The tier a day is usually logged with stays in the open; the rest waits.
    await expect(page.locator('input[name="is_period"]')).toBeAttached();
    await expect(todaySymptomOptions(page).first()).toBeVisible();
    await expect(page.locator('#dashboard-bbt')).toBeHidden();

    // An anchor into the disclosure must not land on nothing: the late-cycle
    // actions link straight at the pregnancy test, which now lives inside it.
    // Measured on a day that holds none of these fields, so the server is not
    // the one opening it.
    await page.goto('/dashboard#dashboard-pregnancy-test');
    await expect(dashboardMoreDisclosure(page)).toHaveAttribute('open', '');
    await page.goto('/dashboard');
    await expect(dashboardMoreDisclosure(page)).not.toHaveAttribute('open', '');

    await openDashboardMoreFields(page);
    const bbtInput = page.locator('#dashboard-bbt');
    await saveToday(page, async () => {
      await bbtInput.fill('36.60');
      await bbtInput.blur();
    });

    // Fields inside still save through the autosave, and what was recorded is
    // never left behind a closed disclosure.
    await page.reload();
    await expect(dashboardMoreDisclosure(page)).toHaveAttribute('open', '');
    await expect(page.locator('#dashboard-bbt')).toHaveValue('36.60');
  });

  test('a saved pregnancy result is removed by a button, not by a third radio', async ({
    page,
  }) => {
    await registerOwnerOnDashboard(page, 'dashboard-pregnancy-remove');
    await openDashboardMoreFields(page);

    const field = page.locator('[data-pregnancy-test]');
    await expect(field).toHaveAttribute('data-pregnancy-test-state', 'absent');
    // The group offers the two results and nothing else — the unset value rides
    // a hidden carrier, which is not in the accessibility tree.
    await expect(field.getByRole('radio')).toHaveCount(2);
    await expect(field.locator('[data-pregnancy-test-remove]')).toHaveCount(0);

    await saveToday(page, async () => {
      await field.locator('[data-pregnancy-test-option="negative"]').click();
    });

    await page.reload();
    await expect(dashboardMoreDisclosure(page)).toHaveAttribute('open', '');

    const savedField = page.locator('[data-pregnancy-test]');
    await expect(savedField).toHaveAttribute('data-pregnancy-test-state', 'recorded');
    await expect(savedField.getByRole('radio')).toHaveCount(2);
    const remove = savedField.getByRole('button', {
      name: localeText('en', 'dashboard.pregnancy_test.remove'),
    });
    await expect(remove).toHaveAttribute('type', 'button');

    // There is no save button on this screen: the removal marks the journal
    // dirty and the autosave carries it.
    await saveToday(page, async () => {
      await remove.click();
    });
    await expect(savedField).toHaveAttribute('data-pregnancy-test-state', 'absent');

    await page.reload();
    await expect(page.locator('input[name="pregnancy_test"][value="negative"]')).not.toBeChecked();
    await expect(page.locator('[data-pregnancy-test]')).toHaveAttribute(
      'data-pregnancy-test-state',
      'absent'
    );
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

    await saveToday(page, async () => {
      await periodToggle.check();
      await checkStyledControl(flowLight);
      await symptomChipForOption(firstSymptom).click();
      await expect(symptomInputForOption(firstSymptom)).toBeChecked();
    });
    await page.reload();

    await expect(periodToggle).toBeChecked();
    await saveToday(page, async () => {
      await periodToggle.uncheck();
    });

    await expect(symptomInputForOption(firstSymptom)).toBeChecked();
    await expect(flowLight).toBeDisabled();

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

    const noteText = `to-clear-${Date.now()}`;

    await saveToday(page, async () => {
      await periodToggle.check();
      await checkStyledControl(flowMedium);
      await symptomChipForOption(firstSymptom).click();
      await openTodayNotes(page);
      await notes.fill(noteText);
    });

    await page.reload();

    // Positive anchor: the entry really is populated after the save and the
    // reload. Without it the post-clear assertions below pass on an editor that
    // was never filled — an unsaved form and a cleared one look identical.
    await expect(periodToggle).toBeChecked();
    await expect(flowMedium).toBeChecked();
    await expect(symptomInputForOption(firstSymptom)).toBeChecked();
    await expect(notes).toHaveValue(noteText);

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
    const todayAction = await todayForm.getAttribute('hx-put');
    expect(todayAction).toMatch(/^\/api\/v1\/days\/\d{4}-\d{2}-\d{2}$/);

    const todayISO = String(todayAction).replace('/api/v1/days/', '');
    const month = todayISO.slice(0, 7);
    const periodToggle = page.locator('input[name="is_period"]');
    const flowMedium = page.locator('input[name="flow"][value="medium"]');
    const notes = page.locator('#today-notes');
    const noteText = `dashboard-calendar-sync-${Date.now()}`;

    await saveToday(page, async () => {
      await periodToggle.check();
      await checkStyledControl(flowMedium);
      await openTodayNotes(page);
      await notes.fill(noteText);
    });

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

    await saveToday(page, async () => {
      await checkStyledControl(moodFour);
      await symptomChipForOption(firstSymptom).click();
    });

    const todayAction = await todaySaveForm(page).first().getAttribute('hx-put');
    expect(todayAction).toMatch(/^\/api\/v1\/days\/\d{4}-\d{2}-\d{2}$/);

    const todayISO = String(todayAction).replace('/api/v1/days/', '');
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

    // Cancel must issue nothing at all. An empty #save-status .status-error is a
    // weaker claim than it looks — it also holds when the POST fired and
    // succeeded — so record the wire instead, and reload inside the window so any
    // request the cancelled click was going to issue has had its chance.
    const cancelledRequests = await mutatingRequestsDuring(
      page,
      (pathname) => pathname.endsWith('/cycle-start'),
      async () => {
        await manualStartButton.click();
        await cancelConfirmDialog(page);

        await page.reload();
        await expect(manualStartButton).toBeVisible();
      }
    );
    expect(cancelledRequests, 'cancelling the manual cycle start must issue no request').toEqual([]);

    const [request] = await Promise.all([
      page.waitForRequest(
        (candidate) =>
          candidate.method() === 'POST' && candidate.url().includes('/cycle-start?source=dashboard'),
      ),
      (async () => {
        await manualStartButton.click();
        await expect(page.locator('#confirm-modal')).toBeVisible();
        await page.locator('#confirm-modal-accept').click();
      })(),
    ]);
    const response = await request.response();
    expect(response, 'expected a response for POST /cycle-start?source=dashboard').not.toBeNull();
    expect(
      response!.ok(),
      `POST /cycle-start?source=dashboard failed with ${response!.status()}`
    ).toBeTruthy();

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

  test('the journal saves without a button and offers one step back', async ({ page }) => {
    await registerOwnerOnDashboard(page, 'dashboard-autosave-undo');

    const form = todaySaveForm(page).first();
    const indicator = page.locator('[data-dashboard-autosave-indicator]');
    const notes = page.locator('#today-notes');

    // One save model on this screen: there is nothing to press.
    await expect(form.locator('[data-save-button]')).toHaveCount(0);
    // Idle says nothing at all.
    await expect(indicator).toHaveAttribute('data-autosave-state', 'idle');
    await expect(indicator).toHaveText('');

    const savePath = await todaySavePath(page);
    const firstNote = `undo-first-${Date.now()}`;
    await saveToday(page, async () => {
      await openTodayNotes(page);
      await notes.fill(firstNote);
    });

    // "Saved" only after the server answered, and it comes with the way back.
    await expect(indicator).toHaveAttribute('data-autosave-state', 'saved');
    await expect(indicator).toContainText(localeText('en', 'dashboard.autosave_saved'));
    const undo = indicator.locator('[data-dashboard-autosave-undo]');
    await expect(undo).toBeVisible();
    await expect(undo).toHaveText(localeText('en', 'dashboard.autosave_undo'));
    await expect(undo).toHaveAttribute('type', 'button');

    const secondNote = `undo-second-${Date.now()}`;
    await saveToday(page, async () => {
      await notes.fill(secondNote);
    });

    // Undo re-saves the state the server held before that save, through the
    // same path — bind to its own request, not to the page settling.
    const [undoRequest] = await Promise.all([
      page.waitForRequest(
        (candidate) => candidate.method() === 'PUT' && candidate.url().includes(savePath),
      ),
      indicator.locator('[data-dashboard-autosave-undo]').click(),
    ]);
    const undoResponse = await undoRequest.response();
    expect(undoResponse, `expected a response for PUT ${savePath}`).not.toBeNull();
    expect(undoResponse!.ok(), `the undo failed with ${undoResponse!.status()}`).toBeTruthy();
    expect(undoRequest.postData() ?? '').toContain(`notes=${firstNote}`);

    await expect(notes).toHaveValue(firstNote);
    // Depth one: there is no undoing of an undo.
    await expect(indicator.locator('[data-dashboard-autosave-undo]')).toHaveCount(0);

    await page.reload();
    await expect(page.locator('#today-notes')).toHaveValue(firstNote);
  });

  test('undoing the first save of an empty day clears the day again', async ({ page }) => {
    await registerOwnerOnDashboard(page, 'dashboard-undo-clears-day');

    // Onboarding auto-fills the current period, so today starts with data. Clear
    // it first: the case under test is the first save of a day that holds none.
    await clearTodayButton(page).click();
    await expect(page.locator('#confirm-modal')).toBeVisible();
    await page.locator('#confirm-modal-accept').click();
    await expect(page).toHaveURL(/\/dashboard$/);
    await expect(clearTodayButton(page)).toHaveCount(0);

    const savePath = await todaySavePath(page);
    await expect(todaySaveForm(page).first()).toHaveAttribute('data-today-entry-exists', 'false');

    const periodToggle = page.locator('input[name="is_period"]');
    await saveToday(page, async () => {
      await periodToggle.check();
    });

    // An empty day is an absent entry, so the step back is the clear-today
    // DELETE rather than an upsert of blank values.
    const [deleteRequest] = await Promise.all([
      page.waitForRequest(
        (candidate) => candidate.method() === 'DELETE' && candidate.url().includes(savePath),
      ),
      page.locator('[data-dashboard-autosave-undo]').click(),
    ]);
    const deleteResponse = await deleteRequest.response();
    expect(deleteResponse, `expected a response for DELETE ${savePath}`).not.toBeNull();
    expect(deleteResponse!.ok(), `the undo failed with ${deleteResponse!.status()}`).toBeTruthy();

    await expect(page).toHaveURL(/\/dashboard$/);
    await expect(page.locator('input[name="is_period"]')).not.toBeChecked();
    await expect(clearTodayButton(page)).toHaveCount(0);
  });

  test('usage-goal chips change the mode from the dashboard without visiting settings', async ({
    page,
  }) => {
    await registerOwnerOnDashboard(page, 'dashboard-usage-goal-switch');

    const summary = page.locator('[data-usage-goal-summary]');
    const chip = page.locator('[data-usage-goal-chip]');
    const quickSwitch = page.locator('[data-usage-goal-quick-switch]');
    await expect(summary).toHaveAttribute('data-usage-goal-label-key', 'settings.goal.health');
    // The header carries the mode as a chip; the two-mode switch is one click
    // away, behind it.
    await expect(chip).toContainText(localeText('en', 'settings.goal.health'));
    await chip.click();
    await expect(quickSwitch).toBeVisible();

    // The mode already in force is not offered again; the other two are.
    await expect(quickSwitch.locator('[data-usage-goal-choice="health"]')).toHaveCount(0);
    const avoidChip = quickSwitch.locator('[data-usage-goal-choice="avoid_pregnancy"]');
    await expect(avoidChip).toHaveText(localeText('en', 'settings.goal.avoid'));

    await avoidChip.click();

    // The server answers with a re-render, so the whole page speaks in the new
    // mode: the summary, and the chips now offering the way back.
    await expect(summary).toHaveAttribute('data-usage-goal-label-key', 'settings.goal.avoid');
    await expect(summary).toHaveAttribute('data-usage-goal-summary-key', 'usage_goal.summary.avoid');
    await expect(quickSwitch.locator('[data-usage-goal-choice="health"]')).toHaveCount(1);
    await expect(quickSwitch.locator('[data-usage-goal-choice="avoid_pregnancy"]')).toHaveCount(0);

    // It is a real save, not a client-side flip: it survives a reload and the
    // settings form shows the same answer.
    await page.reload();
    await expect(summary).toHaveAttribute('data-usage-goal-label-key', 'settings.goal.avoid');

    await page.goto('/settings');
    await expect(
      page.locator('#settings-cycle input[name="usage_goal"][value="avoid_pregnancy"]')
    ).toBeChecked();
  });

  test('the goal about timing gets the ovulation estimate and the temperature in the open', async ({
    page,
  }) => {
    await registerOwnerOnDashboard(page, 'dashboard-timing-frame');
    await enableBBTTracking(page);

    const statusLine = dashboardStatusLine(page);
    const ovulation = statusLine.locator('[data-dashboard-ovulation]');
    const bbtInput = page.locator('#dashboard-bbt');

    // The default goal is the neutral one: no timing line, temperature folded
    // away with the other rare fields.
    await expect(ovulation).toHaveCount(0);
    await expect(bbtInput).toBeHidden();

    await page.locator('[data-usage-goal-chip]').click();
    await page.locator('[data-usage-goal-choice="trying_to_conceive"]').click();
    await expect(page.locator('[data-usage-goal-summary]')).toHaveAttribute(
      'data-usage-goal-label-key',
      'settings.goal.trying'
    );

    // What the goal promised at onboarding, on the next load of the page.
    await page.reload();
    await expect(statusLine).toContainText(localeText('en', 'dashboard.ovulation'));
    await expect(ovulation).toHaveCount(1);
    await expect(bbtInput).toBeVisible();
    await expect(dashboardMoreDisclosure(page).locator('#dashboard-bbt')).toHaveCount(0);

    // A field in the visible tier still saves itself, and what was recorded
    // reads back from the same place.
    await saveToday(page, async () => {
      await bbtInput.fill('36.60');
      await bbtInput.blur();
    });
    await page.reload();
    await expect(page.locator('#dashboard-bbt')).toHaveValue('36.60');
    await expect(dashboardMoreDisclosure(page).locator('#dashboard-bbt')).toHaveCount(0);
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
    await expect.poll(async () => {
      await replacementPage.reload();
      return replacementPage.locator('#today-notes').inputValue();
    }, { timeout: 5000 }).toBe(noteText);
  });
});
