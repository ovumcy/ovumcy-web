// A form the browser refuses to submit (native validation) sends no request, so
// nothing replaces the status row: the "Saved." of the previous save used to sit
// beside the validation bubble. The refusal takes the earlier success down.

import test from "node:test";
import assert from "node:assert/strict";
import { readAppBundle, loadDOMWithScript } from "./_helpers.mjs";

const APP_BUNDLE = readAppBundle();

const PAGE = `<!doctype html><html><head></head><body>
  <form hx-put="/api/v1/users/current/cycle" hx-target="#cycle-status" data-save-feedback>
    <input id="cycle-length" name="cycle_length" type="number" min="15" max="90" required>
    <button type="submit" data-save-button>Save</button>
    <div id="cycle-status" class="save-status" aria-live="polite"><div class="status-ok">Saved.</div></div>
  </form>
  <div id="elsewhere" class="save-status"><div class="status-ok">Other card saved.</div></div>
</body></html>`;

test("a native validation refusal withdraws the previous success from the form's status row only", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: PAGE });
  const { window } = dom;
  const input = window.document.querySelector("#cycle-length");

  // `invalid` does not bubble; the page listens in the capture phase.
  input.dispatchEvent(new window.Event("invalid", { bubbles: false, cancelable: true }));

  assert.equal(window.document.querySelector("#cycle-status .status-ok"), null, "the stale Saved. is withdrawn");
  assert.ok(window.document.querySelector("#elsewhere .status-ok"), "another card's status is untouched");
});
