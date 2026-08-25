// The dashboard journal is autosave-only, so the only thing a manual htmx save
// finalizer has to do is stand down: stop the pending timers, drop the dirty
// flag, and leave the indicator idle. That outcome does not depend on whether
// the request succeeded — the failure notice is rendered by the save-status
// swap, not by the indicator row — which is why the finalizer's two arms were
// byte-identical and one of them was dead.
//
// Collapsing them changes no behaviour, and this pin is what says so: the same
// end state must hold for a successful request and for a failed one. It is an
// equivalence pin, not a defect detector — the rule that detects identical
// branches (sonarjs/no-identical-branches) is not enabled here and adding a
// lint dependency for one call site is not worth it.

import test from "node:test";
import assert from "node:assert/strict";
import { readAppBundle, loadDOMWithScript } from "./_helpers.mjs";

const APP_BUNDLE = readAppBundle();
const TODAY = "2026-08-25";

function dashboardPage() {
  return `<!doctype html><html><head><meta name="csrf-token" content="unit-test-token"></head><body>
  <div data-dashboard-editor>
    <form
      hx-put="/api/v1/days/${TODAY}"
      hx-target="#save-status"
      hx-swap="innerHTML"
      data-save-feedback
      data-dashboard-save-form
      data-dashboard-date="${TODAY}"
      data-today-entry-exists="true"
      data-autosave-saving="Saving..."
      data-autosave-saved="Saved">
      <input type="hidden" name="csrf_token" value="unit-test-token">
      <input type="radio" name="mood" value="4">
      <div id="save-status" class="save-status" aria-live="polite"></div>
      <div class="dashboard-autosave-indicator" data-dashboard-autosave-indicator data-autosave-state="saving" aria-live="polite"></div>
    </form>
  </div>
</body></html>`;
}

async function finalizeManualSave(successful) {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: dashboardPage() });
  const { document } = dom.window;
  const form = document.querySelector("[data-dashboard-save-form]");
  form.dataset.autosaveDirty = "true";

  form.dispatchEvent(
    new dom.window.CustomEvent("htmx:afterRequest", {
      bubbles: true,
      detail: { elt: form, successful: successful, xhr: null },
    }),
  );

  return {
    dirty: form.dataset.autosaveDirty,
    indicator: document
      .querySelector("[data-dashboard-autosave-indicator]")
      .getAttribute("data-autosave-state"),
  };
}

test("a successful manual save leaves the journal clean and the indicator idle", async () => {
  const state = await finalizeManualSave(true);
  assert.equal(state.dirty, undefined);
  assert.equal(state.indicator, "idle");
});

test("a failed manual save leaves the journal clean and the indicator idle", async () => {
  const state = await finalizeManualSave(false);
  assert.equal(state.dirty, undefined);
  assert.equal(
    state.indicator,
    "idle",
    "the indicator row reports save state, not save outcome: the failure notice is the save-status swap's job",
  );
});
