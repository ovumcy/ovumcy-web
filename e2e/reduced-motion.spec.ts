import { expect, test, type Locator } from './support/fixtures';
import { registerAndOnboardWithStartDaysAgo } from './support/stats-helpers';

// WEB-492. The preference for reduced motion is honoured in three places: the
// CSS motion scale (transition durations collapse), the auth-panel fade
// (never armed), and every programmatic scroll (an instant jump instead of a
// smooth one). The unit suite pins the scripts against a stubbed matchMedia;
// here the real browser's emulated preference drives all three, and each
// observation is repeated under "no-preference" so the preference, not the
// page, is what makes the difference.

type ScrollArg = { block?: string; behavior?: string } | null;

async function maxTransitionSeconds(target: Locator): Promise<number> {
  return target.evaluate((node) =>
    Math.max(
      ...getComputedStyle(node)
        .transitionDuration.split(',')
        .map((value) => parseFloat(value))
    )
  );
}

test.describe('Reduced motion', () => {
  test('auth panel fade is armed only without the preference, and the motion scale collapses', async ({
    page,
  }) => {
    await page.emulateMedia({ reducedMotion: 'reduce' });
    await page.goto('/login');
    const panel = page.locator('[data-auth-panel]');
    await expect(panel).toBeVisible();
    await expect(panel).not.toHaveClass(/auth-panel-transition/);
    // Force the class the script withheld, so the duration read is the CSS
    // scale's own answer and not the absence of a transition.
    await panel.evaluate((node) => node.classList.add('auth-panel-transition'));
    const reduced = await maxTransitionSeconds(panel);
    expect(reduced).toBeLessThan(0.001);

    await page.emulateMedia({ reducedMotion: 'no-preference' });
    await page.goto('/login');
    await expect(panel).toBeVisible();
    await expect(panel).toHaveClass(/auth-panel-transition/);
    const unreduced = await maxTransitionSeconds(panel);
    expect(unreduced).toBeGreaterThan(100 * reduced);
  });

  test('dashboard motion scale and every scroll honour the preference', async ({ page }) => {
    test.slow();
    await registerAndOnboardWithStartDaysAgo(page, 'reduced-motion', 5);
    await page.addInitScript(() => {
      const calls: unknown[] = [];
      (window as unknown as { __scrollCalls: unknown[] }).__scrollCalls = calls;
      const original: Element['scrollIntoView'] = Reflect.get(Element.prototype, 'scrollIntoView');
      Element.prototype.scrollIntoView = function (this: Element, arg?: unknown) {
        calls.push(arg ?? null);
        Reflect.apply(original, this, [arg]);
      };
    });

    const observe = async (preference: 'reduce' | 'no-preference') => {
      await page.emulateMedia({ reducedMotion: preference });
      // The hash target is scrolled to on load; the quick action on click.
      // Leave the document first: a goto to the same path with a hash is a
      // same-document jump, which would skip the init script and the load.
      await page.goto('about:blank');
      await page.goto('/dashboard#dashboard-symptoms');
      const button = page.locator('.dashboard-quick-action[data-quick-action="mood"]');
      await expect(button).toBeVisible();
      const seconds = await maxTransitionSeconds(button);
      await button.click();
      const calls = await page.evaluate(
        () => (window as unknown as { __scrollCalls: ScrollArg[] }).__scrollCalls
      );
      const hash = calls.find((arg) => arg?.block === 'start');
      const quick = calls.find((arg) => arg?.block === 'center');
      expect(hash, 'the hash target is scrolled into view').toBeTruthy();
      expect(quick, 'the quick action scrolls its section into view').toBeTruthy();
      return { seconds, hash, quick };
    };

    const reduced = await observe('reduce');
    expect(reduced.seconds).toBeLessThan(0.001);
    expect(reduced.hash?.behavior).not.toBe('smooth');
    expect(reduced.quick?.behavior).not.toBe('smooth');

    const unreduced = await observe('no-preference');
    expect(unreduced.seconds).toBeGreaterThan(0.001);
    expect(unreduced.seconds).toBeGreaterThan(100 * reduced.seconds);
    expect(unreduced.hash?.behavior).toBe('smooth');
    expect(unreduced.quick?.behavior).toBe('smooth');
  });
});
