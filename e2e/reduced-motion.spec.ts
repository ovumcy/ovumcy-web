import { expect, test, type Locator, type Page } from './support/fixtures';
import { registerAndOnboardWithStartDaysAgo } from './support/stats-helpers';

// WEB-492. The preference for reduced motion is honoured in three places: the
// CSS motion scale (transition durations collapse), the auth-panel fade
// (never armed), and the dashboard's programmatic scrolls (an instant jump
// instead of a smooth one). The unit suite pins the scripts against a stubbed matchMedia;
// here the real browser's emulated preference drives all three, and each
// observation is repeated under "no-preference" so the preference, not the
// page, is what makes the difference.

type ScrollArg = { block?: string; behavior?: string } | null;

// app.js is a deferred script, so the page is visible before its handlers are
// bound. Every bind step runs in one synchronous ready callback, and the hash
// disclosure bind comes after the auth-panel fade and before the quick-action
// bind, so this attribute on <body> means all of them have run. A negative
// assertion (a class never added, a scroll never smooth) is read only after it.
async function waitForAppReady(page: Page): Promise<void> {
  await expect(page.locator('body')).toHaveAttribute('data-hash-disclosure-bound', '1');
}

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
    await waitForAppReady(page);
    await expect(panel).not.toHaveClass(/auth-panel-transition/);
    // Force the class the script withheld, so the duration read is the CSS
    // scale's own answer and not the absence of a transition.
    await panel.evaluate((node) => node.classList.add('auth-panel-transition'));
    const reduced = await maxTransitionSeconds(panel);
    expect(reduced).toBeLessThan(0.001);

    await page.emulateMedia({ reducedMotion: 'no-preference' });
    await page.goto('/login');
    await expect(panel).toBeVisible();
    await waitForAppReady(page);
    await expect(panel).toHaveClass(/auth-panel-transition/);
    const unreduced = await maxTransitionSeconds(panel);
    expect(unreduced).toBeGreaterThanOrEqual(0.1);
    expect(unreduced).toBeGreaterThan(100 * reduced);
  });

  test('dashboard motion scale, hash-target scroll and quick-action scroll honour the preference', async ({ page }) => {
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
      await waitForAppReady(page);
      const seconds = await maxTransitionSeconds(button);
      await button.click();
      const calls = await page.evaluate(
        () => (window as unknown as { __scrollCalls: ScrollArg[] }).__scrollCalls
      );
      // Exactly these two: an extra call must not be able to stand in for one.
      expect(calls, 'one scroll for the hash target, one for the quick action').toHaveLength(2);
      const rootScrollBehavior = await page.evaluate(
        () => getComputedStyle(document.documentElement).scrollBehavior
      );
      const hash = calls.find((arg) => arg?.block === 'start');
      const quick = calls.find((arg) => arg?.block === 'center');
      expect(hash, 'the hash target is scrolled into view').toBeTruthy();
      expect(quick, 'the quick action scrolls its section into view').toBeTruthy();
      return { seconds, hash, quick, rootScrollBehavior };
    };

    const reduced = await observe('reduce');
    expect(reduced.seconds).toBeLessThan(0.001);
    expect(reduced.rootScrollBehavior).not.toBe('smooth');
    expect(reduced.hash?.behavior).not.toBe('smooth');
    expect(reduced.quick?.behavior).not.toBe('smooth');

    const unreduced = await observe('no-preference');
    expect(unreduced.seconds).toBeGreaterThan(0.001);
    expect(unreduced.seconds).toBeGreaterThan(100 * reduced.seconds);
    expect(unreduced.hash?.behavior).toBe('smooth');
    expect(unreduced.quick?.behavior).toBe('smooth');
  });
});
