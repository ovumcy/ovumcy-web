// A11y audit #26-A1: the confirm modal is a role="dialog" with aria-modal,
// but it used to leave keyboard focus free to Tab out into the page behind
// the backdrop and dropped the user's place in the document after closing.
// These tests pin the focus contract: opening moves focus into the dialog,
// Tab/Shift+Tab cycle between the two controls without escaping, and
// closing (accept, cancel, or Escape) returns focus to the element that
// had it before the dialog opened.

import test from "node:test";
import assert from "node:assert/strict";
import { readAppBundle, loadDOMWithScript } from "./_helpers.mjs";

const APP_BUNDLE = readAppBundle();

const PAGE = `<!doctype html><html><head></head><body
  data-confirm-cancel="No" data-confirm-delete="Yes">
  <button id="invoker" type="button">Delete thing</button>
  <div
    id="confirm-modal"
    class="confirm-modal-backdrop hidden"
    role="dialog"
    aria-modal="true"
    aria-hidden="true"
    aria-labelledby="confirm-modal-message">
    <div class="confirm-modal-center">
      <section class="journal-card confirm-modal-card">
        <p id="confirm-modal-message"></p>
        <div class="confirm-modal-actions">
          <button id="confirm-modal-cancel" type="button"></button>
          <button id="confirm-modal-accept" type="button"></button>
        </div>
      </section>
    </div>
  </div>
</body></html>`;

function pressTab(window, { shiftKey = false } = {}) {
  const event = new window.KeyboardEvent("keydown", {
    key: "Tab",
    shiftKey,
    bubbles: true,
    cancelable: true,
  });
  window.document.dispatchEvent(event);
  return event;
}

function pressEscape(window) {
  const event = new window.KeyboardEvent("keydown", {
    key: "Escape",
    bubbles: true,
    cancelable: true,
  });
  window.document.dispatchEvent(event);
  return event;
}

test("opening the confirm modal moves focus to the cancel button", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: PAGE });
  try {
    const { document } = dom.window;
    document.getElementById("invoker").focus();
    dom.window.__ovumcyOpenConfirm("Really?", "Yes");
    assert.equal(document.activeElement, document.getElementById("confirm-modal-cancel"));
  } finally {
    dom.window.close();
  }
});

test("Tab cycles between cancel and accept without leaving the dialog", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: PAGE });
  try {
    const { document } = dom.window;
    const cancel = document.getElementById("confirm-modal-cancel");
    const accept = document.getElementById("confirm-modal-accept");

    document.getElementById("invoker").focus();
    dom.window.__ovumcyOpenConfirm("Really?", "Yes");
    assert.equal(document.activeElement, cancel);

    // Tab from cancel: jsdom does not implement native tab navigation, so
    // the browser default is a no-op here; only the trap's edge handling is
    // observable. Tab from the LAST control must wrap to the first.
    accept.focus();
    let event = pressTab(dom.window);
    assert.equal(event.defaultPrevented, true, "Tab on the last control is intercepted");
    assert.equal(document.activeElement, cancel, "Tab wraps from accept back to cancel");

    // Shift+Tab from the FIRST control must wrap to the last.
    event = pressTab(dom.window, { shiftKey: true });
    assert.equal(event.defaultPrevented, true, "Shift+Tab on the first control is intercepted");
    assert.equal(document.activeElement, accept, "Shift+Tab wraps from cancel back to accept");

    // If focus somehow lands outside the dialog while it is open, the next
    // Tab press pulls it back inside instead of walking the page behind.
    document.getElementById("invoker").focus();
    event = pressTab(dom.window);
    assert.equal(event.defaultPrevented, true);
    assert.equal(document.activeElement, cancel, "Tab re-enters the dialog when focus escaped");
  } finally {
    dom.window.close();
  }
});

test("Tab is not intercepted while the modal is closed", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: PAGE });
  try {
    const { document } = dom.window;
    document.getElementById("invoker").focus();
    const event = pressTab(dom.window);
    assert.equal(event.defaultPrevented, false, "no focus trap without an open dialog");
    assert.equal(document.activeElement, document.getElementById("invoker"));
  } finally {
    dom.window.close();
  }
});

test("closing via Escape restores focus to the invoking element", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: PAGE });
  try {
    const { document } = dom.window;
    const invoker = document.getElementById("invoker");
    invoker.focus();

    let resolvedWith = null;
    dom.window.__ovumcyOpenConfirm("Really?", "Yes").then((accepted) => {
      resolvedWith = accepted;
    });
    assert.notEqual(document.activeElement, invoker, "focus moved into the dialog");

    pressEscape(dom.window);
    // Let the promise resolution microtask run.
    await new Promise((resolve) => setImmediate(resolve));

    assert.equal(resolvedWith, false, "Escape rejects the confirmation");
    assert.equal(document.activeElement, invoker, "focus returns to the element that opened the dialog");
    assert.equal(document.getElementById("confirm-modal").getAttribute("aria-hidden"), "true");
  } finally {
    dom.window.close();
  }
});

test("accepting restores focus to the invoking element", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: PAGE });
  try {
    const { document } = dom.window;
    const invoker = document.getElementById("invoker");
    invoker.focus();

    let resolvedWith = null;
    dom.window.__ovumcyOpenConfirm("Really?", "Yes").then((accepted) => {
      resolvedWith = accepted;
    });

    document.getElementById("confirm-modal-accept").click();
    await new Promise((resolve) => setImmediate(resolve));

    assert.equal(resolvedWith, true, "accept resolves the confirmation");
    assert.equal(document.activeElement, invoker, "focus returns to the element that opened the dialog");
  } finally {
    dom.window.close();
  }
});
