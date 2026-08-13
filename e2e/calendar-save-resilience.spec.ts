import { expect, test, type Locator, type Page } from '@playwright/test';
import {
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  readRecoveryCode,
  registerOwnerViaUI,
} from './support/auth-helpers';
import { expectTextContrastAA } from './support/contrast-helpers';
import { dashboardTodayISO } from './support/dashboard-helpers';
import { saveSettingsLanguage } from './support/language-helpers';
import { localeText } from './support/locale-helpers';
import { ensureNotesFieldVisible } from './support/note-helpers';
import {
  attemptDayEditorSave,
  openCalendarDayEditor,
  shiftISODate,
} from './support/stats-helpers';
import { setRequestTimezoneFromBrowser } from './support/timezone-helpers';

/**
 * A self-hosted instance is regularly unreachable — the owner is off the home
 * network, the box is asleep — so a day-entry save that never lands is a normal
 * condition, not an edge case. What must hold when it happens: the typed entry
 * stays in the form exactly as typed, no "saved" is ever announced, the notice
 * reads as a transport hiccup rather than a medical alarm, and a visible retry
 * resends the same form. The form in the live DOM is the whole recovery
 * mechanism: nothing is persisted client-side.
 */

const SAVE_STATUS = '#calendar-save-status';
const FAILURE_NOTICE = `${SAVE_STATUS} [data-day-save-failed]`;
const RETRY_BUTTON = `${SAVE_STATUS} [data-day-save-retry]`;

type SaveOutcome = 'server-error' | 'network-failure' | 'allow';

interface DayPUTInterceptor {
  /** Attempts that reached the interceptor, including the ones let through. */
  attempts(): number;
  set(outcome: SaveOutcome): void;
}

/**
 * Routes the day-entry PUT for one date. The outcome is switchable so a single
 * test can fail a save, fail its retry, and then let the retry through — the
 * three phases of the flow under test — without re-registering the route.
 */
async function interceptDayPUT(page: Page, isoDate: string): Promise<DayPUTInterceptor> {
  let outcome: SaveOutcome = 'server-error';
  let attempts = 0;

  await page.route(`**/api/v1/days/${isoDate}`, async (route) => {
    if (route.request().method() !== 'PUT') {
      await route.fallback();
      return;
    }

    attempts += 1;
    if (outcome === 'allow') {
      await route.continue();
      return;
    }
    if (outcome === 'network-failure') {
      await route.abort('failed');
      return;
    }
    // A 500 with no body: the server did not answer with a status fragment the
    // client could show, which is what an upstream failure looks like.
    await route.fulfill({ status: 500, contentType: 'text/html; charset=utf-8', body: '' });
  });

  return {
    attempts: () => attempts,
    set: (next: SaveOutcome) => {
      outcome = next;
    },
  };
}

async function registerOwnerOnCalendar(page: Page, prefix: string): Promise<string> {
  const credentials = createCredentials(prefix);

  await registerOwnerViaUI(page, credentials);
  await expectInlineRegisterRecoveryStep(page);
  await readRecoveryCode(page);
  await continueFromRecoveryCode(page);
  await completeOnboardingIfPresent(page);
  await setRequestTimezoneFromBrowser(page);

  await page.goto('/calendar');
  await expect(page).toHaveURL(/\/calendar(?:\?.*)?$/);

  const todayButton = page.locator('button[data-day]:has(.calendar-today-pill)').first();
  await expect(todayButton).toBeVisible();
  const todayISO = await todayButton.getAttribute('data-day');
  expect(todayISO).toMatch(/^\d{4}-\d{2}-\d{2}$/);
  return String(todayISO);
}

/** Fills the editor with values a spec can recognise again field by field. */
async function fillDayEntry(form: Locator, note: string): Promise<void> {
  await form.locator('input[name="is_period"]').check();
  await form.locator('label.choice-option:has(input[name="mood"][value="4"]) .chip-round').click();
  await expect(form.locator('input[name="mood"][value="4"]')).toBeChecked();
  const notes = await ensureNotesFieldVisible(form, '#calendar-notes');
  await notes.fill(note);
}

