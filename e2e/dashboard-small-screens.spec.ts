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

  test('Escape closes the goal chip and returns focus to it', async ({ page }) => {
    await registerAndOnboardWithStartDaysAgo(page, 'dash-chip-escape', 10);
    await page.setViewportSize({ width: 375, height: 700 });
    await page.goto('/dashboard');

    const chip = page.locator('[data-usage-goal-chip]');
    await chip.click();
    const panel = page.locator('[data-usage-goal-quick-switch]');
    await expect(panel).toBeVisible();

    await page.keyboard.press('Escape');
    await expect(panel).toBeHidden();
    await expect(chip).toBeFocused();

    await chip.click();
    await expect(panel).toBeVisible();
    await page.locator('[data-usage-goal-choice]').first().focus();
    await page.keyboard.press('Escape');
    await expect(panel).toBeHidden();
    await expect(chip).toBeFocused();
  });

  test('ticking the period day refreshes the status header', async ({ page }) => {
    await registerAndOnboardWithStartDaysAgo(page, 'dash-hero-refresh', 10);
    await page.goto('/dashboard');

    const toggle = page.locator('input[name="is_period"]');
    const label = page.locator('.period-toggle-main');
    const labelBefore = (await label.textContent())?.trim();
    // The cycle-day node is replaced by a refresh, so a mark on it is lost.
    await page.locator('[data-dashboard-cycle-day]').evaluate((node) => {
      (node as HTMLElement & { staleMark?: boolean }).staleMark = true;
    });
    const refresh = page.waitForResponse(
      (response) =>
        response.request().method() === 'GET' &&
        new URL(response.url()).pathname === '/dashboard' &&
        response.request().resourceType() === 'fetch',
    );
    await toggle.setChecked(!(await toggle.isChecked()));
    await refresh;
    await expect
      .poll(() =>
        page
          .locator('[data-dashboard-cycle-day]')
          .evaluate((node) => (node as HTMLElement & { staleMark?: boolean }).staleMark === true),
      )
      .toBe(false);
    expect((await label.textContent())?.trim()).toBe(labelBefore);
  });

  test('an htmx Save of the dashboard form refreshes the status header', async ({ page }) => {
    await registerAndOnboardWithStartDaysAgo(page, 'dash-save-refresh', 10);
    await page.goto('/dashboard');

    await page.locator('[data-dashboard-cycle-day]').evaluate((node) => {
      (node as HTMLElement & { staleMark?: boolean }).staleMark = true;
    });
    const save = page.waitForResponse(
      (response) => response.request().method() === 'PATCH' || response.request().method() === 'PUT',
    );
    // Tick and submit in one task, before the autosave timer can take the change.
    await page.locator('[data-dashboard-save-form]').evaluate((form) => {
      const toggle = form.querySelector<HTMLInputElement>('input[name="is_period"]')!;
      toggle.checked = !toggle.checked;
      (form as HTMLFormElement).requestSubmit();
    });
    await save;
    await expect
      .poll(() =>
        page
          .locator('[data-dashboard-cycle-day]')
          .evaluate((node) => (node as HTMLElement & { staleMark?: boolean }).staleMark === true),
      )
      .toBe(false);
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
      // 1.5rem lands a hair under 24 on some engines' subpixel rounding.
      expect(box!.height).toBeGreaterThanOrEqual(23.9);
    }
  });
});
