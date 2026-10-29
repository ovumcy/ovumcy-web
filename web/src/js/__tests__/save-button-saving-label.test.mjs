// While a save is in flight the button says so: the busy state used to be only
// `disabled` + `aria-busy`, and the `data-saving-label` the templates declare
// was never read, leaving the button reading "Update entry" with an empty
// status row.

import test from "node:test";
import assert from "node:assert/strict";
import { readAppBundle, loadDOMWithScript } from "./_helpers.mjs";

const APP_BUNDLE = readAppBundle();

const PAGE = `<!doctype html><html><head></head><body>
  <form hx-put="/api/v1/days/2026-08-11" data-save-feedback>
    <button type="submit" data-save-button data-saving-label="Saving...">Update entry</button>
    <div id="status" class="save-status" aria-live="polite"></div>
  </form>
</body></html>`;

function fire(window, form, name, detail) {
  form.dispatchEvent(new window.CustomEvent(name, { detail, bubbles: true }));
}

test("the save button shows its saving label while the request runs and restores its text after", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: PAGE });
  const { window } = dom;
  const form = window.document.querySelector("form");
  const button = form.querySelector("[data-save-button]");

  fire(window, form, "htmx:beforeRequest", { target: form });
  assert.equal(button.disabled, true);
  assert.equal(button.textContent, "Saving...");

  // A second busy call (a retry racing the first) must not record the saving
  // label as the text to restore.
  fire(window, form, "htmx:beforeRequest", { target: form });
  fire(window, form, "htmx:afterRequest", { target: form, successful: true, xhr: null });
  assert.equal(button.disabled, false);
  assert.equal(button.textContent, "Update entry");
});