/** Asserts the entry is still on screen exactly as it was typed. */
async function expectDayEntryStillTyped(form: Locator, note: string): Promise<void> {
  await expect(form.locator('#calendar-notes')).toHaveValue(note);
  await expect(form.locator('input[name="is_period"]')).toBeChecked();
  await expect(form.locator('input[name="mood"][value="4"]')).toBeChecked();
}

test.describe('day-entry save resilience', () => {
  test('a rejected save keeps the typed entry and no save is announced', async ({ page }) => {
    const todayISO = await registerOwnerOnCalendar(page, 'day-save-rejected');
    const targetISO = shiftISODate(todayISO, -20);
    const note = 'slept badly, headache since morning';

    const interceptor = await interceptDayPUT(page, targetISO);
    const form = await openCalendarDayEditor(page, targetISO);
    await fillDayEntry(form, note);

    await attemptDayEditorSave(page, targetISO, form);
    await expect(page.locator(FAILURE_NOTICE)).toBeVisible();

    // Requirement: nothing typed is wiped by the error path.
    await expectDayEntryStillTyped(form, note);

    // Requirement: "saved" is never optimistic. The success status is the
    // server's own fragment, so a save that the server refused must leave the
    // status container without one — before, during and after the failure.
    await expect(page.locator(`${SAVE_STATUS} .status-ok`)).toHaveCount(0);
    await expect(page.locator(SAVE_STATUS)).not.toContainText(
      localeText('en', 'common.saved_at').replace('%s', '')
    );

    // Requirement: a resubmit does not clear the form either. Bind to the
    // retry's own request so the assertions below cannot read the state left by
    // the first attempt.
    const [retryRequest] = await Promise.all([
      page.waitForRequest(
        (candidate) =>
          candidate.method() === 'PUT' && candidate.url().includes(`/api/v1/days/${targetISO}`),
      ),
      page.locator(RETRY_BUTTON).click(),
    ]);
    expect(await retryRequest.response(), 'the retry must produce a response').not.toBeNull();
    await expect(page.locator(FAILURE_NOTICE)).toBeVisible();
    expect(interceptor.attempts(), 'the retry must reach the endpoint again').toBe(2);
    await expectDayEntryStillTyped(form, note);
    await expect(page.locator(`${SAVE_STATUS} .status-ok`)).toHaveCount(0);
  });

  test('a network failure is announced calmly and the retry resends the same entry', async ({
    page,
  }) => {
    const todayISO = await registerOwnerOnCalendar(page, 'day-save-offline');
    const targetISO = shiftISODate(todayISO, -22);
    const note = 'cramps in the evening';

    const interceptor = await interceptDayPUT(page, targetISO);
    interceptor.set('network-failure');
    const form = await openCalendarDayEditor(page, targetISO);
    await fillDayEntry(form, note);

    await attemptDayEditorSave(page, targetISO, form);

    // The connection dropped mid-flight: the owner must still be told, in the
    // polite live region, and the entry must still be on screen.
    const notice = page.locator(FAILURE_NOTICE);
    await expect(notice).toBeVisible();
    await expect(page.locator(SAVE_STATUS)).toHaveAttribute('aria-live', 'polite');
    await expect(notice).toContainText(localeText('en', 'daylog.save_failed'));
    await expectDayEntryStillTyped(form, note);
    await expect(page.locator(`${SAVE_STATUS} .status-ok`)).toHaveCount(0);

    // The save button has to come back, or the retry affordance is the only way
    // out of a state the owner reached by pressing it once.
    await expect(form.locator('button[data-save-button]')).toBeEnabled();

    // Requirement: the retry resubmits the SAME form state. Let it through and
    // assert the server received exactly what is on screen.
    interceptor.set('allow');
    const [retryRequest] = await Promise.all([
      page.waitForRequest(
        (candidate) =>
          candidate.method() === 'PUT' && candidate.url().includes(`/api/v1/days/${targetISO}`),
      ),
      page.locator(RETRY_BUTTON).click(),
    ]);
    const retryResponse = await retryRequest.response();
    expect(retryResponse, 'the retry must produce a response').not.toBeNull();
    expect(retryResponse!.ok(), `the retry failed with ${retryResponse!.status()}`).toBeTruthy();
    expect(interceptor.attempts()).toBe(2);
    // htmx percent-encodes the body, so the note travels with %20 for spaces.
    const retryBody = retryRequest.postData() ?? '';
    expect(retryBody).toContain(`notes=${encodeURIComponent(note)}`);
    expect(retryBody).toContain('is_period=true');
    expect(retryBody).toContain('mood=4');

    // Only now may a save be announced. The success status is not asserted on
    // screen here: a committed save fires `calendar-day-updated`, which reloads
    // the grid and re-renders the day panel in summary mode, so the container
    // holding it is replaced within a few frames. The durable proof that this
    // attempt — and only this attempt — was the one that landed is reading the
    // entry back from the server below.
    await page.waitForLoadState('networkidle');
    await page.unroute(`**/api/v1/days/${targetISO}`);
    const reopened = await openCalendarDayEditor(page, targetISO);
    await expect(reopened.locator('#calendar-notes')).toHaveValue(note);
  });

  test('the failure notice is neutral, readable and localized, not a health alarm', async ({
    page,
  }) => {
    const todayISO = await registerOwnerOnCalendar(page, 'day-save-notice');
    const targetISO = shiftISODate(todayISO, -24);

    const interceptor = await interceptDayPUT(page, targetISO);
    interceptor.set('network-failure');
    const form = await openCalendarDayEditor(page, targetISO);
    await fillDayEntry(form, 'quiet day');
    await attemptDayEditorSave(page, targetISO, form);

    const notice = page.locator(FAILURE_NOTICE);
    await expect(notice).toBeVisible();

    // A failed save must not borrow the red error palette, which on a health
    // surface reads as a finding about the owner's body rather than about the
    // network.
    await expect(notice).not.toHaveClass(/status-error/);
    await expect(page.locator(`${SAVE_STATUS} .status-error`)).toHaveCount(0);
    await expect(page.locator(`${SAVE_STATUS} [role="alert"]`)).toHaveCount(0);
    await expect(page.locator(`${SAVE_STATUS} [aria-live="assertive"]`)).toHaveCount(0);

    // The retry is a real, visible control, not a link buried in the copy.
    await expect(page.locator(RETRY_BUTTON)).toBeVisible();
    await expect(page.locator(RETRY_BUTTON)).toHaveText(localeText('en', 'daylog.save_retry'));

    await expectTextContrastAA(page, FAILURE_NOTICE, 'day-save failure notice');
    await expectTextContrastAA(page, RETRY_BUTTON, 'day-save retry button');

    // The copy comes from the shipped catalogues, in every UI language: switch
    // the interface to a second locale and fail a save there too.
    await page.unroute(`**/api/v1/days/${targetISO}`);
    await page.goto('/settings');
    await saveSettingsLanguage(page, 'ru');

    const localizedISO = shiftISODate(todayISO, -26);
    const localizedInterceptor = await interceptDayPUT(page, localizedISO);
    localizedInterceptor.set('network-failure');
    const localizedForm = await openCalendarDayEditor(page, localizedISO);
    await fillDayEntry(localizedForm, 'тихий день');
    await attemptDayEditorSave(page, localizedISO, localizedForm);

    await expect(page.locator(FAILURE_NOTICE)).toContainText(localeText('ru', 'daylog.save_failed'));
    await expect(page.locator(RETRY_BUTTON)).toHaveText(localeText('ru', 'daylog.save_retry'));
  });
});

