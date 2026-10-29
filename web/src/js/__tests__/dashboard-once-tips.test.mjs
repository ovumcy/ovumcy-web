// A once-tip mark names the account the page was rendered for: localStorage
// outlives a sign-out, and an unkeyed mark hid the first-run tips from every
// later account on the same browser.

import test from "node:test";
import assert from "node:assert/strict";
import { readAppBundle, loadDOMWithScript } from "./_helpers.mjs";

const APP_BUNDLE = readAppBundle();

function page(binding) {
  const attr = binding === null ? "" : ` data-account-binding="${binding}"`;
  return `<!doctype html><html><head><meta name="csrf-token" content="t"></head><body${attr}>
  <div data-dashboard-editor>
    <form data-dashboard-save-form data-dashboard-date="2026-08-12">
      <input type="hidden" name="day_form_account" value="${binding || ""}">
      <p data-once-tip="dashboard-first-tour" hidden>tip</p>
    </form>
  </div>
</body></html>`;
}

async function load(binding, stored) {
  return loadDOMWithScript(APP_BUNDLE, {
    html: page(binding),
    url: "https://ovumcy.test/dashboard",
    beforeRun: (window) => {
      window.fetch = () => Promise.resolve({ ok: true, status: 200, headers: { get: () => null }, text: () => Promise.resolve("") });
      for (const [key, value] of stored) {
        window.localStorage.setItem(key, value);
      }
    },
  });
}

function snapshot(dom) {
  const out = [];
  for (let i = 0; i < dom.window.localStorage.length; i++) {
    const key = dom.window.localStorage.key(i);
    out.push([key, dom.window.localStorage.getItem(key)]);
  }
  return out;
}

const tipHidden = (dom) => dom.window.document.querySelector("[data-once-tip]").hasAttribute("hidden");

test("a second account on the same browser still sees the once-tip", async () => {
  const first = await load("acct-a", []);
  assert.equal(tipHidden(first), false);
  const marks = snapshot(first);
  assert.ok(marks.length > 0);

  const again = await load("acct-a", marks);
  assert.equal(tipHidden(again), true, "the same account sees it once");

  const second = await load("acct-b", marks);
  assert.equal(tipHidden(second), false, "another account is not hidden by the first one's mark");
});

test("a page without a binding keeps the unkeyed mark", async () => {
  const first = await load(null, []);
  assert.deepEqual(snapshot(first), [["ovumcy_once_tip:dashboard-first-tour", "1"]]);
});
