import { expect, test, type Page } from './support/fixtures';
import {
  completeOnboardingIfPresent,
  continueFromRecoveryCode,
  createCredentials,
  expectInlineRegisterRecoveryStep,
  readRecoveryCode,
  registerOwnerViaUI,
} from './support/auth-helpers';
import { setRequestTimezoneFromBrowser } from './support/timezone-helpers';

// WEB-492. The preference for reduced motion is honoured in three places: the
// CSS motion scale (transition durations collapse), the auth-panel fade
// (never armed), and every programmatic scroll (an instant jump instead of a
// smooth one). The unit suite pins the scripts against a stubbed matchMedia;
// here the real browser's emulated preference drives all three, and each
// observation is repeated under "no-preference" so the preference, not the
// page, is what makes the difference.

type Preference = 'reduce' | 'no-preference';
const PREFERENCES: Preference[] = ['reduce', 'no-preference'];

type ScrollArg = { block?: string; behavior?: string } | null;

async function registerOnboardedOwner(page: Page, prefix: string): Promise<void> {
  const credentials = createCredentials(prefix);
  await registerOwnerViaUI(page, credentials);
  await expectInlineRegisterRecoveryStep(page);
  await readRecoveryCode(page);
  await continueFromRecoveryCode(page);
  await completeOnboardingIfPresent(page);
  await setRequestTimezoneFromBrowser(page);
}

test.describe('Reduced motion', () => {
  for (const preference of PREFERENCES) {
    const reduced = preference === 'reduce';

    test(`auth panel fade is ${reduced ? 'never armed' : 'armed'} under ${preference}`, async ({
      page,
    }) => {
      await page.emulateMedia({ reducedMotion: preference });
      await page.goto('/login');
      const panel = page.locator('[data-auth-panel]');
      await expect(panel).toBeVisible();
      const seconds = await panel.evaluate((node) =>
        Math.max(
          ...getComputedStyle(node)
            .transitionDuration.split(',')
            .map((value) => parseFloat(value))
        )
      );
      if (reduced) {
        await expect(panel).not.toHaveClass(/auth-panel-transition/);
        expect(seconds).toBeLessThan(0.001);
      } else {
        await expect(panel).toHaveClass(/auth-panel-transition/);
        expect(seconds).toBeGreaterThan(0.05);
      }
    });

    test(`motion scale and quick-action scroll under ${preference}`, async ({ page }) => {
      test.slow();
      await registerOnboardedOwner(page, `reduced-motion-${reduced ? 'on' : 'off'}`);
      await page.emulateMedia({ reducedMotion: preference });
      await page.addInitScript(() => {
        const calls: unknown[] = [];
        (window as unknown as { __scrollCalls: unknown[] }).__scrollCalls = calls;
        const original: Element['scrollIntoView'] = Reflect.get(Element.prototype, 'scrollIntoView');
        Element.prototype.scrollIntoView = function (this: Element, arg?: unknown) {
          calls.push(arg ?? null);
          Reflect.apply(original, this, [arg]);
        };
      });
      await page.goto('/dashboard');

      // The CSS lever: the motion scale collapses to 0.01ms under "reduce".
      const button = page.locator('.dashboard-quick-action[data-quick-action="mood"]');
      await expect(button).toBeVisible();
      const seconds = await button.evaluate((node) =>
        Math.max(
          ...getComputedStyle(node)
            .transitionDuration.split(',')
            .map((value) => parseFloat(value))
        )
      );
      if (reduced) {
        expect(seconds).toBeLessThan(0.001);
      } else {
        expect(seconds).toBeGreaterThan(0.05);
      }

      // The script lever: the jump the reader triggers.
      await button.click();
      const calls = await page.evaluate(
        () => (window as unknown as { __scrollCalls: ScrollArg[] }).__scrollCalls
      );
      const jump = calls.find((arg) => arg?.block === 'center');
      expect(jump, 'the quick action scrolls its section into view').toBeTruthy();
      expect(jump?.behavior).toBe(reduced ? undefined : 'smooth');
    });
  }
});
