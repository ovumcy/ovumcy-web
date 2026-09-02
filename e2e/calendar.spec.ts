import { expect, test, type Locator, type Page } from './support/fixtures';
import {
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  readRecoveryCode,
  registerOwnerViaUI,
  apiOriginHeader,
} from './support/auth-helpers';
import { applyTheme } from './support/contrast-helpers';
import { saveSettingsLanguage } from './support/language-helpers';
import { expectElementAboveMobileTabbar } from './support/mobile-layout-helpers';
import { ensureNotesFieldVisible } from './support/note-helpers';
import { markCycleStart, openCalendarDayEditor, saveDayEditorForm } from './support/stats-helpers';
import { setRequestTimezoneFromBrowser } from './support/timezone-helpers';
import { checkStyledControl } from './support/form-helpers';
import { selectOnboardingStartDate } from './support/onboarding-helpers';
import { localeText } from './support/locale-helpers';
import {
  acceptConfirmDialog,
  cancelConfirmDialog,
  expectConfirmDialogCaptions,
  mutatingRequestsDuring,
} from './support/confirm-dialog-helpers';

function shiftISODate(iso: string, days: number): string {
  const [y, m, d] = iso.split('-').map((part) => Number(part));
  const date = new Date(y, m - 1, d);
  date.setDate(date.getDate() + days);

  const yyyy = date.getFullYear();
  const mm = String(date.getMonth() + 1).padStart(2, '0');
  const dd = String(date.getDate()).padStart(2, '0');
  return `${yyyy}-${mm}-${dd}`;
}

async function registerOwnerOnCalendar(page: Page, prefix: string): Promise<void> {
  const creds = createCredentials(prefix);

  await registerOwnerViaUI(page, creds);
  await expectInlineRegisterRecoveryStep(page);

  await readRecoveryCode(page);
  await continueFromRecoveryCode(page);
  await completeOnboardingIfPresent(page);

  await setRequestTimezoneFromBrowser(page);
  await page.goto('/calendar');
  await expect(page).toHaveURL(/\/calendar(?:\?.*)?$/);
}

async function openCalendarNotes(form: Locator): Promise<void> {
  await ensureNotesFieldVisible(form, '#calendar-notes');
}

async function openSexActivityDisclosure(form: Locator): Promise<void> {
  const disclosure = form.locator('details[data-sex-activity-details]');
  const isOpen = await disclosure.evaluate((element) => element.hasAttribute('open'));
  if (!isOpen) {
    await disclosure.locator('summary').click();
  }
  await expect(form.locator('[data-sex-activity-option="protected"]')).toBeVisible();
}

/** The computed background-color the browser actually paints for a cell. */
async function paintedBackgroundColor(cell: Locator): Promise<string> {
  return cell.evaluate((node: Element) => window.getComputedStyle(node).backgroundColor);
}

async function todayISOFromCalendar(page: Page): Promise<string> {
  const todayButton = page.locator('button[data-day]:has(.calendar-today-pill)').first();
  await expect(todayButton).toBeVisible();
  const todayISO = await todayButton.getAttribute('data-day');
  expect(todayISO).toMatch(/^\d{4}-\d{2}-\d{2}$/);
  return todayISO!;
}

