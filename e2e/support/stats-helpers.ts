import { expect, type Locator, type Page, type Request } from '@playwright/test';
import {
  apiOriginHeader,
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  readRecoveryCode,
  registerOwnerViaUI,
} from './auth-helpers';
import { isoToday, shiftISODate } from './iso-date-helpers';
import { selectOnboardingStartDate } from './onboarding-helpers';
import { setRequestTimezoneFromBrowser } from './timezone-helpers';

// The date arithmetic itself lives in the dependency-free `iso-date-helpers`, so
// that `auth-helpers` can use it without importing this module back. Re-exported
// here because the specs that seed cycle data already reach for it under this
// module's name.
export { isoToday, shiftISODate };

export async function registerOwnerAndEnableIrregularMode(
  page: Page,
  prefix: string
): Promise<void> {
  const credentials = createCredentials(prefix);

  await registerOwnerViaUI(page, credentials);
  await expectInlineRegisterRecoveryStep(page);
  await readRecoveryCode(page);
  await continueFromRecoveryCode(page);
  await completeOnboardingIfPresent(page);
  await setRequestTimezoneFromBrowser(page);

  await page.goto('/settings');
  await expect(page).toHaveURL(/\/settings$/);

  const cycleForm = page.locator('#settings-cycle form[action="/api/v1/users/current/cycle"]');
  await expect(cycleForm).toBeVisible();
  await cycleForm.locator('input[name="irregular_cycle"]').check();
  await cycleForm.locator('button[data-save-button]').click();
  await expect(page.locator('#settings-cycle-status .status-ok')).toBeVisible();
}

/**
 * Registers an owner and onboards with an explicit last-period-start date,
 * `startDaysAgo` days back, returning that date.
 *
 * `completeOnboardingIfPresent` hardcodes today-3, which makes today an
 * auto-period-fill day and leaves the account inside the onboarding period
 * cluster; every scenario that needs today to sit outside it, or that needs a
 * baseline old enough to carry completed cycles, seeds through here instead.
 *
 * Step 1 holds exactly one mechanism for the value — the month picker — and its
 * MinDate is today - 60 days on every calendar date, so an anchor stays inside
 * that window and older cycle starts are added afterwards through
 * `markCycleStartViaAPI`, which has no past-date bound.
 *
 * Step 2 arms automatic period fill, which ships OFF for a new account: the
 * period days around the anchor are what turn it into a completed first cycle,
 * and every caller here seeds from that baseline. A spec whose subject IS the
 * default drives onboarding itself rather than calling this.
 *
 * Onboarding done, "today" is pinned to the browser's timezone, so a day count a
 * caller computes from the returned anchor matches the one the app renders
 * whatever zone the server runs in.
 */
export async function registerAndOnboardWithStartDaysAgo(
  page: Page,
  prefix: string,
  startDaysAgo: number
): Promise<string> {
  const credentials = createCredentials(prefix);
  await registerOwnerViaUI(page, credentials);
  await expectInlineRegisterRecoveryStep(page);
  await readRecoveryCode(page);
  await continueFromRecoveryCode(page);

  const startISO = shiftISODate(isoToday(), -startDaysAgo);
  await selectOnboardingStartDate(page, startISO);
  await page.locator('form[hx-post="/api/v1/onboarding/steps/1"] button[type="submit"]').click();

  const stepTwoForm = page.locator('form[hx-post="/api/v1/onboarding/steps/2"]');
  await expect(stepTwoForm).toBeVisible();

  // Armed explicitly, never inherited — see the note above. `check()` is
  // idempotent; the assertion keeps this failing loudly if the control moves
  // instead of quietly onboarding an account with no period days.
  const autoPeriodFill = stepTwoForm.locator('input[name="auto_period_fill"]');
  await autoPeriodFill.check();
  await expect(autoPeriodFill).toBeChecked();

  await Promise.all([
    page.waitForURL(/\/dashboard(?:\?.*)?$/, { timeout: 15000 }),
    stepTwoForm.locator('[data-onboarding-step2-submit]').click(),
  ]);

  await setRequestTimezoneFromBrowser(page);
  return startISO;
}

