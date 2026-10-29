import { expect, test } from './support/fixtures';
import { registerAndOnboardWithStartDaysAgo } from './support/stats-helpers';

/**
 * WEB-276: on a phone the autosave outcome used to render after every form
 * section, hundreds of pixels below the fold, so a sighted owner saw no
 * confirmation. The bar now pins itself above the floating tab bar.
 */
test('the autosave confirmation stays on screen above the tab bar on a phone', async ({ page }) => {
  await registerAndOnboardWithStartDaysAgo(page, 'dash-autosave-visible', 10);
  await page.setViewportSize({ width: 375, height: 812 });
  await page.goto('/dashboard');

  const mood = page.locator('label[data-mood-option]').first();
  await mood.scrollIntoViewIfNeeded();
  await mood.click();

  const indicator = page.locator('[data-dashboard-autosave-indicator]');
  await expect(indicator).toHaveAttribute('data-autosave-state', 'saved');

  const fit = await page.evaluate(() => {
    const indicatorBox = document.querySelector('[data-dashboard-autosave-indicator]')!.getBoundingClientRect();
    const tabbarTop = document.querySelector('nav.mobile-tabbar')!.getBoundingClientRect().top;
    return { top: indicatorBox.top, bottom: indicatorBox.bottom, tabbarTop, viewport: window.innerHeight };
  });
  expect(fit.top, 'the confirmation starts inside the viewport').toBeGreaterThanOrEqual(0);
  expect(fit.bottom, 'the confirmation clears the tab bar').toBeLessThanOrEqual(fit.tabbarTop);
});
