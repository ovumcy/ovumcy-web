import { expect, type Locator, type Page } from '@playwright/test';

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
