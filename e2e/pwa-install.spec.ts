import { expect, test } from '@playwright/test';
import { localeText } from './support/locale-helpers';

test.describe('PWA install prompt', () => {
  test('shows the custom mobile install CTA and triggers the native prompt', async ({ page }) => {
    await page.addInitScript(() => {
      (window as Window & { __pwaInstallPromptCalls?: number }).__pwaInstallPromptCalls = 0;

      window.addEventListener('DOMContentLoaded', () => {
        const installEvent = new Event('beforeinstallprompt');

        Object.defineProperty(installEvent, 'prompt', {
          configurable: true,
          value: () => {
            const state = window as Window & { __pwaInstallPromptCalls?: number };
            state.__pwaInstallPromptCalls = (state.__pwaInstallPromptCalls ?? 0) + 1;
            return Promise.resolve();
          },
        });
        Object.defineProperty(installEvent, 'userChoice', {
          configurable: true,
          value: Promise.resolve({ outcome: 'accepted', platform: 'web' }),
        });

        window.dispatchEvent(installEvent);
      });
    });

    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto('/login');

    // Address the banner and its action by their hooks, not by a styling class
    // and an accessible name: `.mobile-install-banner` is a layout class and
    // getByRole({ name: 'Install app' }) only resolves in English. The rendered
    // title is still asserted once, against the key the template declares.
    const banner = page.locator('[data-pwa-install-banner]');
    const installTitle = banner.locator('[data-pwa-install-title]');
    await expect(banner).toBeVisible();
    await expect(installTitle).toHaveAttribute('data-pwa-install-title-key', 'pwa.install.title');
    await expect(installTitle).toHaveText(localeText('en', 'pwa.install.title'));

    await banner.locator('[data-pwa-install-action="install"]').click();

    await expect
      .poll(async () => {
        return page.evaluate(() => {
          return (window as Window & { __pwaInstallPromptCalls?: number }).__pwaInstallPromptCalls ?? 0;
        });
      })
      .toBe(1);
    await expect(banner).toBeHidden();
  });
});
