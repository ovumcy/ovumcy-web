// WEB-244: the recovery-code reveal is one-time on the server, so a reload or
// a navigation away before the owner acknowledges the code loses it. The
// confirm form's binder arms a beforeunload guard that asks the browser for
// its leave confirmation until the acknowledged submit, which disarms it
// before navigating on.

import test from "node:test";
import assert from "node:assert/strict";
import { readAppBundle, loadDOMWithScript } from "./_helpers.mjs";

const APP_BUNDLE = readAppBundle();

const REVEAL_PAGE = `<!doctype html><html><head></head><body>
  <div data-recovery-code-tools>
    <span data-recovery-code-value>OVUM-AAAA-BBBB-CCCC</span>
    <form action="/dashboard" method="get" data-recovery-code-confirm data-recovery-continue-target="dashboard" data-recovery-required-message="Check this box to continue.">
      <input id="recovery-code-saved" type="checkbox" required data-recovery-code-checkbox>
      <div data-recovery-code-status></div>
      <button type="submit" data-recovery-code-submit aria-disabled="true">Continue</button>
    </form>
  </div>
</body></html>`;

const PLAIN_PAGE = `<!doctype html><html><head></head><body><p>nothing to guard</p></body></html>`;

function fireBeforeUnload(window) {
  const event = new window.Event("beforeunload", { cancelable: true });
  window.dispatchEvent(event);
  return event;
}

// jsdom cannot navigate; it reports the attempt on its virtual console. The
// recorder captures that report as the navigation it stands for, and only
// that one — every other jsdom report still fails the test through the
// shared harness.
const NAVIGATION_REPORT = "Not implemented: navigation to another Document";

function installNavigationRecorder(window, navigations) {
  const virtualConsole = window._settings && window._settings.virtualConsole;
  assert.ok(virtualConsole, "jsdom's virtual console is where a navigation is reported");
  const forward = virtualConsole.emit.bind(virtualConsole);
  virtualConsole.emit = (type, error, ...rest) => {
    if (type === "jsdomError" && error && error.message === NAVIGATION_REPORT) {
      navigations.push(String(window.location.href));
      return true;
    }
    return forward(type, error, ...rest);
  };
}

test("leaving the reveal page before acknowledging asks for confirmation", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: REVEAL_PAGE });
  try {
    const event = fireBeforeUnload(dom.window);
    assert.equal(event.defaultPrevented, true, "beforeunload should be cancelled while the code is unacknowledged");
  } finally {
    dom.window.close();
  }
});

test("the acknowledged submit disarms the guard before navigating on", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: REVEAL_PAGE });
  try {
    const { document } = dom.window;
    const navigations = [];
    installNavigationRecorder(dom.window, navigations);

    document.getElementById("recovery-code-saved").checked = true;
    const form = document.querySelector("[data-recovery-code-confirm]");
    form.dispatchEvent(new dom.window.Event("submit", { bubbles: true, cancelable: true }));
    assert.equal(navigations.length, 1, "the acknowledged submit navigates to the continue target");

    const event = fireBeforeUnload(dom.window);
    assert.equal(event.defaultPrevented, false, "beforeunload must pass once the code is acknowledged");
  } finally {
    dom.window.close();
  }
});

test("a refused submit keeps the guard armed", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: REVEAL_PAGE });
  try {
    const { document } = dom.window;
    const form = document.querySelector("[data-recovery-code-confirm]");
    form.dispatchEvent(new dom.window.Event("submit", { bubbles: true, cancelable: true }));
    assert.equal(fireBeforeUnload(dom.window).defaultPrevented, true);
  } finally {
    dom.window.close();
  }
});

test("a page without a reveal form arms no guard", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: PLAIN_PAGE });
  try {
    assert.equal(fireBeforeUnload(dom.window).defaultPrevented, false);
  } finally {
    dom.window.close();
  }
});
