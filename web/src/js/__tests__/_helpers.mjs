// Shared jsdom bootstrapping helpers for the Ovumcy client-side unit tests.
//
// The production JS is a series of concatenated IIFEs (see scripts/build-js.mjs),
// so its internal functions are closure-scoped and cannot be imported
// directly. The tests below load the bundle into a fresh jsdom window and
// exercise the public behaviour: HTMX events fired on document.body, DOM
// mutations on test fixtures, and side effects on `document.cookie` or
// `navigator.clipboard`. This way the tests cover the same code path that
// runs in the browser, with no need to modify production sources.

import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";
import { JSDOM, VirtualConsole } from "jsdom";

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, "..", "..", "..", "..");

// jsdom emulator limits we accept as environment noise rather than a test or
// product defect. Keyed on the EXACT message jsdom's virtual console reports
// (VirtualConsole "jsdomError" event) — never a regex or prefix match, so a
// message this suite has not seen before (potentially a real, newly-reachable
// "not implemented" surface) is never silently swallowed by a broad pattern.
// Add an entry here only with a one-line reason; everything else fails the
// test that produced it.
export const ALLOWED_JSDOM_ERROR_MESSAGES = new Map([
  [
    "Not implemented: HTMLFormElement's requestSubmit() method",
    "jsdom has no requestSubmit() implementation (tracked upstream, jsdom/jsdom#3542); " +
      "production's retry path calls it and jsdom's own fallback still dispatches the " +
      "submit event the suite asserts on, so this is an emulator gap, not a product defect.",
  ],
]);

// jsdom also reports a real thrown exception (from an event handler or a
// timer callback) as a "jsdomError" whose message is the wrapped exception —
// e.g. `Uncaught [Error: boom]`. That is a duplicate of the same failure the
// window "error" listener below already records with the original message
// and stack, so it is not collected a second time here.
const JSDOM_ERROR_ECHO_PREFIX = "Uncaught ";

// Exact-string match only — see the allowlist comment above. Exported so the
// harness self-test can pin the "one word off still fails" contract directly
// against the matcher, without needing to coax jsdom into emitting a second
// real "Not implemented" message.
export function isAllowedJsdomErrorMessage(message) {
  return ALLOWED_JSDOM_ERROR_MESSAGES.has(message);
}

export function readAppBundle() {
  return readFileSync(path.join(repoRoot, "web", "static", "js", "app.js"), "utf8");
}

export function readChartLite() {
  return readFileSync(path.join(repoRoot, "web", "static", "js", "chart-lite.js"), "utf8");
}

export function readTimezoneBootstrap() {
  return readFileSync(path.join(repoRoot, "web", "src", "js", "timezone-bootstrap.js"), "utf8");
}

// loadDOMWithScript spins up a fresh jsdom window, evaluates the supplied
// script source inside it, waits for `load` so any DOMContentLoaded
// listeners installed by the bundle have a chance to run, and returns the
// window for the test to interact with.
//
// Any error jsdom surfaces from inside the window — an exception thrown by
// an event handler or a timer callback, reported through the window "error"
// event and/or jsdom's virtual console — is recorded and, unless its exact
// message is allow-listed above, makes `dom.window.close()` throw and name
// it. Every existing test already calls `dom.window.close()` in a `finally`
// block, so this applies harness-wide with no per-test opt-in.
//
// An unhandled promise REJECTION inside jsdom-evaluated script is not routed
// through this mechanism: jsdom does not implement window "unhandledrejection"
// dispatch (nothing in jsdom/lib/jsdom/browser/Window.js wires it up), but the
// rejection still reaches Node's own realm — jsdom evaluates scripts in a real
// V8 context via `vm`, so an unhandled rejection there is an unhandled
// rejection as far as Node and `node --test` are concerned, and already fails
// the current (or, if the test already returned, the whole) test file without
// any help from this harness.
export async function loadDOMWithScript(scriptSource, { html, url, beforeRun } = {}) {
  const unhandled = [];

  const virtualConsole = new VirtualConsole();
  // Keep forwarding console.log/warn/error/etc. (including jsdomError) to the
  // real console so test output looks the same as before this change.
  virtualConsole.forwardTo(console);
  virtualConsole.on("jsdomError", (error) => {
    if (error.message.startsWith(JSDOM_ERROR_ECHO_PREFIX)) {
      return;
    }
    if (isAllowedJsdomErrorMessage(error.message)) {
      return;
    }
    unhandled.push({ source: "jsdom virtual console", message: error.message, stack: error.stack });
  });

  const dom = new JSDOM(html ?? "<!doctype html><html><head></head><body></body></html>", {
    url: url ?? "https://ovumcy.test/",
    runScripts: "outside-only",
    pretendToBeVisual: true,
    virtualConsole,
  });

  dom.window.addEventListener("error", (windowErrorEvent) => {
    const message =
      windowErrorEvent.message ||
      (windowErrorEvent.error && windowErrorEvent.error.message) ||
      "unknown window error";
    if (isAllowedJsdomErrorMessage(message)) {
      return;
    }
    unhandled.push({
      source: "window error event",
      message,
      stack: windowErrorEvent.error && windowErrorEvent.error.stack,
    });
  });

  if (typeof beforeRun === "function") {
    beforeRun(dom.window);
  }

  // Eval the script in the jsdom realm so it sees jsdom's document/window.
  dom.window.eval(scriptSource);

  // Fire DOMContentLoaded synchronously — many bundle pieces gate their
  // initialisation on it.
  const event = new dom.window.Event("DOMContentLoaded", { bubbles: true, cancelable: true });
  dom.window.document.dispatchEvent(event);

  const originalClose = dom.window.close.bind(dom.window);
  dom.window.close = function assertNoUnhandledErrorsThenClose() {
    originalClose();
    if (unhandled.length === 0) {
      return;
    }
    const summary = unhandled
      .map((entry, index) => `  ${index + 1}. [${entry.source}] ${entry.message}`)
      .join("\n");
    throw new Error(
      `loadDOMWithScript: ${unhandled.length} unhandled jsdom/window error(s) surfaced during this test:\n${summary}`
    );
  };

  return dom;
}
