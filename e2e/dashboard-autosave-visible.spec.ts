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

/**
 * WEB-369: a positive pregnancy test grows the saved outcome into the paused-
 * predictions sentence plus the bleeding/pain safety sentence. The bar used to
 * keep its sticky offset only while it was one line; grown, it ran under the
 * tab bar and covered the safety sentence.
 */
test('a long saved outcome keeps its safety sentence above the tab bar on a phone', async ({ page }) => {
  await registerAndOnboardWithStartDaysAgo(page, 'dash-autosave-long', 10);
  await page.setViewportSize({ width: 375, height: 812 });
  await page.goto('/dashboard');

  await page.evaluate(() => {
    document.querySelectorAll('details').forEach((d) => {
      if (d.querySelector('input[name="pregnancy_test"]')) d.open = true;
    });
  });
  await page.locator('[data-pregnancy-test-option="positive"]').click();

  const indicator = page.locator('[data-dashboard-autosave-indicator]');
  await expect(indicator).toHaveAttribute('data-autosave-state', 'saved');
  await expect(page.locator('#save-status')).toContainText(/bleeding/i);
  await page.evaluate(() => window.scrollTo(0, 0));

  const fit = await page.evaluate(() => {
    const bar = document.querySelector('.dashboard-autosave-bar')!;
    const status = document.querySelector('#save-status')!;
    const tabbarTop = document.querySelector('nav.mobile-tabbar')!.getBoundingClientRect().top;
    const sb = status.getBoundingClientRect();
    const hit = document.elementFromPoint(sb.left + 20, sb.bottom - 4);
    return {
      barBottom: bar.getBoundingClientRect().bottom,
      statusBottom: sb.bottom,
      statusClipped: status.scrollHeight > status.clientHeight + 1,
      tabbarTop,
      hitIsStatus: !!hit && status.contains(hit),
    };
  });
  expect(fit.barBottom, 'the bar clears the tab bar').toBeLessThanOrEqual(fit.tabbarTop);
  expect(fit.statusBottom, 'the safety sentence clears the tab bar').toBeLessThanOrEqual(fit.tabbarTop);
  expect(fit.statusClipped, 'the safety sentence is not clipped').toBe(false);
  expect(fit.hitIsStatus, 'nothing paints over the safety sentence').toBe(true);
});
