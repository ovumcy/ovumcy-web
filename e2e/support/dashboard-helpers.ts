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

export async function dashboardCurrentPhaseText(page: Page): Promise<string> {
  const mode = await dashboardPrimarySummaryMode(page);
  const text =
    mode === 'hero'
      ? await dashboardCycleHero(page).locator('.dashboard-cycle-hero-center-phase').textContent()
      : await dashboardFallbackStatusLine(page).locator('.dashboard-status-item').first().textContent();

  return String(text || '').trim();
}
