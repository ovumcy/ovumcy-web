import { test as base, expect } from '@playwright/test';
import type { BrowserContext } from '@playwright/test';

/**
 * The suite's own `test`, and the only one specs may import.
 *
 * Playwright's stock `test` reports a page that threw an uncaught exception as
 * a pass, as long as the locators the spec happened to assert still resolved:
 * a user-visible JavaScript crash coexists with green assertions and merges
 * unnoticed. Before this fixture existed the suite had exactly one error
 * handler — a `page.on('console')` inside a single register test — so every
 * new spec had to remember its own error plumbing and none of them did.
 *
 * The listeners bind to the **BrowserContext**, not to a page, so a page the
 * spec opens itself (`context.newPage()`) is covered without doing anything.
 * A spec that builds its own context (`browser.newContext()`) takes the
 * `browserErrors` fixture and calls `watch` on it — see
 * `auth-session-rotation.spec.ts`.
 *
 * Rendered-copy assertions still come from `locale-helpers.ts`; this file only
 * judges what the browser itself reports.
 */

/** A browser error that is expected, with the reason it is not a defect. */
interface AllowedBrowserError {
  readonly pattern: RegExp;
  /** Why this class of message is noise rather than a product fault. */
  readonly reason: string;
}

/**
 * Errors every test tolerates. Each entry carries its reason inline on
 * purpose: a bare regex list rots into a mute button the moment nobody
 * remembers what a pattern was covering. Anything not matched here fails the
 * test that produced it.
 */
const ALLOWED_EVERYWHERE: readonly AllowedBrowserError[] = [
  {
    pattern: /^console\.error: Failed to load resource: /,
    reason:
      'The engine logs every non-2xx or aborted response on the console, and this suite drives ' +
      'rejections through the UI constantly — a sweep of the whole suite recorded 363 of these ' +
      'for 403 alone, plus 400/401/500 and net::ERR_FAILED, all from tests whose subject IS the ' +
      'refusal. The status is asserted where it matters, so the line carries no information ' +
      'this suite does not already have; a script that then breaks on the response still ' +
      'reports its own uncaught error, which this fixture does not tolerate.',
  },
];

/** An engine's own error, with markup that still makes the engine emit it. */
interface EngineBrowserError extends AllowedBrowserError {
  /**
   * Body markup that reproduces the error under the app's CSP. The
   * cross-browser smoke serves it and fails once the engine stops emitting
   * the line, so the entry cannot outlive its reason.
   */
  readonly reproducer: string;
}

/** Errors one engine emits on pages the other engines render cleanly. */
export const ALLOWED_PER_BROWSER: Readonly<Record<string, readonly EngineBrowserError[]>> = {
  webkit: [
    {
      pattern:
        /^console\.error: Refused to apply a stylesheet because its hash, its nonce, or 'unsafe-inline' does not appear in the style-src directive of the Content Security Policy\.$/,
      reason:
        'WebKit 27.2 (Playwright 1.64) reports its own user-agent stylesheet for <select> as ' +
        "refused under style-src 'self': a page holding a <select> logs this line once per " +
        'load however many it holds, while an <input type=number> or plain markup logs ' +
        'nothing. The select renders identically with the CSP bypassed, the page carries no ' +
        'inline style, and the CSP stays as it is. Chromium runs the whole suite and reports ' +
        'an inline <style> or style attribute of ours in its own wording, which this pattern ' +
        'does not match.',
      reproducer: '<select aria-label="probe"><option>probe</option></select>',
    },
  ],
};

/** Collects what the browser reported, and judges it at the end of the test. */
export class BrowserErrorWatcher {
  private readonly observed: string[] = [];
  private readonly allowed: AllowedBrowserError[];

  constructor(browserName: string) {
    this.allowed = [...ALLOWED_EVERYWHERE, ...(ALLOWED_PER_BROWSER[browserName] ?? [])];
  }

  /**
   * Binds to a context the spec created itself. The fixture already watches
   * the built-in `context`, so this is only for `browser.newContext()`.
   */
  watch(context: BrowserContext): void {
    context.on('console', (message) => {
      if (message.type() === 'error') {
        this.observed.push(`console.error: ${message.text()}`);
      }
    });
    context.on('weberror', (webError) => {
      this.observed.push(`pageerror: ${webError.error().message || String(webError.error())}`);
    });
  }

  /**
   * Tolerates one class of error in this test only, for a stated reason. Use
   * it when the test's own subject is a page provoked into erroring; a
   * blanket suppression belongs in `ALLOWED_EVERYWHERE` with its reason, not
   * here.
   */
  allow(pattern: RegExp, reason: string): void {
    this.allowed.push({ pattern, reason });
  }

  /** How many recorded lines so far match `pattern`, allowed or not. */
  countReported(pattern: RegExp): number {
    return this.observed.filter((text) => pattern.test(text)).length;
  }

  /** Everything recorded that no allowlist entry covers. */
  unexpected(): string[] {
    return this.observed.filter((text) => !this.allowed.some(({ pattern }) => pattern.test(text)));
  }
}

export const test = base.extend<{ browserErrors: BrowserErrorWatcher }>({
  // `auto` so a spec inherits the guard without naming it: a fixture that has
  // to be opted into is the per-spec plumbing this replaces.
  browserErrors: [
    async ({ context, browserName }, use, testInfo) => {
      const watcher = new BrowserErrorWatcher(browserName);
      watcher.watch(context);

      await use(watcher);

      const unexpected = watcher.unexpected();
      if (unexpected.length === 0) {
        return;
      }

      const report = unexpected.map((text) => `  - ${text}`).join('\n');
      if (testInfo.status !== testInfo.expectedStatus) {
        // The test is already red for its own reason. Reporting these as the
        // failure would bury it, since a broken page tends to produce both.
        await testInfo.attach('browser-errors', { body: report, contentType: 'text/plain' });
        return;
      }

      throw new Error(
        `The browser reported ${unexpected.length} unexpected error(s) during this test; ` +
          'every assertion passed anyway, which is the false green this guard exists for. ' +
          'Fix the page, or allowlist the class with its reason in e2e/support/fixtures.ts.\n' +
          report
      );
    },
    { auto: true },
  ],
});

export { expect };
export type { Browser, BrowserContext, Frame, Locator, Page, Request } from '@playwright/test';