export async function todayISOFromDashboard(page: Page): Promise<string> {
  await page.goto('/dashboard');
  await expect(page).toHaveURL(/\/dashboard$/);
  const action = await page.locator('[data-dashboard-save-form]').first().getAttribute('hx-put');
  expect(action).toMatch(/^\/api\/v1\/days\/\d{4}-\d{2}-\d{2}$/);
  return String(action).replace('/api/v1/days/', '');
}

export async function markCycleStart(page: Page, isoDate: string): Promise<void> {
  const month = isoDate.slice(0, 7);
  await page.goto(`/calendar?month=${month}&day=${isoDate}`);
  await expect(page).toHaveURL(new RegExp(`/calendar\\?month=${month}&day=${isoDate}`));

  const manualStartButton = page.locator(
    `[data-day-cycle-start-form][data-day-cycle-start-date="${isoDate}"] [data-day-cycle-start-button]`
  );
  await expect(manualStartButton).toBeVisible();
  // Bind to the click's own request, not any matching response: waitForResponse
  // can resolve on a still-in-flight earlier request under load (see
  // saveDayEditorForm below).
  const [request] = await Promise.all([
    page.waitForRequest(
      (candidate) =>
        candidate.method() === 'POST' &&
        candidate.url().includes(`/api/v1/days/${isoDate}/cycle-start?source=calendar`),
    ),
    page.waitForNavigation({
      url: new RegExp(`/calendar\\?month=${month}&day=${isoDate}`),
      waitUntil: 'load',
    }),
    manualStartButton.click(),
  ]);
  const response = await request.response();
  expect(
    response,
    `expected a response for POST /api/v1/days/${isoDate}/cycle-start?source=calendar`
  ).not.toBeNull();
  expect(
    response!.ok(),
    `POST /api/v1/days/${isoDate}/cycle-start?source=calendar failed with ${response!.status()}`
  ).toBeTruthy();
  // HX-Refresh reloads the current page, which lazy-loads the day editor via
  // hx-trigger="load" (calendar.html). Callers immediately chain into another
  // markCycleStart/openCalendarDayEditor navigation, so let that fetch settle
  // before returning or it competes with the next page.goto.
  await page.waitForLoadState('networkidle');
}

/**
 * Backdates one cycle start through the API, replacing whatever start the date
 * already carries.
 *
 * The counterpart to `markCycleStart` above: that one drives the calendar's own
 * manual-start button and is the right tool when the button is the subject, this
 * one is pure seeding and has no past-date bound, so it reaches the anchors
 * onboarding's picker cannot.
 *
 * The endpoint sets `IsPeriod=true` AND `CycleStart=true` on the day, then runs
 * auto-period-fill. The explicit flag is the point: it is what
 * `latestExplicitCycleStartBeforeOrOn` picks up, where a plain `is_period` day
 * upsert would leave `stats.LastPeriodStart` anchored to the `user.LastPeriodStart`
 * onboarding wrote.
 *
 * `page.request` sends no Origin of its own and the CSRF middleware validates it
 * on every mutating request under the HTTPS posture, so the header is explicit
 * here (`apiOriginHeader`).
 */
export async function markCycleStartViaAPI(page: Page, isoDate: string): Promise<void> {
  const csrf = (await page.locator('meta[name="csrf-token"]').getAttribute('content')) ?? '';
  const response = await page.request.post(`/api/v1/days/${isoDate}/cycle-start`, {
    headers: {
      ...apiOriginHeader(page),
      'X-CSRF-Token': csrf,
      'Content-Type': 'application/x-www-form-urlencoded',
    },
    form: { replace_existing: 'true' },
  });
  expect(response.status(), `mark cycle start at ${isoDate}`).toBeLessThan(400);
}

