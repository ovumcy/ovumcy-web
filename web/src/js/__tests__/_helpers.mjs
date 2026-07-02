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
import { JSDOM } from "jsdom";

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, "..", "..", "..", "..");

export function readAppBundle() {
  return readFileSync(path.join(repoRoot, "web", "static", "js", "app.js"), "utf8");
}

export function readTimezoneBootstrap() {
  return readFileSync(path.join(repoRoot, "web", "src", "js", "timezone-bootstrap.js"), "utf8");
}

// loadDOMWithScript spins up a fresh jsdom window, evaluates the supplied
// script source inside it, waits for `load` so any DOMContentLoaded
// listeners installed by the bundle have a chance to run, and returns the
// window for the test to interact with.
export async function loadDOMWithScript(scriptSource, { html, url, beforeRun } = {}) {
  const dom = new JSDOM(html ?? "<!doctype html><html><head></head><body></body></html>", {
    url: url ?? "https://ovumcy.test/",
    runScripts: "outside-only",
    pretendToBeVisual: true,
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

  return dom;
}
