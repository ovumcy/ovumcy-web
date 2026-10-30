import { expect, test, type Page } from './support/fixtures';
import { registerAndOnboardWithStartDaysAgo } from './support/stats-helpers';
import { saveInterfaceSettingsForm } from './support/settings-interface-helpers';

/**
 * Phone chrome (WEB-372, WEB-373): the sticky header and the floating tab bar
 * must never hide, or break the words of, what they sit next to.
 */
async function openInterfaceSection(page: Page): Promise<void> {
  const section = page.locator('#settings-interface');
  if ((await section.getAttribute('open')) === null) {
    await section.locator('summary').click();
  }
}

test.describe('Phone chrome geometry', () => {
  test('the Interface save confirmation lands below the sticky header (WEB-372)', async ({ page }) => {
    await registerAndOnboardWithStartDaysAgo(page, 'phone-iface-status', 10);
    await page.setViewportSize({ width: 375, height: 812 });
    await page.goto('/settings');
    await openInterfaceSection(page);

    const form = page.locator('[data-settings-interface-form]');
    await form.locator('[data-settings-interface-language-option="ru"] .chip-stack').click();
    await saveInterfaceSettingsForm(page, { expectSuccessFlash: true });
    await expect(page).toHaveURL(/\/settings#settings-interface-status$/);
    await expect(page.locator('html')).toHaveAttribute('lang', 'ru');

    const status = page.locator('#settings-interface-status .status-ok');
    await expect(status).toBeVisible();
    // The anchor jump has landed once the page has scrolled and stopped.
    let last = -1;
    await expect
      .poll(async () => {
        const y = await page.evaluate(() => window.scrollY);
        const settled = y > 0 && y === last;
        last = y;
        return settled;
      })
      .toBe(true);

    const geometry = await page.evaluate(() => {
      const header = document.querySelector('header')!.getBoundingClientRect();
      const box = document.querySelector('#settings-interface-status .status-ok')!.getBoundingClientRect();
      const hit = document.elementFromPoint(box.left + 20, box.top + 5);
      return {
        headerBottom: header.bottom,
        statusTop: box.top,
        statusBottom: box.bottom,
        viewport: window.innerHeight,
        hitIsStatus: !!hit && !!hit.closest('#settings-interface-status'),
      };
    });
    expect(geometry.statusTop, 'the status starts below the sticky header').toBeGreaterThanOrEqual(
      geometry.headerBottom,
    );
    expect(geometry.hitIsStatus, 'the header does not paint over the status').toBe(true);
    expect(geometry.statusBottom, 'the status is inside the viewport').toBeLessThanOrEqual(geometry.viewport);
  });

  test('the Russian tab bar at 320 never splits a word (WEB-373)', async ({ page }) => {
    await registerAndOnboardWithStartDaysAgo(page, 'phone-tabbar-ru', 10);
    await page.setViewportSize({ width: 320, height: 700 });
    await page.goto('/settings');
    await openInterfaceSection(page);
    await page.locator('[data-settings-interface-language-option="ru"] .chip-stack').click();
    await saveInterfaceSettingsForm(page, { expectSuccessFlash: true });
    await page.goto('/stats');
    await expect(page.locator('html')).toHaveAttribute('lang', 'ru');

    const split = await page.evaluate(() => {
      const broken: string[] = [];
      document.querySelectorAll<HTMLElement>('nav.mobile-tabbar a').forEach((link) => {
        const node = Array.from(link.childNodes).find(
          (child) => child.nodeType === Node.TEXT_NODE && (child.textContent ?? '').trim() !== '',
        );
        if (!node) return;
        const text = node.textContent ?? '';
        for (const match of text.matchAll(/\S+/g)) {
          const range = document.createRange();
          range.setStart(node, match.index);
          range.setEnd(node, match.index + match[0].length);
          const lines = new Set(Array.from(range.getClientRects()).map((rect) => Math.round(rect.top)));
          if (lines.size > 1) broken.push(match[0]);
        }
      });
      return broken;
    });
    expect(split, 'no tab label wraps inside a word').toEqual([]);

    const overflow = await page.evaluate(() =>
      Array.from(document.querySelectorAll<HTMLElement>('nav.mobile-tabbar a')).filter(
        (link) => link.scrollWidth > link.clientWidth + 1,
      ).length,
    );
    expect(overflow, 'no tab label is clipped by its cell').toBe(0);
  });
});