export async function openCalendarDayEditor(page: Page, isoDate: string): Promise<Locator> {
  const month = isoDate.slice(0, 7);
  await page.goto(`/calendar?month=${month}&day=${isoDate}`, { waitUntil: 'domcontentloaded' });
  await expect(page).toHaveURL(new RegExp(`/calendar\\?month=${month}&day=${isoDate}`));

  const editButton = page.locator(`[data-day-editor-open="${isoDate}"]`).first();
  await expect(editButton).toBeVisible();
  // The "Add entry"/"Edit entry" disclosure fires hx-get /calendar/day/{date}?mode=edit
  // into #day-editor. Bind the click to that request's own response (waitForRequest ->
  // request.response(), as saveDayEditorForm/markCycleStart do) rather than
  // waitForResponse on the URL, which could resolve on the still-in-flight
  // hx-trigger="load" fetch page.goto kicked off. Awaiting the response before the
  // visibility check leaves the default 5s window covering only the client-side htmx
  // swap, not the network round-trip — the part that overran under full-suite serial
  // CPU contention and flaked calendar-autofill-clear.spec.ts.
  const [request] = await Promise.all([
    page.waitForRequest(
      (candidate) =>
        candidate.method() === 'GET' &&
        candidate.url().includes(`/calendar/day/${isoDate}`) &&
        candidate.url().includes('mode=edit'),
    ),
    editButton.evaluate((node) => {
      if (node instanceof HTMLButtonElement) {
        node.click();
      }
    }),
  ]);
  const response = await request.response();
  expect(response, `expected a response for GET /calendar/day/${isoDate}?mode=edit`).not.toBeNull();
  expect(
    response!.ok(),
    `GET /calendar/day/${isoDate}?mode=edit failed with ${response!.status()}`,
  ).toBeTruthy();

  const form = page.locator(`[data-day-editor-form][data-day-editor-date="${isoDate}"]`);
  await expect(form).toBeVisible();
  return form;
}

export async function saveDayEditorForm(page: Page, isoDate: string, form: Locator): Promise<void> {
  // Bind the wait to the request this click issues, not to any PUT response for
  // the date. waitForResponse would resolve on the first matching response to
  // arrive after registration — under CPU contention a still-in-flight earlier
  // PUT's response can land inside this window and satisfy the predicate before
  // the actual save lands. The `request` event only fires for requests issued
  // after registration, so this captures exactly the click's PUT; awaiting that
  // request's own response then blocks until this save has truly committed.
  const [request] = await Promise.all([
    page.waitForRequest(
      (candidate) =>
        candidate.method() === 'PUT' && candidate.url().includes(`/api/v1/days/${isoDate}`),
    ),
    form.locator('button[data-save-button]').click(),
  ]);
  const response = await request.response();
  expect(response, `expected a response for PUT /api/v1/days/${isoDate}`).not.toBeNull();
  expect(response!.ok(), `PUT /api/v1/days/${isoDate} failed with ${response!.status()}`).toBeTruthy();

  // The PUT response only means the write committed. Its htmx afterSwap then
  // fires `calendar-day-updated`, which reloads the whole calendar grid
  // (GET /calendar) and re-lazy-loads the day editor — a cascade that outlives
  // this click. If the caller navigates (openCalendarDayEditor → page.goto)
  // while that cascade is still hitting the server, the next page's own
  // hx-trigger="load" editor fetch competes with it and, under CPU contention,
  // can miss the 5s visibility window. A bare click followed by page.goto is
  // worse still: the navigation aborts a PUT that has not committed yet and the
  // save is silently lost. Let the app go quiescent first so the save is fully
  // settled — not just committed — before returning.
  await page.waitForLoadState('networkidle');
}

/**
 * Clicks the day-editor save button and returns the PUT it issued, WITHOUT
 * asserting that the save succeeded.
 *
 * `saveDayEditorForm` above is the helper every ordinary save goes through: it
 * proves the write committed and lets the htmx cascade settle. A save-failure
 * spec cannot use it, because the failure is the subject. This variant keeps
 * the one part that must never be hand-rolled — binding the wait to the click's
 * own request rather than to any matching response — and leaves the verdict to
 * the caller. It deliberately does not settle the network: an intercepted save
 * fires no `calendar-day-updated` cascade, and `networkidle` would only add a
 * timeout. Never follow it with `page.goto`: a navigation would abort the
 * in-flight PUT, which is the data loss the save helpers exist to prevent.
 */
