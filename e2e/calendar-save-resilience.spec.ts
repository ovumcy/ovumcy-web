import {
  expect,
  test,
  type Browser,
  type BrowserErrorWatcher,
  type Locator,
  type Page,
} from './support/fixtures';
import {
  apiOriginHeader,
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  createCredentials,
  expectDedicatedRecoveryPage,
  expectInlineRegisterRecoveryStep,
  loginViaUI,
  readRecoveryCode,
  registerOwnerViaUI,
} from './support/auth-helpers';
import { expectTextContrastAA } from './support/contrast-helpers';
import { dashboardTodayISO } from './support/dashboard-helpers';
import { saveSettingsLanguage } from './support/language-helpers';
import { localeText, type Locale } from './support/locale-helpers';
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
 * htmx logs its own request-failure events on the console: `htmx:sendError`
 * and `htmx:afterRequest` when the connection drops, and
 * `Response Status Error Code NNN from …` when the server refuses. Every save
 * this file breaks produces them by construction, so they are tolerated where
 * the breakage is armed rather than suppressed suite-wide — a save failing
 * anywhere else still fails its test.
 *
 * The refusal log names the URL, so the allowance is scoped to the very path
 * the interception is breaking. Left as a bare `Response Status Error Code `
 * prefix it would also swallow an htmx-reported failure from an unrelated
 * endpoint during the same test — a real 500 elsewhere would merge green here,
 * because these tests assert about the day form and nothing else. The
 * connection-drop logs carry no URL and stay as they are; `route.abort` is the
 * only thing in these tests that drops a connection.
 */
const HTMX_CONNECTION_DROP_LOG = /^console\.error: htmx:(?:sendError|afterRequest)/;

const htmxRefusalLogFor = (path: string): RegExp =>
  new RegExp(`^console\\.error: Response Status Error Code \\d+ from ${path.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}`);

/**
 * Routes the day-entry PUT for one date. The outcome is switchable so a single
 * test can fail a save, fail its retry, and then let the retry through — the
 * three phases of the flow under test — without re-registering the route.
 */
