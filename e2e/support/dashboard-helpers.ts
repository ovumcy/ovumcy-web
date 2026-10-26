import { expect, type Locator, type Page, type Request } from '@playwright/test';

export function dashboardSaveForm(page: Page): Locator {
  return page.locator('[data-dashboard-save-form]').first();
}

/**
 * Today's ISO date as the journal itself reports it.
 *
 * The form keeps its `hx-put` after the save button was removed — the autosave
 * runner reads the verb and URL from it — so it stays the one place a spec
 * learns which day it is editing.
 */
export async function dashboardTodayPath(page: Page): Promise<string> {
  const action = await dashboardSaveForm(page).getAttribute('hx-put');
  expect(action).toMatch(/^\/api\/v1\/days\/\d{4}-\d{2}-\d{2}$/);
  return String(action);
}

export async function dashboardTodayISO(page: Page): Promise<string> {
  return (await dashboardTodayPath(page)).replace('/api/v1/days/', '');
}

const pinnedClockPages = new WeakSet<Page>();

/**
 * Freezes the page's clock at local noon of today, once per page.
 *
 * Noon, not the moment of the call: the page keys its day at the start of the
 * navigation (`Date.now()` minus the time elapsed since it began), so a clock
 * pinned seconds after local midnight would key the page onto the previous day
 * while the server renders the new one. Noon is twelve hours from either
 * boundary, so the pinned day is the server's day for any run that does not
 * straddle a whole half-day.
 *
 * The dashboard reloads itself onto the new day the first time its autosave
 * settles after the browser's local midnight (`reloadDashboardOnNewDay`). That
 * is right for an owner and wrong for a spec that crosses midnight between two
 * saves: the reload replaces the page the spec is holding, and the control it
 * waits on is gone (WEB-321, an undo that vanished at 00:00:01). Pinning makes
 * the page's `Date` stand still, so the rollover check never sees a new day;
 * timers keep running, so the autosave debounce still fires. Call it BEFORE the
 * navigation that opens the dashboard: the day the page opened on is recorded
 * at load, and the pin carries into every later load of the same context.
 * `saveDashboardEntry` also calls it, for a spec that reached the dashboard on
 * its own path.
 */
export async function pinDashboardClock(page: Page): Promise<void> {
  if (pinnedClockPages.has(page)) {
    return;
  }
  pinnedClockPages.add(page);
  const noon = new Date();
  noon.setHours(12, 0, 0, 0);
  await page.clock.setFixedTime(noon);
}

/**
 * Makes the changes in `edit` and waits for the autosave they trigger.
 *
 * The dashboard has no save button: every change re-arms a 2 s debounce, so the
 * request listener is registered BEFORE `edit` runs and the wait binds to the
 * autosave's own request (`waitForRequest` → `request.response()`), never to
 * page state. `edit` must therefore contain field interactions only — no
 * navigation, and nothing that idles longer than the debounce, or the runner
 * would send a first PATCH mid-way through it.
 *
 * The settle assertion afterwards is the second half: the runner drops the
 * dirty flag only once the server has answered, so a form that still carries it
 * has a save the spec has not seen yet — and a `page.goto` at that moment would
 * abort it, which is silent data loss.
 */
export async function saveDashboardEntry(
  page: Page,
  edit: () => Promise<void>,
  options?: { savePath?: string }
): Promise<string> {
  await pinDashboardClock(page);
  const path = options?.savePath ?? (await dashboardTodayPath(page));
  const matchesSave = (candidate: Request) =>
    candidate.method() === 'PATCH' && candidate.url().includes(path);

  const sent: Request[] = [];
  const record = (candidate: Request) => {
    if (matchesSave(candidate)) {
      sent.push(candidate);
    }
  };
  page.on('request', record);

  try {
    const requestPromise = page.waitForRequest(matchesSave);
    await edit();
    await requestPromise;

    const form = dashboardSaveForm(page);
    await expect(form).not.toHaveAttribute('data-autosave-dirty', 'true');

    // Every attempt that left the page has to have landed: a spec must not read
    // its data back from a save that was refused.
    for (const request of sent) {
      const response = await request.response();
      expect(response, `expected a response for PATCH ${path}`).not.toBeNull();
      expect(response!.ok(), `PATCH ${path} failed with ${response!.status()}`).toBeTruthy();
    }
  } finally {
    page.off('request', record);
  }

  return path.replace('/api/v1/days/', '');
}

/**
 * The journal's "More" disclosure, holding the fields a day rarely carries.
 */
export function dashboardMoreDisclosure(page: Page): Locator {
  return page.locator('[data-dashboard-more]');
}

/**
 * Opens the "More" disclosure when it is closed.
 *
 * A control inside a closed `<details>` is not rendered, so a spec that fills
 * BBT, cervical mucus, cycle factors or notes on the dashboard has to open the
 * disclosure first — the server renders it open only for a day that already
 * holds one of those values.
 */
export async function openDashboardMoreFields(page: Page): Promise<Locator> {
  const disclosure = dashboardMoreDisclosure(page);
  await expect(disclosure).toHaveCount(1);

  // Its own summary, not the ones belonging to the disclosures it holds
  // (intimacy, notes) — a bare `summary` descendant locator matches three.
  if (!(await disclosure.evaluate((node) => node.hasAttribute('open')))) {
    await disclosure.locator('> summary').click();
  }
  await expect(disclosure).toHaveAttribute('open', '');
  return disclosure;
}

/**
 * The dashboard's single status header.
 *
 * The wave-2 design pass removed the hero/status-line either-or: one header is
 * always rendered, so a spec no longer branches over which summary it got.
 */
export function dashboardStatusHeader(page: Page): Locator {
  return page.locator('[data-dashboard-status-header]');
}

export function dashboardStatusLine(page: Page): Locator {
  return page.locator('[data-dashboard-status-line]');
}

export async function expectDashboardStatusHeader(page: Page): Promise<Locator> {
  const header = dashboardStatusHeader(page);
  await expect(header).toBeVisible();
  return header;
}

export async function dashboardNextPeriodText(page: Page): Promise<string> {
  await expectDashboardStatusHeader(page);

  const value = await page.locator('[data-dashboard-next-period]').textContent();
  return String(value || '').trim();
}

export async function dashboardCurrentCycleDay(page: Page): Promise<number> {
  const header = await expectDashboardStatusHeader(page);
  const text = await header.locator('[data-dashboard-cycle-day]').textContent();

  const match = String(text || '').match(/\d+/);
  expect(match, `Cannot parse cycle day from "${String(text || '').trim()}"`).toBeTruthy();
  return Number(match![0]);
}

/**
 * The phase the dashboard reports, as the locale-independent enum value.
 *
 * The header declares it on `data-dashboard-phase`, so a spec can assert
 * `'menstrual'` instead of branching over «Менструальная» / "Menstrual" per
 * language — a regex alternation that quietly reduces to "any of these words"
 * and passes on the wrong phase whenever two languages share a spelling.
 */
export async function dashboardCurrentPhase(page: Page): Promise<string> {
  const header = await expectDashboardStatusHeader(page);

  const phase = (await header.getAttribute('data-dashboard-phase')) ?? '';
  expect(phase, 'the dashboard status header must declare data-dashboard-phase').not.toBe('');
  return phase;
}
