import { expect, test, type Page } from '@playwright/test';
import {
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  readRecoveryCode,
  registerOwnerViaUI,
} from './support/auth-helpers';
import { localeText } from './support/locale-helpers';

type InstallPromptWindow = Window & { __pwaInstallPromptCalls?: number };

// Arms a fake `beforeinstallprompt` on every page load of this context and counts
// how often the page asks the browser to prompt. The outcome is a parameter: the
// accepted path marks the app installed, which is the wrong end state for a spec
// that keeps navigating.
async function armInstallPrompt(page: Page, outcome: 'accepted' | 'dismissed'): Promise<void> {
  await page.addInitScript((choice) => {
    (window as InstallPromptWindow).__pwaInstallPromptCalls = 0;

    window.addEventListener('DOMContentLoaded', () => {
      const installEvent = new Event('beforeinstallprompt');

      Object.defineProperty(installEvent, 'prompt', {
        configurable: true,
        value: () => {
          const state = window as InstallPromptWindow;
          state.__pwaInstallPromptCalls = (state.__pwaInstallPromptCalls ?? 0) + 1;
          return Promise.resolve();
        },
      });
      Object.defineProperty(installEvent, 'userChoice', {
        configurable: true,
        value: Promise.resolve({ outcome: choice, platform: 'web' }),
      });

      window.dispatchEvent(installEvent);
    });
  }, outcome);
}

function installPromptCalls(page: Page): Promise<number> {
  return page.evaluate(() => (window as InstallPromptWindow).__pwaInstallPromptCalls ?? 0);
}

test.describe('PWA install prompt', () => {
  test('offers the install in one compact row and triggers the native prompt', async ({ page }) => {
    await armInstallPrompt(page, 'accepted');

    await page.setViewportSize({ width: 390, height: 844 });
    await page.goto('/login');

    // Address the offer and its action by their hooks, not by a styling class and
    // an accessible name: `.mobile-install-offer` is a layout class and
    // getByRole({ name: 'Install Ovumcy' }) only resolves in English. The rendered
    // label is still asserted once, against the key the template declares.
    const offer = page.locator('[data-pwa-install-offer]');
    const installButton = offer.locator('[data-pwa-install-action="install"]');
    await expect(offer).toBeVisible();
    await expect(installButton).toHaveAttribute('data-pwa-install-title-key', 'pwa.install.title');
    await expect(installButton).toHaveText(localeText('en', 'pwa.install.title'));

    // The compact contract, measured rather than eyeballed: the offer is a single
    // row of two controls and carries none of the pitch paragraphs it replaced.
    await expect(offer.locator('[data-pwa-install-action]')).toHaveCount(2);
    await expect(offer.locator('[data-pwa-install-copy]')).toHaveCount(0);
    const offerBox = await offer.boundingBox();
    const buttonBox = await installButton.boundingBox();
    expect(offerBox, 'the install offer has no layout box').not.toBeNull();
    expect(buttonBox, 'the install action has no layout box').not.toBeNull();
    expect(offerBox!.height).toBeLessThan(buttonBox!.height * 2);

    await installButton.click();

    await expect.poll(() => installPromptCalls(page)).toBe(1);
    await expect(offer).toBeHidden();
  });

  test('a dismissed offer stays reachable from the settings interface section', async ({ page }) => {
    await armInstallPrompt(page, 'dismissed');
    await page.setViewportSize({ width: 390, height: 844 });

    const credentials = createCredentials('pwa-install-settings');
    await registerOwnerViaUI(page, credentials);
    await expectInlineRegisterRecoveryStep(page);
    await readRecoveryCode(page);
    await continueFromRecoveryCode(page);
    await completeOnboardingIfPresent(page);

    const offer = page.locator('[data-pwa-install-offer]');
    await expect(offer).toBeVisible();
    await offer.locator('[data-pwa-install-action="dismiss"]').click();
    await expect(offer).toBeHidden();

    await page.goto('/settings');

    // The dismissal is remembered client-side, so the row above the page stays
    // away — and the settings entry keeps the install one click from here.
    await expect(offer).toBeHidden();
    const settingsRow = page.locator('[data-pwa-install-settings]');
    const settingsInstall = settingsRow.locator('[data-pwa-install-action="install"]');
    await expect(settingsRow).toBeVisible();
    await expect(settingsInstall).toBeVisible();
    await expect(settingsInstall).toHaveText(localeText('en', 'pwa.install.action.install'));
    await expect(settingsRow.locator('[data-pwa-install-hint="prompt"]')).toBeVisible();

    await settingsInstall.click();
    await expect.poll(() => installPromptCalls(page)).toBe(1);
  });
});
