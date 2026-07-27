import { expect, type Locator, type Page } from '@playwright/test';

export function dashboardCycleHero(page: Page): Locator {
  return page.locator('[data-dashboard-cycle-hero]');
}

export function dashboardFallbackStatusLine(page: Page): Locator {
  return page.locator('[data-dashboard-status-line]');
}

export async function dashboardPrimarySummaryMode(page: Page): Promise<'hero' | 'fallback'> {
  const hero = dashboardCycleHero(page);
  const fallback = dashboardFallbackStatusLine(page);

  if ((await hero.count()) > 0) {
    await expect(hero).toBeVisible();
    await expect(fallback).toHaveCount(0);
    return 'hero';
  }

  await expect(fallback).toBeVisible();
  return 'fallback';
}

export async function dashboardNextPeriodText(page: Page): Promise<string> {
  const mode = await dashboardPrimarySummaryMode(page);
  if (mode === 'hero') {
    const value = await dashboardCycleHero(page)
      .locator('[data-dashboard-cycle-hero-next-period]')
      .textContent();
    return String(value || '').trim();
  }

  const value = await page.locator('[data-dashboard-next-period]').textContent();
  return String(value || '').trim();
}

export async function dashboardCurrentCycleDay(page: Page): Promise<number> {
  const mode = await dashboardPrimarySummaryMode(page);
  const text =
    mode === 'hero'
      ? await dashboardCycleHero(page).locator('.dashboard-cycle-hero-center-day').textContent()
      : await dashboardFallbackStatusLine(page).locator('.dashboard-status-item').nth(1).textContent();

  const match = String(text || '').match(/\d+/);
  expect(match, `Cannot parse cycle day from "${String(text || '').trim()}"`).toBeTruthy();
  return Number(match![0]);
}

/**
 * The phase the dashboard reports, as the locale-independent enum value.
 *
 * Both primary summaries declare it on `data-dashboard-phase`, so a spec can
 * assert `'menstrual'` instead of branching over «Менструальная» / "Menstrual"
 * per language — a regex alternation that quietly reduces to "any of these
 * words" and passes on the wrong phase whenever two languages share a spelling.
 */
export async function dashboardCurrentPhase(page: Page): Promise<string> {
  const mode = await dashboardPrimarySummaryMode(page);
  const root = mode === 'hero' ? dashboardCycleHero(page) : dashboardFallbackStatusLine(page);

  const phase = (await root.getAttribute('data-dashboard-phase')) ?? '';
  expect(phase, 'the dashboard summary must declare data-dashboard-phase').not.toBe('');
  return phase;
}