export async function attemptDayEditorSave(
  page: Page,
  isoDate: string,
  form: Locator
): Promise<Request> {
  const [request] = await Promise.all([
    page.waitForRequest(
      (candidate) =>
        candidate.method() === 'PUT' && candidate.url().includes(`/api/v1/days/${isoDate}`),
    ),
    form.locator('button[data-save-button]').click(),
  ]);
  return request;
}

export async function saveCycleFactorOnDay(
  page: Page,
  isoDate: string,
  factorKey: string
): Promise<void> {
  const form = await openCalendarDayEditor(page, isoDate);
  const factorChip = form.locator(
    `label.choice-option:has(input[name="cycle_factor_keys"][value="${factorKey}"]) .chip-lead`
  );
  await factorChip.click();
  const [request] = await Promise.all([
    page.waitForRequest(
      (candidate) =>
        candidate.method() === 'PUT' && candidate.url().includes(`/api/v1/days/${isoDate}`),
    ),
    form.evaluate((node) => {
      if (node instanceof HTMLFormElement) {
        node.requestSubmit();
      }
    }),
  ]);
  const response = await request.response();
  expect(response, `expected a response for PUT /api/v1/days/${isoDate}`).not.toBeNull();
  expect(response!.ok(), `PUT /api/v1/days/${isoDate} failed with ${response!.status()}`).toBeTruthy();
  // Let the calendar-day-updated grid refresh + editor re-lazy-load cascade
  // settle before the reopen below re-navigates (see saveDayEditorForm).
  await page.waitForLoadState('networkidle');
  const savedForm = await openCalendarDayEditor(page, isoDate);
  await expect(savedForm.locator(`input[name="cycle_factor_keys"][value="${factorKey}"]`)).toBeChecked();
}

export async function saveBBTOnDay(page: Page, isoDate: string, value: string): Promise<void> {
  await page.goto('/settings');
  await expect(page).toHaveURL(/\/settings$/);

  const trackingSection = page.locator('#settings-tracking');
  await expect(trackingSection).toBeVisible();
  const trackingForm = trackingSection.locator('form[data-settings-draft-form="tracking"]');
  await expect(trackingForm).toBeVisible();

  const trackBBT = trackingSection.locator('input[name="track_bbt"]');
  if (!(await trackBBT.isChecked())) {
    await trackBBT.evaluate((node) => {
      if (node instanceof HTMLInputElement) {
        node.click();
      }
    });
    await expect(trackBBT).toBeChecked();
    await trackingForm.evaluate((node) => {
      if (node instanceof HTMLFormElement) {
        node.requestSubmit();
      }
    });
    await expect(page.locator('#settings-tracking-status .status-ok')).toBeVisible();
  }

  const form = await openCalendarDayEditor(page, isoDate);
  const bbtInput = form.locator('#calendar-bbt');
  await expect(bbtInput).toBeVisible();
  await bbtInput.fill(value);
  const [request] = await Promise.all([
    page.waitForRequest(
      (candidate) =>
        candidate.method() === 'PUT' && candidate.url().includes(`/api/v1/days/${isoDate}`),
    ),
    form.evaluate((node) => {
      if (node instanceof HTMLFormElement) {
        node.requestSubmit();
      }
    }),
  ]);
  const response = await request.response();
  expect(response, `expected a response for PUT /api/v1/days/${isoDate}`).not.toBeNull();
  expect(response!.ok(), `PUT /api/v1/days/${isoDate} failed with ${response!.status()}`).toBeTruthy();
  // Let the calendar-day-updated grid refresh + editor re-lazy-load cascade
  // settle before the reopen below re-navigates (see saveDayEditorForm).
  await page.waitForLoadState('networkidle');

  const savedForm = await openCalendarDayEditor(page, isoDate);
  await expect(savedForm.locator('#calendar-bbt')).not.toHaveValue('');
}
