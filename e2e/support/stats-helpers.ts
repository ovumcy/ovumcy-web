import { expect, type Locator, type Page } from '@playwright/test';
import {
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  readRecoveryCode,
  registerOwnerViaUI,
} from './auth-helpers';
import { setRequestTimezoneFromBrowser } from './timezone-helpers';

export function shiftISODate(iso: string, days: number): string {
  const [year, month, day] = iso.split('-').map((part) => Number(part));
  const shifted = new Date(year, month - 1, day);
  shifted.setDate(shifted.getDate() + days);
  const yyyy = shifted.getFullYear();
  const mm = String(shifted.getMonth() + 1).padStart(2, '0');
  const dd = String(shifted.getDate()).padStart(2, '0');
  return `${yyyy}-${mm}-${dd}`;
}

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

  const cycleForm = page.locator('section#settings-cycle form[action="/api/v1/users/current/cycle"]');
  await expect(cycleForm).toBeVisible();
  await cycleForm.locator('input[name="irregular_cycle"]').check();
  await cycleForm.locator('button[data-save-button]').click();
  await expect(page.locator('#settings-cycle-status .status-ok')).toBeVisible();
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
  // saveDayEditorForm in calendar-autofill-clear.spec.ts).
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

export async function openCalendarDayEditor(page: Page, isoDate: string): Promise<Locator> {
  const month = isoDate.slice(0, 7);
  await page.goto(`/calendar?month=${month}&day=${isoDate}`, { waitUntil: 'domcontentloaded' });
  await expect(page).toHaveURL(new RegExp(`/calendar\\?month=${month}&day=${isoDate}`));

  const editButton = page.locator(`[data-day-editor-open="${isoDate}"]`).first();
  await expect(editButton).toBeVisible();
  // Bind the disclosure click to its own htmx GET /calendar/day/{date}?mode=edit
  // (waitForRequest -> request.response()) so the network round-trip is awaited
  // explicitly and the form's default 5s visibility check only covers the client
  // htmx swap. See calendar-autofill-clear.spec.ts for the full rationale.
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

export async function saveCycleFactorOnDay(
  page: Page,
  isoDate: string,
  factorKey: string
): Promise<void> {
  const form = await openCalendarDayEditor(page, isoDate);
  const factorChip = form.locator(
    `label.choice-option:has(input[name="cycle_factor_keys"][value="${factorKey}"]) .check-chip`
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
