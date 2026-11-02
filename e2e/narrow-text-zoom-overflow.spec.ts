import { expect, test, type Page } from './support/fixtures';
import {
  isoToday,
  markCycleStartViaAPI,
  registerAndOnboardWithStartDaysAgo,
  shiftISODate,
} from './support/stats-helpers';
import { saveInterfaceSettingsForm } from './support/settings-interface-helpers';

/**
 * Narrow phones at 200% text (WEB-441, WEB-442, WEB-439): the document never
 * scrolls sideways, and the danger-zone re-authentication row stacks its field
 * over its button instead of squeezing the two side by side.
 */
/** Several completed cycles and Russian copy: the widest the pages get. */
async function seedRussianAccountWithCycles(page: Page, prefix: string): Promise<void> {
  await registerAndOnboardWithStartDaysAgo(page, prefix, 40);
  for (const daysAgo of [14]) {
    await markCycleStartViaAPI(page, shiftISODate(isoToday(), -daysAgo));
  }
  await page.setViewportSize({ width: 375, height: 812 });
  await page.goto('/settings');
  const section = page.locator('#settings-interface');
  if ((await section.getAttribute('open')) === null) {
    await section.locator('summary').click();
  }
  await section.locator('[data-settings-interface-language-option="ru"] .chip-stack').click();
  await saveInterfaceSettingsForm(page, { expectSuccessFlash: true });
  await expect(page.locator('html')).toHaveAttribute('lang', 'ru');
}

async function zoomTextTo200(page: Page): Promise<void> {
  await page.evaluate(() => {
    document.documentElement.style.fontSize = '32px';
  });
}

async function horizontalOverflow(page: Page): Promise<{ scrollWidth: number; clientWidth: number; wide: string[] }> {
  return page.evaluate(() => {
    const root = document.documentElement;
    const wide = Array.from(document.body.querySelectorAll<HTMLElement>('*'))
      .filter((el) => el.getBoundingClientRect().right > root.clientWidth + 1 && el.getClientRects().length > 0)
      .filter((el) => !el.closest('[data-settings-section-nav], .sr-only'))
      .slice(0, 5)
      .map((el) => `${el.tagName}.${el.className}`);
    return { scrollWidth: root.scrollWidth, clientWidth: root.clientWidth, wide };
  });
}

for (const width of [320, 375]) {
  test.describe(`200% text at ${width}px`, () => {
    for (const path of ['/stats', '/dashboard', '/settings']) {
      test(`${path} does not scroll sideways`, async ({ page }) => {
        await seedRussianAccountWithCycles(page, `zoom-${width}-${path.slice(1)}`);
        await page.setViewportSize({ width, height: 700 });
        await page.goto(path);
        await zoomTextTo200(page);

        const { scrollWidth, clientWidth, wide } = await horizontalOverflow(page);
        expect(scrollWidth, `document is wider than the viewport; offenders: ${wide.join(', ')}`).toBeLessThanOrEqual(
          clientWidth,
        );
        const tabbar = await page.locator('nav.mobile-tabbar').boundingBox();
        expect(tabbar && tabbar.x + tabbar.width, 'the tab bar stays inside the viewport').toBeLessThanOrEqual(width);
      });
    }
  });
}

test('the danger-zone password rows stack the field over the button on a phone (WEB-439)', async ({ page }) => {
  await seedRussianAccountWithCycles(page, 'zoom-danger-rows');
  await page.setViewportSize({ width: 320, height: 700 });
  await page.goto('/settings');
  const danger = page.locator('#settings-danger-zone');
  if ((await danger.count()) > 0 && (await danger.getAttribute('open')) === null) {
    await danger.locator('summary').click();
  }

  for (const id of ['settings-clear-data-password', 'settings-delete-password']) {
    const input = page.locator(`#${id}`);
    await input.scrollIntoViewIfNeeded();
    const geometry = await input.evaluate((el) => {
      const row = el.closest('.settings-form-actions')!;
      const button = row.querySelector<HTMLElement>('button[type="submit"]')!;
      const field = el.getBoundingClientRect();
      const action = button.getBoundingClientRect();
      return {
        // Painted, not spelled: a class name left in the markup with no rule behind
        // it still lays the row out as a block and would pass the geometry alone.
        fieldBasis: getComputedStyle(el.closest('.settings-reauth-field')!).flexBasis,
        fieldWidth: field.width,
        rowWidth: row.getBoundingClientRect().width,
        fieldBottom: field.bottom,
        buttonTop: action.top,
        buttonHeight: action.height,
      };
    });
    expect(geometry.fieldBasis, `${id} wrapper claims the whole line`).toBe('100%');
    expect(geometry.fieldWidth, `${id} takes the row`).toBeGreaterThan(geometry.rowWidth * 0.7);
    expect(geometry.buttonTop, `${id} button sits below the field`).toBeGreaterThanOrEqual(geometry.fieldBottom);
    expect(geometry.buttonHeight, `${id} button is not stretched into an oval`).toBeLessThan(70);
  }
});
