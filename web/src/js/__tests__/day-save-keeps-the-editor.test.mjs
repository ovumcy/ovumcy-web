// A saved calendar day leaves the open editor alone: the editor already shows
// what the owner saved, and a reload would discard anything typed while the
// save ran. The editor is never re-fetched after a save, whatever method or
// path the day form submits with.
//
// The month grid refresh is the save response's own HX-Trigger header, which
// htmx fires on the form and which bubbles to the body. A client-side trigger
// beside it made every save fetch the grid twice.

import test from "node:test";
import assert from "node:assert/strict";
import { readAppBundle, loadDOMWithScript } from "./_helpers.mjs";

const APP_BUNDLE = readAppBundle();
const DAY = "2026-08-12";
const SAVED_FRAGMENT = '<div class="status-ok">Day saved.</div>';

function calendarPage({ method, path }) {
  return `<!doctype html><html><head><meta name="csrf-token" content="unit-test-token"></head><body>
  <div id="day-editor">
    <form ${method}="${path}" hx-target="#calendar-save-status" data-save-feedback data-day-editor-form>
      <input type="hidden" name="csrf_token" value="unit-test-token">
      <textarea name="notes">typed while the save ran</textarea>
      <div id="calendar-save-status" class="save-status" aria-live="polite"></div>
    </form>
  </div></body></html>`;
}

// A recording stand-in for htmx: the unit harness loads no htmx, so the bundle's
// calls to it are what the test observes.
function installHtmxRecorder(window, calls) {
  window.htmx = {
    trigger: (element, name) => calls.push({ kind: "trigger", element, name }),
    ajax: (verb, url, options) => calls.push({ kind: "ajax", verb, url, options }),
    process: () => {},
  };
}

async function saveDay(form) {
  const dom = await loadDOMWithScript(APP_BUNDLE, {
    html: calendarPage(form),
    beforeRun: (window) => installHtmxRecorder(window, form.calls),
  });
  const { window } = dom;
  const formNode = window.document.querySelector("[data-day-editor-form]");
  const region = window.document.getElementById("calendar-save-status");
  region.innerHTML = SAVED_FRAGMENT;
  formNode.dispatchEvent(
    new window.CustomEvent("htmx:afterSwap", {
      detail: { target: region, xhr: { status: 200, responseText: SAVED_FRAGMENT } },
      bubbles: true,
    }),
  );
  return { dom, window };
}

const FORMS = [
  { label: "the shipped hx-put on the v1 path", method: "hx-put", path: `/api/v1/days/${DAY}` },
  { label: "an hx-post on the v1 path", method: "hx-post", path: `/api/v1/days/${DAY}` },
];

for (const shape of FORMS) {
  test(`a saved day leaves the grid refresh to the response and does not re-fetch the editor (${shape.label})`, async () => {
    const calls = [];
    const { dom, window } = await saveDay({ ...shape, calls });
    try {
      assert.deepEqual(
        calls.filter((call) => call.kind === "trigger"),
        [],
        "the client fires no grid refresh of its own",
      );

      assert.deepEqual(
        calls.filter((call) => call.kind === "ajax"),
        [],
        "no request re-fetches the editor after a save",
      );
      assert.equal(
        window.document.querySelector("[data-day-editor-form] textarea").value,
        "typed while the save ran",
        "the editor keeps what the owner typed",
      );
    } finally {
      dom.window.close();
    }
  });
}