test.describe('Calendar page', () => {
  test('default month renders and navigation prev/next/today works', async ({ page }) => {
    await registerOwnerOnCalendar(page, 'calendar-nav');

    // Addressed by the navigation hook, not by the button utility: the Today
    // control is compact secondary navigation, so the calendar screen's only
    // primary fill is the day panel's edit action.
    const navigationCard = page.locator('div.card').filter({
      has: page.locator('[data-calendar-month-nav]'),
    }).first();
    const monthLabel = navigationCard.locator('p.journal-muted').first();
    const prevLink = navigationCard.locator('a.btn-secondary[href^="/calendar?month="]').first();
    const nextLink = navigationCard.locator('a.btn-secondary[href^="/calendar?month="]').nth(1);
    const todayLink = navigationCard.locator('a[data-calendar-today]');

    const initialLabel = ((await monthLabel.textContent()) ?? '').trim();
    expect(initialLabel.length).toBeGreaterThan(0);

    // Each wait names the month the control points at. The generic
    // /calendar?month=\d{4}-\d{2} shape is satisfied by the URL the click starts
    // from once a month is selected, so it would pass without the transition.
    const prevMonth = new URL((await prevLink.getAttribute('href')) ?? '', page.url()).searchParams.get('month');
    expect(prevMonth).toMatch(/^\d{4}-\d{2}$/);
    await prevLink.click();
    await expect(page).toHaveURL(new RegExp(`/calendar\\?month=${prevMonth}$`));
    const prevLabel = ((await monthLabel.textContent()) ?? '').trim();
    expect(prevLabel).not.toBe(initialLabel);

    const nextMonth = new URL((await nextLink.getAttribute('href')) ?? '', page.url()).searchParams.get('month');
    expect(nextMonth).toMatch(/^\d{4}-\d{2}$/);
    expect(nextMonth).not.toBe(prevMonth);
    await nextLink.click();
    await expect(page).toHaveURL(new RegExp(`/calendar\\?month=${nextMonth}$`));

    await todayLink.click();
    await expect(page).toHaveURL(/\/calendar$/);
    await expect(page.locator('button[data-day]:has(.calendar-today-pill)')).toHaveCount(1);
  });

  test('invalid month query redirects to the current calendar page', async ({ page }) => {
    await registerOwnerOnCalendar(page, 'calendar-invalid-month');

    await page.goto('/calendar?month=9999-99');
    await expect(page).toHaveURL(/\/calendar$/);
    // Address the page title by the key it declares. The regex this replaces
    // listed three of six languages and, being an alternation, matched any of
    // them regardless of which language the page was actually rendering.
    const title = page.locator('h1[data-title-key="calendar.title"]');
    await expect(title).toBeVisible();
    await expect(title).toHaveText(localeText('en', 'calendar.title'));
  });

  test('legend groups the grid encoding by concept and leads the grid', async ({ page }) => {
    await registerOwnerOnCalendar(page, 'calendar-legend');

    const legend = page.locator('[data-calendar-legend]');
    await expect(legend).toBeVisible();
    await expect(legend.locator('.legend-swatch-period')).toHaveCount(1);
    await expect(legend.locator('.legend-swatch-predicted')).toHaveCount(1);
    await expect(legend.locator('.legend-swatch-start-window')).toHaveCount(1);
    await expect(legend.locator('.legend-swatch-fertile')).toHaveCount(1);
    await expect(legend.locator('.legend-swatch-today')).toHaveCount(1);
    // Seven concepts, not the nine CSS states the grid happens to have.
    await expect(legend.locator('.legend-item')).toHaveCount(7);

    // The legend is above the first day cell, so it is on screen while the
    // month is being read.
    const legendBox = await legend.boundingBox();
    const firstCellBox = await page.locator('button[data-day]').first().boundingBox();
    expect(legendBox).not.toBeNull();
    expect(firstCellBox).not.toBeNull();
    expect(legendBox!.y).toBeLessThan(firstCellBox!.y);

    const ovulationDot = legend.locator('.calendar-ovulation-dot');
    const tentativeOvulation = legend.locator('.calendar-ovulation-dash');
    await expect(ovulationDot).toHaveCount(1);
    await expect(tentativeOvulation).toHaveCount(1);

    const styles = await ovulationDot.evaluate((node) => {
      const computed = window.getComputedStyle(node);
      return {
        width: parseFloat(computed.width || '0'),
        boxShadow: computed.boxShadow || '',
      };
    });
    expect(styles.width).toBeGreaterThanOrEqual(12);
    expect(styles.boxShadow).not.toBe('none');
  });

  test('mobile calendar keeps the legend scrollable above the bottom tabbar', async ({ page }) => {
    await registerOwnerOnCalendar(page, 'calendar-mobile-safe-area');
    await page.setViewportSize({ width: 390, height: 844 });
    await page.reload();
    await expect(page).toHaveURL(/\/calendar(?:\?.*)?$/);

    const legend = page.locator('.calendar-legend');
    await legend.scrollIntoViewIfNeeded();
    await expectElementAboveMobileTabbar(page, legend);
  });

  test('past day entry can be edited from calendar and persists after reload', async ({ page }) => {
    await registerOwnerOnCalendar(page, 'calendar-past-edit');

    const todayISO = await todayISOFromCalendar(page);
    const pastISO = shiftISODate(todayISO, -2);
    const pastMonth = pastISO.slice(0, 7);

    const dayEditorForm = await openCalendarDayEditor(page, pastISO);

    await dayEditorForm.locator('input[name="is_period"]').check();
    await checkStyledControl(dayEditorForm.locator('input[name="flow"][value="medium"]'));

    const noteText = `calendar-note-${Date.now()}`;
    await openCalendarNotes(dayEditorForm);
    await dayEditorForm.locator('#calendar-notes').fill(noteText);
    await saveDayEditorForm(page, pastISO, dayEditorForm);

    await page.goto(`/calendar?month=${pastMonth}&day=${pastISO}`);
    await expect(page).toHaveURL(new RegExp(`/calendar\\?month=${pastMonth}&day=${pastISO}`));
    await expect(page.locator('#day-editor')).toContainText(noteText);

    const editButton = page.locator(`[data-day-editor-open="${pastISO}"]`).first();
    await expect(editButton).toBeVisible();
    await editButton.click();
    await expect(page.locator(`[data-day-editor-form][data-day-editor-date="${pastISO}"] #calendar-notes`)).toHaveValue(noteText);
    await expect(page.locator(`[data-day-editor-form][data-day-editor-date="${pastISO}"] input[name="is_period"]`)).toBeChecked();
  });

  test('logged day renders data and sex markers in the calendar grid', async ({ page }) => {
    await registerOwnerOnCalendar(page, 'calendar-markers');

    const todayISO = await todayISOFromCalendar(page);
    const pastISO = shiftISODate(todayISO, -1);
    const pastMonth = pastISO.slice(0, 7);

    const dayEditorForm = await openCalendarDayEditor(page, pastISO);
    await openSexActivityDisclosure(dayEditorForm);
    await dayEditorForm.locator('[data-sex-activity-option="protected"]').click();
    await openCalendarNotes(dayEditorForm);
    await dayEditorForm.locator('#calendar-notes').fill(`calendar-marker-${Date.now()}`);
    await saveDayEditorForm(page, pastISO, dayEditorForm);

    await page.goto(`/calendar?month=${pastMonth}&day=${pastISO}`);
    const dayButton = page.locator(`button[data-day="${pastISO}"]`);
    await expect(dayButton).toHaveAttribute('data-calendar-has-data', 'true');
    await expect(dayButton.locator('.calendar-data-marker')).toBeVisible();
    await expect(dayButton.locator('.calendar-sex-marker')).toBeVisible();
  });

  test('existing day entry can be deleted from calendar after confirmation', async ({ page }) => {
    await registerOwnerOnCalendar(page, 'calendar-delete-entry');

    const todayISO = await todayISOFromCalendar(page);
    const pastISO = shiftISODate(todayISO, -2);
    const pastMonth = pastISO.slice(0, 7);
    const noteText = `calendar-delete-${Date.now()}`;

    const dayEditorForm = await openCalendarDayEditor(page, pastISO);
    await dayEditorForm.locator('input[name="is_period"]').check();
    await openCalendarNotes(dayEditorForm);
    await dayEditorForm.locator('#calendar-notes').fill(noteText);
    await saveDayEditorForm(page, pastISO, dayEditorForm);

    await page.goto(`/calendar?month=${pastMonth}&day=${pastISO}`);
    await expect(page.locator('#day-editor')).toContainText(noteText);

    await page.locator(`[data-day-editor-open="${pastISO}"]`).first().click();
    const deleteButton = page.locator(`[data-day-delete-form][data-day-delete-date="${pastISO}"] [data-day-delete-button]`);
    await expect(deleteButton).toBeVisible();
    await deleteButton.click();

    await expect(page.locator('#confirm-modal')).toBeVisible();
    await page.locator('#confirm-modal-accept').click();

    await expect(page.locator(`[data-day-editor-form][data-day-editor-date="${pastISO}"]`)).toHaveCount(0);
    await expect(page.locator(`[data-day-editor-open="${pastISO}"]`).first()).toBeVisible();
    await expect(page.locator('#day-editor')).not.toContainText(noteText);
  });

  // Cancelling the confirmation must be inert. This is a real regression, not a
  // hypothetical: while the delete control carried `data-confirm`, htmx issued
  // the DELETE from its own listener on the form before the document-level
  // interceptor ever ran, so the dialog was decorative and Cancel erased the
  // day anyway — irrecoverable, since health data has no undo. The control now
  // uses `hx-confirm`, which htmx gates on. The template-level guard against the
  // whole class is TestNoTemplateElementMixesHTMXRequestWithDataConfirm; the
  // sibling cancel-is-inert tests cover the other gated surfaces in
  // settings-profile-cycle.spec.ts and settings-calendar-feed.spec.ts.
  test('cancelling the delete confirmation leaves the day entry intact', async ({ page }) => {
    await registerOwnerOnCalendar(page, 'calendar-delete-cancel');

    const todayISO = await todayISOFromCalendar(page);
    const pastISO = shiftISODate(todayISO, -2);
    const pastMonth = pastISO.slice(0, 7);
    const noteText = `calendar-delete-cancel-${Date.now()}`;

    const dayEditorForm = await openCalendarDayEditor(page, pastISO);
    await dayEditorForm.locator('input[name="is_period"]').check();
    await openCalendarNotes(dayEditorForm);
    await dayEditorForm.locator('#calendar-notes').fill(noteText);
    // Bind the wait to this click's own PUT and let the htmx cascade it triggers
    // settle, so the entry is committed — not merely submitted — before the
    // delete flow starts (same reasoning as saveDayEditorForm in
    // calendar-autofill-clear.spec.ts).
    const [saveRequest] = await Promise.all([
      page.waitForRequest(
        (candidate) =>
          candidate.method() === 'PUT' && candidate.url().includes(`/api/v1/days/${pastISO}`)
      ),
      dayEditorForm.locator('button[data-save-button]').click(),
    ]);
    const saveResponse = await saveRequest.response();
    expect(saveResponse, `expected a response for PUT /api/v1/days/${pastISO}`).not.toBeNull();
    expect(
      saveResponse!.ok(),
      `PUT /api/v1/days/${pastISO} failed with ${saveResponse!.status()}`
    ).toBeTruthy();
    await page.waitForLoadState('networkidle');

    const deleteForm = page.locator(`[data-day-delete-form][data-day-delete-date="${pastISO}"]`);
    const isDayMutation = (pathname: string) => pathname.endsWith(`/api/v1/days/${pastISO}`);

    const cancelledRequests = await mutatingRequestsDuring(page, isDayMutation, async () => {
      await openCalendarDayEditor(page, pastISO);
      const deleteButton = deleteForm.locator('[data-day-delete-button]');
      await expect(deleteButton).toBeVisible();
      await deleteButton.click();

      // The dialog must show the captions this surface declared, not a
      // hardcoded fallback: backend coverage only pins that they are declared.
      await expectConfirmDialogCaptions(page, deleteForm);
      await cancelConfirmDialog(page);

      // A reload is the concrete signal that any request the click was going to
      // issue has had its chance — no arbitrary timeout involved.
      await page.reload();
      await expect(page).toHaveURL(new RegExp(`/calendar\\?month=${pastMonth}&day=${pastISO}`));
    });
    expect(cancelledRequests, 'cancelling the delete must issue no request').toEqual([]);

    // The entry survived the dialog and the reload — asserted on the server-rendered
    // grid marker as well as the panel, so this cannot pass on stale client state.
    await expect(page.locator('#day-editor')).toContainText(noteText);
    await expect(page.locator(`button[data-day="${pastISO}"]`)).toHaveAttribute(
      'data-calendar-has-data',
      'true'
    );

    // Positive anchor: the same control, accepted, really does delete — so the
    // assertions above prove Cancel is inert, not that the control is dead.
    await openCalendarDayEditor(page, pastISO);
    await deleteForm.locator('[data-day-delete-button]').click();
    await acceptConfirmDialog(page);

    await expect(deleteForm).toHaveCount(0);
    await expect(page.locator('#day-editor')).not.toContainText(noteText);
  });

  test('future empty day opens editor directly and keeps future warning context', async ({ page }) => {
    await registerOwnerOnCalendar(page, 'calendar-future-day');

    const todayISO = await todayISOFromCalendar(page);
    const futureISO = shiftISODate(todayISO, 3);
    const futureMonth = futureISO.slice(0, 7);

    await page.goto(`/calendar?month=${futureMonth}`);
    await expect(page).toHaveURL(new RegExp(`/calendar\\?month=${futureMonth}`));

    await page.locator(`button[data-day="${futureISO}"]`).click();

    const warningPanel = page.locator('#day-editor .card-quiet.text-sm').first();
    await expect(warningPanel).toBeVisible();
    await expect(warningPanel).not.toHaveText(/^$/);
    await expect(page.locator(`[data-day-editor-form][data-day-editor-date="${futureISO}"]`)).toBeVisible();
    await expect(page.locator(`[data-day-editor-open="${futureISO}"]`)).toHaveCount(0);
  });

  test('saved language keeps selected month/day query localized after returning from settings', async ({ page }) => {
    await registerOwnerOnCalendar(page, 'calendar-lang-query');

    const todayISO = await todayISOFromCalendar(page);
    const pastISO = shiftISODate(todayISO, -2);
    const pastMonth = pastISO.slice(0, 7);

    await page.goto(`/calendar?month=${pastMonth}&day=${pastISO}`);
    await expect(page.locator(`[data-day-editor-open="${pastISO}"]`)).toBeVisible();

    await page.goto('/settings');
    await saveSettingsLanguage(page, 'ru');

    await page.goto(`/calendar?month=${pastMonth}&day=${pastISO}`);
    await expect(page.locator('html')).toHaveAttribute('lang', 'ru');

    const currentURL = new URL(page.url());
    expect(currentURL.pathname).toBe('/calendar');
    expect(currentURL.searchParams.get('month')).toBe(pastMonth);
    expect(currentURL.searchParams.get('day')).toBe(pastISO);
    await expect(page.locator(`[data-day-editor-open="${pastISO}"]`)).toBeVisible();
  });

  test('manual cycle start button in calendar creates a period entry for that day', async ({ page }) => {
    await registerOwnerOnCalendar(page, 'calendar-manual-cycle-start');

    const todayISO = await todayISOFromCalendar(page);
    const pastISO = shiftISODate(todayISO, -3);
    const pastMonth = pastISO.slice(0, 7);

    await page.goto(`/calendar?month=${pastMonth}&day=${pastISO}`);
    await expect(page).toHaveURL(new RegExp(`/calendar\\?month=${pastMonth}&day=${pastISO}`));

    const manualStartButton = page.locator(`[data-day-cycle-start-form][data-day-cycle-start-date="${pastISO}"] [data-day-cycle-start-button]`);
    await expect(manualStartButton).toBeVisible();
    const [req] = await Promise.all([
      page.waitForRequest((candidate) => {
        return (
          candidate.method() === 'POST' &&
          candidate.url().includes(`/api/v1/days/${pastISO}/cycle-start?source=calendar`)
        );
      }),
      manualStartButton.click(),
    ]);
    const cycleStartResponse = await req.response();
    expect(cycleStartResponse, `expected a response for POST /api/v1/days/${pastISO}/cycle-start`).not.toBeNull();
    expect(
      cycleStartResponse!.ok(),
      `POST /api/v1/days/${pastISO}/cycle-start failed with ${cycleStartResponse!.status()}`
    ).toBeTruthy();

    const editButton = page.locator(`[data-day-editor-open="${pastISO}"]`).first();
    await expect(editButton).toBeVisible();
    await editButton.click();

    const dayEditorForm = page.locator(`[data-day-editor-form][data-day-editor-date="${pastISO}"]`);
    await expect(dayEditorForm).toBeVisible();
    await expect(dayEditorForm.locator('input[name="is_period"]')).toBeChecked();
  });

  test('tomorrow keeps manual cycle start available with a warning', async ({ page }) => {
    await registerOwnerOnCalendar(page, 'calendar-future-cycle-start');

    const todayISO = await todayISOFromCalendar(page);
    const tomorrowISO = shiftISODate(todayISO, 1);
    const month = tomorrowISO.slice(0, 7);

    await page.goto(`/calendar?month=${month}&day=${tomorrowISO}`);
    await expect(page).toHaveURL(new RegExp(`/calendar\\?month=${month}&day=${tomorrowISO}`));

    const manualStartForm = page.locator(`[data-day-cycle-start-form][data-day-cycle-start-date="${tomorrowISO}"]`);
    const manualStartButton = manualStartForm.locator('[data-day-cycle-start-button]');
    await expect(manualStartButton).toBeVisible();
    // The notice has its own hook and declares the key it renders — the same
    // element dashboard-warnings.spec.ts pins, so the two specs can no longer
    // describe it two different ways.
    const futureNotice = page.locator('#day-editor [data-future-cycle-start-notice]');
    await expect(futureNotice.first()).toBeVisible();
    await expect(futureNotice.first()).toHaveAttribute(
      'data-notice-key',
      'warning.future_cycle_start'
    );

    // The onboarding seed anchors last_period_start at today-3, so tomorrow is a
    // 4-day short gap: the backend rejects the start with a 400 unless the owner
    // explicitly confirms it as uncertain (ShortGapDays > 0). The confirm modal
    // must open so accepting can flip mark_uncertain=true before the POST fires.
    await manualStartButton.click();
    await expect(page.locator('#confirm-modal')).toBeVisible();

    const [cycleStartRequest] = await Promise.all([
      page.waitForRequest((candidate) => {
        return (
          candidate.method() === 'POST' &&
          candidate.url().includes(`/api/v1/days/${tomorrowISO}/cycle-start?source=calendar`)
        );
      }),
      page.locator('#confirm-modal-accept').click(),
    ]);
    const cycleStartResponse = await cycleStartRequest.response();
    expect(
      cycleStartResponse,
      `expected a response for POST /api/v1/days/${tomorrowISO}/cycle-start?source=calendar`
    ).not.toBeNull();
    expect(cycleStartResponse!.status()).toBeLessThan(400);
    await page.waitForLoadState('networkidle');

    await page.locator(`[data-day-editor-open="${tomorrowISO}"]`).first().click();
    const dayEditorForm = page.locator(`[data-day-editor-form][data-day-editor-date="${tomorrowISO}"]`);
    await expect(dayEditorForm).toBeVisible();
    await expect(dayEditorForm.locator('input[name="is_period"]')).toBeChecked();
  });

  test('summary-view manual cycle start exposes its policy node so the confirm modal opens', async ({
    page,
  }) => {
    // Regression for the calendar summary-view placement bug: the confirm script
    // resolves the policy node via form.parentElement.querySelector(
    // '[data-cycle-start-policy]'), so the hidden policy node MUST be a sibling of
    // the cycle-start form inside the same wrapper. When it was placed outside the
    // wrapper the lookup returned null, the short-gap confirm modal never opened,
    // and the POST failed with a silent 400.
    await registerOwnerOnCalendar(page, 'calendar-cycle-start-policy-scope');

    const todayISO = await todayISOFromCalendar(page);
    const tomorrowISO = shiftISODate(todayISO, 1);
    const month = tomorrowISO.slice(0, 7);

    await page.goto(`/calendar?month=${month}&day=${tomorrowISO}`);
    await expect(page).toHaveURL(new RegExp(`/calendar\\?month=${month}&day=${tomorrowISO}`));

    const manualStartForm = page.locator(`[data-day-cycle-start-form][data-day-cycle-start-date="${tomorrowISO}"]`);
    await expect(manualStartForm).toBeVisible();

    // Structural guard: the policy node must be reachable from the form's parent,
    // exactly as the confirm script looks it up. This fails immediately if the
    // template ever moves the policy node back outside the form's wrapper.
    const policyReachableFromForm = await manualStartForm.evaluate((form) =>
      Boolean(form.parentElement && form.parentElement.querySelector('[data-cycle-start-policy]')),
    );
    expect(policyReachableFromForm).toBe(true);

    // End-to-end: clicking opens the confirm modal, and accepting completes the
    // short-gap cycle start (HTTP < 400) instead of the silent 400.
    await manualStartForm.locator('[data-day-cycle-start-button]').click();
    await expect(page.locator('#confirm-modal')).toBeVisible();

    const [cycleStartRequest] = await Promise.all([
      page.waitForRequest((candidate) => {
        return (
          candidate.method() === 'POST' &&
          candidate.url().includes(`/api/v1/days/${tomorrowISO}/cycle-start?source=calendar`)
        );
      }),
      page.locator('#confirm-modal-accept').click(),
    ]);
    const cycleStartResponse = await cycleStartRequest.response();
    expect(
      cycleStartResponse,
      `expected a response for POST /api/v1/days/${tomorrowISO}/cycle-start?source=calendar`
    ).not.toBeNull();
    expect(cycleStartResponse!.status()).toBeLessThan(400);
  });

  test('BBT tracking without a confirmed signal demotes the predicted ovulation day to a tentative dash', async ({
    page,
  }) => {
    // appendCurrentCycleBBTSignal demotes the predicted OvulationDate from
    // .calendar-ovulation-dot to .calendar-ovulation-dash when:
    //   - user.TrackBBT is true
    //   - stats has a non-zero OvulationDate / NextPeriodStart
    //   - inferBBTOvulationDate finds no confirmed BBT signal in the cycle
    // Onboard 13 days back so the predicted ovulation (cycleStart + 13d)
    // lands on today. The calendar renders the current month, and its grid
    // always contains today by construction, so the demoted dash is visible
    // on every run date. (A past ovulation date can fall before the grid's
    // leading edge during the first week of a month, and a future one can
    // fall past the trailing edge at a month's end — today is the only
    // anchor that is in-grid regardless of when the test runs.) Enable
    // TrackBBT through the tracking endpoint without logging any BBT, then
    // assert the demoted day carries a tentative dash and no confirmed dot.
    const creds = createCredentials('calendar-anovulatory-dash');
    await registerOwnerViaUI(page, creds);
    await expectInlineRegisterRecoveryStep(page);
    await readRecoveryCode(page);
    await continueFromRecoveryCode(page);

    // Custom onboarding flow: anchor last_period_start at today-13 so the
    // predicted ovulation (cycle day 14) lands on today, keeping the demoted
    // dash inside the current month's grid on any run date.
    const startISO = shiftISODate(
      await page.evaluate(() => {
        const now = new Date();
        const yyyy = now.getFullYear();
        const mm = String(now.getMonth() + 1).padStart(2, '0');
        const dd = String(now.getDate()).padStart(2, '0');
        return `${yyyy}-${mm}-${dd}`;
      }),
      -13,
    );
    await selectOnboardingStartDate(page, startISO);
    await page.locator('form[hx-post="/api/v1/onboarding/steps/1"] button[type="submit"]').click();
    const stepTwoForm = page.locator('form[hx-post="/api/v1/onboarding/steps/2"]');
    await expect(stepTwoForm).toBeVisible();
    await Promise.all([
      page.waitForURL(/\/dashboard(?:\?.*)?$/, { timeout: 15000 }),
      stepTwoForm.locator('[data-onboarding-step2-submit]').click(),
    ]);
    await setRequestTimezoneFromBrowser(page);

    // The demotion acts on the PREDICTED ovulation day, and that projection is
    // withheld until one cycle has been observed. Log this cycle's start and the
    // previous one exactly 28 days before it — the length the account settings
    // already carry — so the observed cycle changes nothing about where the
    // predicted ovulation lands, and the latest logged start is still today-13.
    await markCycleStart(page, shiftISODate(startISO, -28));
    await markCycleStart(page, startISO);

    // Enable TrackBBT via the tracking settings endpoint. Send the full
    // default snapshot — the JSON body parser does not treat missing fields
    // as no-op, so a single-field patch would wipe the other tracking flags.
    // The JSON body keeps the published v1 keys, which spell the three section
    // flags in their stored, inverted form; only the settings form posts the
    // positive show_* fields the toggles are labelled with.
    const csrf = (await page.locator('meta[name="csrf-token"]').getAttribute('content')) ?? '';
    const trackingResponse = await page.request.patch('/api/v1/users/current/tracking', {
      headers: { ...apiOriginHeader(page), 'X-CSRF-Token': csrf, 'Content-Type': 'application/json' },
      data: {
        track_bbt: true,
        temperature_unit: 'celsius',
        track_cervical_mucus: false,
        hide_sex_chip: false,
        hide_cycle_factors: false,
        hide_notes_field: false,
        show_historical_phases: false,
      },
    });
    expect(trackingResponse.status()).toBeLessThan(400);

    await page.goto('/calendar');
    await expect(page).toHaveURL(/\/calendar(?:\?.*)?$/);

    // The current cycle's predicted ovulation is demoted from a confirmed dot
    // to a tentative dash. We deliberately do NOT assert zero ovulation dots
    // across the whole grid: future predicted cycles legitimately paint a
    // confirmed dot, and whether one lands inside the visible month depends on
    // today's date (the next cycle's ovulation can fall in this month or the
    // next). Assert the demotion precisely instead — the demoted day cell
    // carries a dash and not a dot. tentativeOvulationMap is only ever populated
    // by this BBT demotion, so the dashed cell is exactly that day. The legend
    // keeps its own dot+dash icons inside .legend-item, excluded by data-day.
    const demotedDay = page.locator('button[data-day]:has(.calendar-ovulation-dash)');
    await expect(demotedDay.first()).toBeVisible();
    await expect(demotedDay.first().locator('.calendar-ovulation-dot')).toHaveCount(0);
  });

  test('usage_goal setting flips the calendar root data-usage-goal attribute', async ({ page }) => {
    // The tailwind palette keys fertile-edge / fertile-peak cell colors off
    // [data-calendar-view][data-usage-goal="..."]. The template wires the
    // user's UsageGoal into that attribute on every calendar render. Lock in
    // that contract: the bare wire from setting -> DOM attribute, which the
    // CSS palette on its own cannot verify.
    await registerOwnerOnCalendar(page, 'calendar-goal-palette');

    const calendarRoot = page.locator('[data-calendar-view]');
    await expect(calendarRoot).toHaveAttribute('data-usage-goal', 'health');

    const csrf = (await page.locator('meta[name="csrf-token"]').getAttribute('content')) ?? '';

    async function patchUsageGoal(goal: string): Promise<void> {
      // A body carrying the goal and nothing else takes the dashboard quick
      // switch's one-column path. This spec wants the ordinary partial save, so
      // it sends the geometry alongside the goal; every member it omits would
      // keep its stored value either way.
      const response = await page.request.patch('/api/v1/users/current/cycle', {
        headers: { ...apiOriginHeader(page), 'X-CSRF-Token': csrf, 'Content-Type': 'application/json' },
        data: {
          cycle_length: 28,
          period_length: 5,
          auto_period_fill: true,
          irregular_cycle: false,
          unpredictable_cycle: false,
          age_group: '',
          usage_goal: goal,
        },
      });
      expect(response.status(), `patch usage_goal=${goal}`).toBeLessThan(400);
    }

    await patchUsageGoal('avoid_pregnancy');
    await page.goto('/calendar');
    await expect(page.locator('[data-calendar-view]')).toHaveAttribute('data-usage-goal', 'avoid_pregnancy');

    await patchUsageGoal('trying_to_conceive');
    await page.goto('/calendar');
    await expect(page.locator('[data-calendar-view]')).toHaveAttribute('data-usage-goal', 'trying_to_conceive');
  });

  test('the two fertile tiers paint distinct fills for the default usage goal', async ({ page }) => {
    test.slow();

    // The tier classes (.calendar-cell-fertile-edge / -peak) always ship
    // alongside .calendar-cell-fertile, whose `background` SHORTHAND resets the
    // `background-color` they set. While the tier rules were written on the bare
    // class the two tied at equal specificity and the shorthand won wherever it
    // was emitted later — so the tiers rendered only under the two goal
    // selectors, and the default goal painted the whole window flat. The tier
    // rules are compounded with the base class now; this pins the RESULT, which
    // is the only form the tie cannot pass green.
    const creds = createCredentials('calendar-fertile-tiers');
    await registerOwnerViaUI(page, creds);
    await expectInlineRegisterRecoveryStep(page);
    await readRecoveryCode(page);
    await continueFromRecoveryCode(page);

    // Anchor the cycle at day 1 of the current month: on the 28/14 defaults the
    // predicted ovulation is cycle day 14 and the fertile window is the five
    // days before it, so the whole window (edge days 9-11, peak days 12-13,
    // ovulation on the 14th) sits mid-month and is inside that month's grid on
    // every run date — unlike a window anchored relative to today, which can
    // fall past the grid's trailing edge at a month's end.
    const monthISO = await page.evaluate(() => {
      const now = new Date();
      return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, '0')}`;
    });
    await selectOnboardingStartDate(page, `${monthISO}-01`);
    await page.locator('form[hx-post="/api/v1/onboarding/steps/1"] button[type="submit"]').click();
    const stepTwoForm = page.locator('form[hx-post="/api/v1/onboarding/steps/2"]');
    await expect(stepTwoForm).toBeVisible();
    await Promise.all([
      page.waitForURL(/\/dashboard(?:\?.*)?$/, { timeout: 15000 }),
      stepTwoForm.locator('[data-onboarding-step2-submit]').click(),
    ]);
    await setRequestTimezoneFromBrowser(page);

    // The fertile window is withheld until one cycle has been observed, so the
    // two cycles before this one are logged as well, 28 days apart — the length
    // the account settings already carry, so the window stays mid-month exactly
    // as described above. Two of them, not one: on a run date that IS the 1st,
    // the anchor itself has not closed a cycle yet, and the pair before it has.
    const anchorISO = `${monthISO}-01`;
    await markCycleStart(page, shiftISODate(anchorISO, -56));
    await markCycleStart(page, shiftISODate(anchorISO, -28));
    await markCycleStart(page, anchorISO);

    await page.goto(`/calendar?month=${monthISO}`);
    await expect(page.locator('[data-calendar-view]')).toHaveAttribute('data-usage-goal', 'health');

    // The ovulation day carries .calendar-cell-fertile on its own — that is the
    // flat fill the two tiers have to differ from.
    const flatCell = page.locator(
      '.calendar-cell-fertile:not(.calendar-cell-fertile-edge):not(.calendar-cell-fertile-peak)'
    );
    const edgeCell = page.locator('.calendar-cell-fertile-edge');
    const peakCell = page.locator('.calendar-cell-fertile-peak');
    await expect(flatCell.first()).toBeVisible();
    await expect(edgeCell.first()).toBeVisible();
    await expect(peakCell.first()).toBeVisible();

    for (const theme of ['light', 'dark'] as const) {
      await applyTheme(page, theme);

      const flat = await paintedBackgroundColor(flatCell.first());
      const edge = await paintedBackgroundColor(edgeCell.first());
      const peak = await paintedBackgroundColor(peakCell.first());

      expect(edge, `${theme}: fertile edge paints the flat fertile fill`).not.toBe(flat);
      expect(peak, `${theme}: fertile peak paints the flat fertile fill`).not.toBe(flat);
      expect(peak, `${theme}: fertile peak paints the fertile edge fill`).not.toBe(edge);

      // The contrast gate resolves rgb()/rgba() only; a fill that computes to
      // any other form (color-mix() serialises as color(srgb ...)) is one it
      // cannot flatten, so it would be unmeasurable rather than compliant.
      for (const [label, value] of [
        ['fertile edge', edge],
        ['fertile peak', peak],
      ] as const) {
        expect(value, `${theme}: ${label} fill is not resolvable as rgb()/rgba()`).toMatch(
          /^rgba?\(/
        );
      }
    }
  });

  test('summary-view manual cycle start on a day WITH logged data exposes its policy node so the confirm modal opens', async ({
    page,
  }) => {
    // Companion to the no-data summary regression above. #238 moved the hidden
    // [data-cycle-start-policy] node inside the cycle-start form's wrapper in
    // BOTH calendar summary-view branches of day_editor_partial.html, but its
    // regression only exercises the {{else}} "no entry" branch (a fresh day with
    // HasDayData false). This closes the gap on the {{if .HasDayData}} branch:
    // a day that HAS a logged entry must still render the manual cycle-start
    // form and its policy node as siblings inside the same wrapper, so the
    // confirm script's form.parentElement.querySelector('[data-cycle-start-policy]')
    // resolves and the short-gap modal opens instead of the POST silently 400ing.
    await registerOwnerOnCalendar(page, 'calendar-cycle-start-policy-withdata');

    const todayISO = await todayISOFromCalendar(page);
    const tomorrowISO = shiftISODate(todayISO, 1);
    const month = tomorrowISO.slice(0, 7);

    // Give tomorrow a NON-period, non-cycle-start entry (a note) so its summary
    // view renders the with-data branch. The onboarding anchor stays
    // last_period_start = today-3, so tomorrow is still a 4-day short gap and
    // manual cycle start stays allowed. Saving the note through the normal day
    // editor writes it on tomorrow's cell regardless of the runner timezone.
    const noteText = `calendar-withdata-${Date.now()}`;
    const dayEditorForm = await openCalendarDayEditor(page, tomorrowISO);
    await openCalendarNotes(dayEditorForm);
    await dayEditorForm.locator('#calendar-notes').fill(noteText);
    const [saveRequest] = await Promise.all([
      page.waitForRequest(
        (candidate) =>
          candidate.method() === 'PUT' && candidate.url().includes(`/api/v1/days/${tomorrowISO}`),
      ),
      dayEditorForm.locator('button[data-save-button]').click(),
    ]);
    const saveResponse = await saveRequest.response();
    expect(saveResponse, `expected a response for PUT /api/v1/days/${tomorrowISO}`).not.toBeNull();
    expect(
      saveResponse!.ok(),
      `PUT /api/v1/days/${tomorrowISO} failed with ${saveResponse!.status()}`
    ).toBeTruthy();

    // Reload the summary (non-edit) view for tomorrow. It now renders the
    // with-data branch: the logged note is shown by owner_log_summary, which the
    // "no entry" branch never renders — proof we are on the {{if .HasDayData}}
    // path — and the "edit entry" affordance sits next to the cycle-start form.
    await page.goto(`/calendar?month=${month}&day=${tomorrowISO}`);
    await expect(page).toHaveURL(new RegExp(`/calendar\\?month=${month}&day=${tomorrowISO}`));
    await expect(page.locator('#day-editor')).toContainText(noteText);
    await expect(page.locator(`[data-day-editor-open="${tomorrowISO}"]`).first()).toBeVisible();

    const manualStartForm = page.locator(`[data-day-cycle-start-form][data-day-cycle-start-date="${tomorrowISO}"]`);
    await expect(manualStartForm).toBeVisible();

    // Structural guard, identical to the no-data regression: the policy node
    // must be reachable from the form's parent, exactly as the confirm script
    // looks it up. This fails immediately if the template ever moves the policy
    // node back outside the with-data branch's cycle-start wrapper.
    const policyReachableFromForm = await manualStartForm.evaluate((form) =>
      Boolean(form.parentElement && form.parentElement.querySelector('[data-cycle-start-policy]')),
    );
    expect(policyReachableFromForm).toBe(true);

    // End-to-end: clicking opens the confirm modal, and accepting completes the
    // short-gap cycle start (HTTP < 400) instead of the silent 400.
    await manualStartForm.locator('[data-day-cycle-start-button]').click();
    await expect(page.locator('#confirm-modal')).toBeVisible();

    const [cycleStartRequest] = await Promise.all([
      page.waitForRequest((candidate) => {
        return (
          candidate.method() === 'POST' &&
          candidate.url().includes(`/api/v1/days/${tomorrowISO}/cycle-start?source=calendar`)
        );
      }),
      page.locator('#confirm-modal-accept').click(),
    ]);
    const cycleStartResponse = await cycleStartRequest.response();
    expect(
      cycleStartResponse,
      `expected a response for POST /api/v1/days/${tomorrowISO}/cycle-start?source=calendar`
    ).not.toBeNull();
    expect(cycleStartResponse!.status()).toBeLessThan(400);
  });
});