/**
 * The dashboard journal saves itself, so its failures arrive on the autosave's
 * fetch path rather than as htmx events. The owner must not be able to tell the
 * difference: same neutral notice, same retry, same untouched entry. A silent
 * error state on the indicator would leave a typed day looking saved.
 */
const DASHBOARD_SAVE_STATUS = '#save-status';
const DASHBOARD_FAILURE_NOTICE = `${DASHBOARD_SAVE_STATUS} [data-day-save-failed]`;
const DASHBOARD_RETRY_BUTTON = `${DASHBOARD_SAVE_STATUS} [data-day-save-retry]`;

async function registerOwnerOnDashboard(page: Page, prefix: string): Promise<string> {
  const credentials = createCredentials(prefix);

  await registerOwnerViaUI(page, credentials);
  await expectInlineRegisterRecoveryStep(page);
  await readRecoveryCode(page);
  await continueFromRecoveryCode(page);
  await completeOnboardingIfPresent(page);
  await setRequestTimezoneFromBrowser(page);

  await page.goto('/dashboard');
  await expect(page).toHaveURL(/\/dashboard$/);
  return dashboardTodayISO(page);
}

test.describe('dashboard autosave resilience', () => {
  test('an autosave that never lands keeps the entry and offers the same retry', async ({
    page,
  }) => {
    const todayISO = await registerOwnerOnDashboard(page, 'dashboard-autosave-offline');
    const note = 'walked in the evening';

    const interceptor = await interceptDayPUT(page, todayISO);
    interceptor.set('network-failure');

    const notes = await ensureNotesFieldVisible(page, '#today-notes');
    // Bind to the autosave's own request: no button is pressed here, the change
    // itself is what schedules the save.
    const failedRequest = page.waitForRequest(
      (candidate) =>
        candidate.method() === 'PUT' && candidate.url().includes(`/api/v1/days/${todayISO}`),
    );
    await notes.fill(note);
    await failedRequest;

    const notice = page.locator(DASHBOARD_FAILURE_NOTICE);
    await expect(notice).toBeVisible();
    await expect(notice).toContainText(localeText('en', 'daylog.save_failed'));
    await expect(notice).not.toHaveClass(/status-error/);
    await expect(page.locator(DASHBOARD_SAVE_STATUS)).toHaveAttribute('aria-live', 'polite');
    await expect(page.locator(`${DASHBOARD_SAVE_STATUS} .status-ok`)).toHaveCount(0);
    await expect(page.locator(`${DASHBOARD_SAVE_STATUS} [role="alert"]`)).toHaveCount(0);
    await expect(page.locator('[data-dashboard-autosave-indicator]')).not.toHaveAttribute(
      'data-autosave-state',
      'saved'
    );
    await expect(notes).toHaveValue(note);

    const retry = page.locator(DASHBOARD_RETRY_BUTTON);
    await expect(retry).toBeVisible();
    await expect(retry).toHaveText(localeText('en', 'daylog.save_retry'));

    // The retry re-enters the autosave with what is on screen.
    interceptor.set('allow');
    const [retryRequest] = await Promise.all([
      page.waitForRequest(
        (candidate) =>
          candidate.method() === 'PUT' && candidate.url().includes(`/api/v1/days/${todayISO}`),
      ),
      retry.click(),
    ]);
    const retryResponse = await retryRequest.response();
    expect(retryResponse, 'the retry must produce a response').not.toBeNull();
    expect(retryResponse!.ok(), `the retry failed with ${retryResponse!.status()}`).toBeTruthy();
    expect(retryRequest.postData() ?? '').toContain(`notes=${note.replace(/ /g, '+')}`);
    expect(interceptor.attempts(), 'the retry must reach the endpoint again').toBe(2);

    await expect(page.locator(DASHBOARD_FAILURE_NOTICE)).toHaveCount(0);
    await expect(page.locator('[data-dashboard-autosave-indicator]')).toHaveAttribute(
      'data-autosave-state',
      'saved'
    );

    await page.unroute(`**/api/v1/days/${todayISO}`);
    await page.reload();
    await expect(page.locator('#today-notes')).toHaveValue(note);
  });
});
