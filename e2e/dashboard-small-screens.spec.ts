import { expect, test, type Page } from './support/fixtures';
import { localeText } from './support/locale-helpers';
import { assertNoHorizontalOverflow } from './support/mobile-layout-helpers';
import {
  markCycleStartViaAPI,
  registerAndOnboardWithStartDaysAgo,
  shiftISODate,
} from './support/stats-helpers';

/**
 * Dashboard on the narrowest supported phones (WEB-259): the status line keeps
 * "Label:" together and never strands a separator, the goal-chip panel stays in
 * the viewport, the capped ribbon says it is capped, and the ribbon cells name
 * today and the fertile window.
 */
const WIDTHS = [375, 320];

async function lineCount(page: Page, selector: string): Promise<number> {
  return page.evaluate((sel) => {
    const item = document.querySelector<HTMLElement>(sel)?.parentElement;
    const label = Array.from(item?.childNodes ?? []).find(
      (node) => node.nodeType === Node.TEXT_NODE && (node.textContent ?? '').trim() !== '',
    );
    if (!label) {
      return -1;
    }
    const range = document.createRange();
    range.selectNodeContents(label);
    return new Set(Array.from(range.getClientRects()).map((rect) => Math.round(rect.top))).size;
  }, selector);
}

test.describe('Dashboard on small screens', () => {
  test('status labels stay whole, the goal chip fits, ribbon cells name today and fertile', async ({
    page,
  }) => {
    const anchor = await registerAndOnboardWithStartDaysAgo(page, 'dash-small', 10);
    for (const offset of [-84, -56, -28]) {
      await markCycleStartViaAPI(page, shiftISODate(anchor, offset));
    }

    for (const width of WIDTHS) {
      await page.setViewportSize({ width, height: 700 });
      await page.goto('/dashboard');
      await page.locator('[data-usage-goal-chip]').click();
      await expect(page.locator('[data-usage-goal-quick-switch]')).toBeVisible();
      await assertNoHorizontalOverflow(page);

      const panel = await page.locator('[data-usage-goal-quick-switch]').boundingBox();
      expect(panel!.x + panel!.width, `goal panel leaves the viewport at ${width}px`).toBeLessThanOrEqual(
        width + 1,
      );

      expect(await lineCount(page, '[data-dashboard-next-period]'), `next-period label wraps at ${width}px`).toBe(
        1,
      );

      const separators = await page.evaluate(() =>
        Array.from(document.querySelectorAll<HTMLElement>('.dashboard-status-separator')).filter(
          (node) => node.offsetParent !== null,
        ).length,
      );
      expect(separators, `status separators stay visible at ${width}px`).toBe(0);
    }

    const today = page.locator('[data-cycle-ribbon-day][data-today="true"]');
    await expect(today).toHaveAttribute('aria-label', new RegExp(localeText('en', 'nav.today')));
    const fertile = page.locator('[data-cycle-ribbon-day][data-fertile="true"]').first();
    await expect(fertile).toHaveAttribute('aria-label', new RegExp(localeText('en', 'dashboard.fertile_window')));
  });

  test('a cycle past the 60-day ribbon says it is cut off', async ({ page }) => {
    const anchor = await registerAndOnboardWithStartDaysAgo(page, 'dash-ribbon-cap', 60);
    for (const offset of [-140, -70]) {
      await markCycleStartViaAPI(page, shiftISODate(anchor, offset));
    }
    await page.setViewportSize({ width: 375, height: 700 });
    await page.goto('/dashboard');

    const note = page.locator('[data-dashboard-cycle-ribbon-truncated]');
    await expect(note).toBeVisible();
    await expect(note).toContainText('60');
  });

  test('late-cycle quick links have a 24px hit area', async ({ page }) => {
    const anchor = await registerAndOnboardWithStartDaysAgo(page, 'dash-late-links', 60);
    for (const offset of [-84, -56, -28]) {
      await markCycleStartViaAPI(page, shiftISODate(anchor, offset));
    }
    await page.setViewportSize({ width: 375, height: 700 });
    await page.goto('/dashboard');

    const links = await page.locator('[data-dashboard-late-cycle-actions] .inline-link').all();
    expect(links.length).toBeGreaterThan(0);
    for (const link of links) {
      const box = await link.boundingBox();
      expect(box!.height).toBeGreaterThanOrEqual(24);
    }
  });
});
