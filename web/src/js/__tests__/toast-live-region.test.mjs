// A11y audit #26-A3: toasts (settings success notices, X-Ovumcy-Notice
// errors, export feedback) used to be appended into a .toast-stack that was
// created lazily on the first showToast call and carried no live-region
// semantics — screen readers never heard them. The contract pinned here:
// the stack exists as an aria-live="polite" region as soon as the app
// initialises (a region inserted and populated in the same breath is
// skipped by screen readers), and toasts land inside that pre-existing
// region.

import test from "node:test";
import assert from "node:assert/strict";
import { readAppBundle, loadDOMWithScript } from "./_helpers.mjs";

const APP_BUNDLE = readAppBundle();

const PAGE = `<!doctype html><html><head></head><body data-toast-close="Close"></body></html>`;

test("toast stack exists as an aria-live polite region before any toast is shown", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: PAGE });
  try {
    const stack = dom.window.document.querySelector(".toast-stack");
    assert.ok(stack, "the toast stack is created eagerly at init, not on first toast");
    assert.equal(stack.getAttribute("aria-live"), "polite");
    assert.equal(stack.children.length, 0, "the eager stack starts empty");
  } finally {
    dom.window.close();
  }
});

test("showToast appends toasts into the pre-existing live region", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: PAGE });
  try {
    const { document } = dom.window;
    const stack = document.querySelector(".toast-stack");

    dom.window.showToast("Saved.", "ok");
    dom.window.showToast("Request failed.", "error");

    assert.equal(document.querySelectorAll(".toast-stack").length, 1, "no second stack is created");
    assert.equal(stack.children.length, 2, "both toasts land inside the live region");
    assert.ok(stack.textContent.includes("Saved."));
    assert.ok(stack.textContent.includes("Request failed."));
  } finally {
    dom.window.close();
  }
});
