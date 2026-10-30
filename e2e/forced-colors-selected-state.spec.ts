import { expect, test, type Locator, type Page } from './support/fixtures';
import { registerAndOnboardWithStartDaysAgo } from './support/stats-helpers';

/**
 * WEB-371: a Windows contrast theme repaints every background, so a selected
 * chip or the current page link that differs from its siblings only by fill
 * looks identical to them. Each state must keep a cue the repaint leaves alone:
 * border width/style, an outline, a text decoration or a heavier weight.
 */
async function geometryCue(locator: Locator): Promise<string> {
  return locator.evaluate((el) => {
    const s = getComputedStyle(el);
    return JSON.stringify([
      s.borderTopWidth,
      s.borderTopStyle,
      s.outlineStyle,
      s.outlineWidth,
      s.textDecorationLine,
      s.fontWeight,
    ]);
  });
}

async function expectForcedColors(page: Page): Promise<void> {
  await page.emulateMedia({ forcedColors: 'active' });
  expect(await page.evaluate(() => matchMedia('(forced-colors: active)').matches)).toBe(true);
}

test.describe('Forced colors: selected and current states keep a non-fill cue', () => {
  test('a selected mood chip differs from an unselected one by geometry', async ({ page }) => {
    await registerAndOnboardWithStartDaysAgo(page, 'fc-mood', 10);
    await page.setViewportSize({ width: 1280, height: 900 });
    await page.goto('/dashboard');
    await expectForcedColors(page);

    const mood = page.locator('label[data-mood-option]');
    await mood.first().scrollIntoViewIfNeeded();
    await mood.first().click();
    await expect(mood.first().locator('input')).toBeChecked();

    const selected = await geometryCue(mood.first().locator('.chip'));
    const unselected = await geometryCue(mood.nth(1).locator('.chip'));
    expect(selected, 'the selected mood chip is distinguishable without a background').not.toBe(unselected);
  });

  test('the active desktop nav link differs from an inactive one by geometry', async ({ page }) => {
    await registerAndOnboardWithStartDaysAgo(page, 'fc-nav', 10);
    await page.setViewportSize({ width: 1280, height: 900 });
    await page.goto('/dashboard');
    await expectForcedColors(page);

    const active = await geometryCue(page.locator('a.nav-link[aria-current="page"]'));
    const inactive = await geometryCue(page.locator('a.nav-link:not([aria-current])').first());
    expect(active, 'the current page link is distinguishable without a background').not.toBe(inactive);
  });

  test('the active tab-bar link differs from an inactive one by geometry', async ({ page }) => {
    await registerAndOnboardWithStartDaysAgo(page, 'fc-tabbar', 10);
    await page.setViewportSize({ width: 375, height: 812 });
    await page.goto('/dashboard');
    await expectForcedColors(page);

    const active = await geometryCue(page.locator('nav.mobile-tabbar a[aria-current="page"]'));
    const inactive = await geometryCue(page.locator('nav.mobile-tabbar a:not([aria-current])').first());
    expect(active, 'the current tab is distinguishable without a background').not.toBe(inactive);
  });

  test('the active language button differs from an inactive one by geometry', async ({ page }) => {
    await page.setViewportSize({ width: 1280, height: 900 });
    await page.goto('/login');
    await expectForcedColors(page);

    const active = await geometryCue(page.locator('.lang-switch .lang-link[aria-pressed="true"]'));
    const inactive = await geometryCue(page.locator('.lang-switch .lang-link[aria-pressed="false"]').first());
    expect(active, 'the current language is distinguishable without a background').not.toBe(inactive);
  });
});
