import { test, type Page } from './support/fixtures';
import { assertNoHorizontalOverflow } from './support/mobile-layout-helpers';
import { registerAndOnboardWithStartDaysAgo } from './support/stats-helpers';
import { saveInterfaceSettingsForm } from './support/settings-interface-helpers';

/**
 * WEB-370 (WCAG 1.4.10): at 200% text size on a 375px phone the dashboard and
 * settings pages must reflow with no horizontal scroll. The longest copy is
 * Russian, so the pages are read in ru.
 */
async function russianOwnerAtTwoHundredPercent(page: Page, prefix: string): Promise<void> {
  await registerAndOnboardWithStartDaysAgo(page, prefix, 10);
  await page.setViewportSize({ width: 375, height: 812 });
  await page.goto('/settings');
  await page.locator('#settings-interface summary').click();
  await page.locator('[data-settings-interface-language-option="ru"] .chip-stack').click();
  await saveInterfaceSettingsForm(page, { expectSuccessFlash: true });
}

async function zoomAndMeasure(page: Page, path: string): Promise<void> {
  await page.goto(path);
  await page.evaluate(() => {
    document.documentElement.style.fontSize = '32px';
  });
  await assertNoHorizontalOverflow(page);
}

test.describe('200% text zoom at 375 reflows without horizontal scroll', () => {
  test('dashboard in Russian', async ({ page }) => {
    await russianOwnerAtTwoHundredPercent(page, 'zoom-dashboard');
    await zoomAndMeasure(page, '/dashboard');
  });

  test('settings in Russian', async ({ page }) => {
    await russianOwnerAtTwoHundredPercent(page, 'zoom-settings');
    await zoomAndMeasure(page, '/settings');
  });
});