async function interceptDayPUT(
  page: Page,
  isoDate: string,
  browserErrors: BrowserErrorWatcher
): Promise<DayPUTInterceptor> {
  // Armed from construction, because `outcome` starts at `server-error`: this
  // interceptor can refuse from its first request, so the refusal it will log is
  // already the behaviour under test.
  browserErrors.allow(
    htmxRefusalLogFor(`/api/v1/days/${isoDate}`),
    `this test forces PUT /api/v1/days/${isoDate} to be refused, and htmx logging that refusal is the behaviour under test`
  );

  let outcome: SaveOutcome = 'server-error';
  let attempts = 0;
  let dropTolerated = false;

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
      // The connection-drop log carries no URL, so it cannot be scoped by path
      // the way the refusal is. It is scoped by ARMING instead: a test that
      // never switches this interceptor to `network-failure` never drops a
      // connection, and must not tolerate one — a real drop elsewhere would
      // otherwise pass unreported, since these tests assert about the day form
      // and nothing else.
      if (next === 'network-failure' && !dropTolerated) {
        browserErrors.allow(
          HTMX_CONNECTION_DROP_LOG,
          `this test aborts PUT /api/v1/days/${isoDate}, and htmx logging the dropped connection is the behaviour under test`
        );
        dropTolerated = true;
      }
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
  test('a reload while the save is still on the wire keeps the entry', async ({ page }) => {
    // Registration, the editor round-trip and a read-back poll in one test.
    test.slow();
    const todayISO = await registerOwnerOnCalendar(page, 'day-save-inflight-reload');
    const targetISO = shiftISODate(todayISO, -24);
    const note = 'left the page mid-save';

    // Hold the explicit Save, so the reload lands while it is undelivered —
    // a slow uplink, made deterministic.
    let releaseSave = (): void => {};
    const saveReleased = new Promise<void>((resolve) => {
      releaseSave = resolve;
    });
    await page.route(`**/api/v1/days/${targetISO}`, async (route) => {
      if (route.request().method() !== 'PUT') {
        await route.fallback();
        return;
      }
      await saveReleased;
      // A request the reload cancelled cannot be continued; that is the loss
      // the read-back below reports.
      await route.continue().catch(() => undefined);
    });
    const dialogs: string[] = [];
    page.on('dialog', (dialog) => {
      dialogs.push(dialog.type());
      void dialog.accept();
    });

    const form = await openCalendarDayEditor(page, targetISO);
    await fillDayEntry(form, note);
    await attemptDayEditorSave(page, targetISO, form);
    await expect(form.locator('button[data-save-button]')).toBeDisabled();

    await page.reload();
    releaseSave();

    // Read back what the server holds, not what a page renders.
    await expect.poll(async () => {
      const response = await page.request.get(`/api/v1/days/${targetISO}`, {
        headers: { Accept: 'application/json', ...apiOriginHeader(page) },
      });
      if (!response.ok()) {
        return `HTTP ${response.status()}`;
      }
      return ((await response.json()) as { notes?: string }).notes;
    }, { message: 'the entry saved before the reload', timeout: 10000 }).toBe(note);
    expect(dialogs, 'leaving while the save is open asks first').toContain('beforeunload');
  });

  test('a rejected save keeps the typed entry and no save is announced', async ({ browserErrors, page }) => {
    const todayISO = await registerOwnerOnCalendar(page, 'day-save-rejected');
    const targetISO = shiftISODate(todayISO, -20);
    const note = 'slept badly, headache since morning';

    const interceptor = await interceptDayPUT(page, targetISO, browserErrors);
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
    browserErrors,
    page,
  }) => {
    const todayISO = await registerOwnerOnCalendar(page, 'day-save-offline');
    const targetISO = shiftISODate(todayISO, -22);
    const note = 'cramps in the evening';

    const interceptor = await interceptDayPUT(page, targetISO, browserErrors);
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
    browserErrors,
    page,
  }) => {
    const todayISO = await registerOwnerOnCalendar(page, 'day-save-notice');
    const targetISO = shiftISODate(todayISO, -24);

    const interceptor = await interceptDayPUT(page, targetISO, browserErrors);
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
    const localizedInterceptor = await interceptDayPUT(page, localizedISO, browserErrors);
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
    browserErrors,
    page,
  }) => {
    const todayISO = await registerOwnerOnDashboard(page, 'dashboard-autosave-offline');
    const note = 'walked in the evening';

    const interceptor = await interceptDayPUT(page, todayISO, browserErrors);
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

/**
 * A session can end while the owner is typing — another device changed the
 * account's security posture, which revokes every other session. The save is
 * then refused with the auth guard's 401, and signing in again in the SAME tab
 * would navigate away from the only copy of the entry. The notice therefore
 * offers the sign-in page in a new tab; the entry stays in this page's form,
 * and the retry beside the link lands once the new tab has signed in. Nothing
 * from the entry is stored in the browser to survive the trip.
 */
const SESSION_EXPIRED_KEY = 'common.error.unauthorized';

/** Signs the same account in on a second device and revokes every other session from there. */
async function revokeSessionFromAnotherDevice(
  browser: Browser,
  browserErrors: BrowserErrorWatcher,
  credentials: { email: string; password: string }
): Promise<void> {
  const otherContext = await browser.newContext();
  browserErrors.watch(otherContext);
  try {
    const otherPage = await otherContext.newPage();
    await loginViaUI(otherPage, credentials);
    await expect(otherPage).toHaveURL(/\/dashboard(?:\?.*)?$/);
    await otherPage.goto('/settings');
    // Regenerating the recovery code bumps the account's session version and
    // re-issues a cookie to this device only.
    const form = otherPage.locator('form[action="/api/v1/users/current/recovery-code"]');
    await form.locator('#settings-recovery-code-password').fill(credentials.password);
    await form.locator('button[type="submit"]').click();
    await expect(otherPage.locator('#confirm-modal')).toBeVisible();
    await otherPage.locator('#confirm-modal-accept').click();
    await expectDedicatedRecoveryPage(otherPage);
  } finally {
    await otherContext.close();
  }
}

/** Follows the notice's sign-in link into the tab it opens and signs in there. */
async function signInThroughNoticeLink(
  page: Page,
  link: Locator,
  credentials: { email: string; password: string }
): Promise<void> {
  const [signInTab] = await Promise.all([page.context().waitForEvent('page'), link.click()]);
  await signInTab.waitForLoadState();
  await expect(signInTab).toHaveURL(/\/login(?:\?.*)?$/);
  await signInTab.locator('#login-email').fill(credentials.email);
  await signInTab.locator('#login-password').fill(credentials.password);
  await signInTab.locator('form[action="/api/v1/sessions"] button[type="submit"]').click();
  await expect(signInTab).toHaveURL(/\/dashboard(?:\?.*)?$/);
  await signInTab.close();
}

async function expectSignInLink(page: Page, notice: Locator, locale: Locale): Promise<Locator> {
  await expect(notice.locator(`[data-notice-key="${SESSION_EXPIRED_KEY}"]`)).toHaveText(
    localeText(locale, SESSION_EXPIRED_KEY)
  );
  const link = notice.locator('a[data-day-save-sign-in]');
  await expect(link).toBeVisible();
  await expect(link).toHaveText(localeText(locale, 'daylog.save_sign_in'));
  await expect(link).toHaveAttribute('target', '_blank');
  await expect(link).toHaveAttribute('rel', 'noopener');
  // Same origin, fixed path: nothing from the entry or the account in the URL.
  const href = await link.evaluate((anchor) => (anchor as HTMLAnchorElement).href);
  expect(new URL(href).origin).toBe(new URL(page.url()).origin);
  expect(new URL(href).pathname + new URL(href).search).toBe('/login');
  await expect(notice.locator('[data-day-save-retry]')).toHaveText(localeText(locale, 'daylog.save_retry'));
  return link;
}

/** No draft may be stashed client-side: the live form is the whole mechanism. */
async function expectNothingStored(page: Page, note: string): Promise<void> {
  const stored = await page.evaluate(() =>
    [window.localStorage, window.sessionStorage]
      .flatMap((storage) => Array.from({ length: storage.length }, (_, index) => storage.getItem(storage.key(index) ?? '') ?? ''))
      .concat(document.cookie)
      .join('\n')
  );
  expect(stored).not.toContain(note);
  const cookies = await page.context().cookies();
  for (const cookie of cookies) {
    expect(decodeURIComponent(cookie.value)).not.toContain(note);
  }
}

async function csrfCookieValue(page: Page): Promise<string | undefined> {
  return (await page.context().cookies()).find((cookie) => cookie.name === 'ovumcy_csrf')?.value;
}

test.describe('a day save refused for an ended session', () => {
  test('dashboard (fr): the notice offers sign-in in a new tab and the retry then saves the entry', async ({
    browser,
    browserErrors,
    page,
  }) => {
    // Two devices, two sign-ins and a recovery-code rotation in one flow.
    test.slow();
    const credentials = createCredentials('day-save-session-fr');
    await registerOwnerViaUI(page, credentials);
    await expectInlineRegisterRecoveryStep(page);
    await readRecoveryCode(page);
    await continueFromRecoveryCode(page);
    await completeOnboardingIfPresent(page);
    await setRequestTimezoneFromBrowser(page);
    await page.goto('/settings');
    await saveSettingsLanguage(page, 'fr');
    await page.goto('/dashboard');
    await expect(page).toHaveURL(/\/dashboard$/);
    const todayISO = await dashboardTodayISO(page);

    await revokeSessionFromAnotherDevice(browser, browserErrors, credentials);

    const note = 'migraine depuis ce matin';
    const notes = await ensureNotesFieldVisible(page, '#today-notes');
    const refused = page.waitForResponse(
      (response) => response.request().method() === 'PUT' && response.url().includes(`/api/v1/days/${todayISO}`)
    );
    await notes.fill(note);
    await page.locator('label.choice-option:has(input[name="mood"][value="4"]) .chip-round').click();
    expect((await refused).status(), 'the revoked session must be refused by the auth guard').toBe(401);

    const notice = page.locator(DASHBOARD_FAILURE_NOTICE);
    await expect(notice).toBeVisible();
    const link = await expectSignInLink(page, notice, 'fr');
    await expect(notes).toHaveValue(note);
    await expect(page.locator('input[name="mood"][value="4"]')).toBeChecked();
    await expectNothingStored(page, note);

    const csrfBefore = await csrfCookieValue(page);
    await signInThroughNoticeLink(page, link, credentials);
    // The retry sends the token this page already holds: signing in must not
    // have rotated it, or the retry would be refused as a forgery.
    expect(await csrfCookieValue(page)).toBe(csrfBefore);
    await expect(notes).toHaveValue(note);

    const [retryResponse] = await Promise.all([
      page.waitForResponse(
        (response) => response.request().method() === 'PUT' && response.url().includes(`/api/v1/days/${todayISO}`)
      ),
      notice.locator('[data-day-save-retry]').click(),
    ]);
    expect(retryResponse.status(), 'the retry after signing in must land').toBe(200);
    await expect(page.locator(DASHBOARD_FAILURE_NOTICE)).toHaveCount(0);

    await page.reload();
    await expect(page.locator('#today-notes')).toHaveValue(note);
  });

  test('calendar (de): the notice offers sign-in in a new tab and the retry then saves the entry', async ({
    browser,
    browserErrors,
    page,
  }) => {
    // Two devices, two sign-ins and a recovery-code rotation in one flow.
    test.slow();
    const credentials = createCredentials('day-save-session-de');
    await registerOwnerViaUI(page, credentials);
    await expectInlineRegisterRecoveryStep(page);
    await readRecoveryCode(page);
    await continueFromRecoveryCode(page);
    await completeOnboardingIfPresent(page);
    await setRequestTimezoneFromBrowser(page);
    await page.goto('/settings');
    await saveSettingsLanguage(page, 'de');
    await page.goto('/calendar');
    const todayButton = page.locator('button[data-day]:has(.calendar-today-pill)').first();
    const targetISO = shiftISODate(String(await todayButton.getAttribute('data-day')), -20);
    browserErrors.allow(
      htmxRefusalLogFor(`/api/v1/days/${targetISO}`),
      `this test revokes the session before PUT /api/v1/days/${targetISO}, and htmx logging that 401 is the behaviour under test`
    );

    const form = await openCalendarDayEditor(page, targetISO);
    await revokeSessionFromAnotherDevice(browser, browserErrors, credentials);

    const note = 'Kopfschmerzen seit dem Morgen';
    await fillDayEntry(form, note);
    const refused = page.waitForResponse(
      (response) => response.request().method() === 'PUT' && response.url().includes(`/api/v1/days/${targetISO}`)
    );
    await attemptDayEditorSave(page, targetISO, form);
    expect((await refused).status(), 'the revoked session must be refused by the auth guard').toBe(401);

    const notice = page.locator(FAILURE_NOTICE);
    await expect(notice).toBeVisible();
    const link = await expectSignInLink(page, notice, 'de');
    await expectDayEntryStillTyped(form, note);
    await expectNothingStored(page, note);

    const csrfBefore = await csrfCookieValue(page);
    await signInThroughNoticeLink(page, link, credentials);
    expect(await csrfCookieValue(page)).toBe(csrfBefore);
    await expectDayEntryStillTyped(form, note);

    const [retryResponse] = await Promise.all([
      page.waitForResponse(
        (response) => response.request().method() === 'PUT' && response.url().includes(`/api/v1/days/${targetISO}`)
      ),
      page.locator(RETRY_BUTTON).click(),
    ]);
    expect(retryResponse.ok(), `the retry after signing in failed with ${retryResponse.status()}`).toBeTruthy();

    await page.waitForLoadState('networkidle');
    const reopened = await openCalendarDayEditor(page, targetISO);
    await expect(reopened.locator('#calendar-notes')).toHaveValue(note);
  });
});
