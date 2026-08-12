// The dashboard journal has no save button: the page saves itself. That makes
// two properties load-bearing, and neither is observable from the markup alone.
//
// The safety rail first: a day nobody touched must produce NO request. The
// dashboard renders every control the owner tracks, most of them empty, and a
// save fired on page load would write those defaults back as observations
// about the day — "not a period day, no symptoms, mood unset" recorded as if
// the owner had said so. Only a control the owner actually changed may make the
// form dirty, and only a dirty form is ever sent.
//
// Then undo: one step back, held in memory on the form node and nowhere else.
// A day entry is health data — the snapshot is never written to localStorage,
// sessionStorage, or any other client store.

import test from "node:test";
import assert from "node:assert/strict";
import { readAppBundle, loadDOMWithScript } from "./_helpers.mjs";

const APP_BUNDLE = readAppBundle();

const TODAY = "2026-08-12";
const SAVED_LABEL = "Saved";
const UNDO_LABEL = "Undo";

function dashboardPage({ entryExists = false, notes = "" } = {}) {
  return `<!doctype html><html><head><meta name="csrf-token" content="unit-test-token"></head><body>
  <div data-dashboard-editor>
    <form
      hx-put="/api/v1/days/${TODAY}"
      hx-target="#save-status"
      hx-swap="innerHTML"
      data-save-feedback
      data-dashboard-save-form
      data-dashboard-date="${TODAY}"
      data-today-entry-exists="${entryExists ? "true" : "false"}"
      data-autosave-clear-url="/api/v1/days/${TODAY}?source=dashboard"
      data-autosave-saving="Saving..."
      data-autosave-saved="${SAVED_LABEL}"
      data-autosave-invalid="Fix the form errors to save"
      data-autosave-undo="${UNDO_LABEL}"
      data-day-save-failed-text="Couldn't save. Your entry is still here."
      data-day-save-retry-label="Try again">
      <input type="hidden" name="csrf_token" value="unit-test-token">
      <label class="period-toggle" data-binary-toggle data-active="false">
        <input type="checkbox" name="is_period" value="true" data-period-toggle data-binary-toggle-input>
      </label>
      <input type="radio" name="mood" value="4">
      <input type="checkbox" name="symptom_ids" value="7">
      <textarea id="today-notes" name="notes" data-dashboard-notes>${notes}</textarea>
      <div id="save-status" class="save-status" aria-live="polite"></div>
      <div class="dashboard-autosave-indicator" data-dashboard-autosave-indicator data-autosave-state="idle" aria-live="polite"></div>
    </form>
  </div>
</body></html>`;
}

/** Records every fetch the bundle makes and answers each one with `ok`. */
function installFetchRecorder(window, { ok = true } = {}) {
  const calls = [];
  window.fetch = (url, init) => {
    calls.push({ url: String(url), init: init || {} });
    return Promise.resolve({
      ok,
      status: ok ? 200 : 500,
      headers: { get: () => null },
      text: () => Promise.resolve(""),
    });
  };
  return calls;
}

async function loadDashboard(options = {}) {
  let calls = [];
  const dom = await loadDOMWithScript(APP_BUNDLE, {
    html: dashboardPage(options),
    beforeRun: (window) => {
      calls = installFetchRecorder(window, options);
    },
  });
  return { dom, calls: () => calls };
}

function form(window) {
  return window.document.querySelector("[data-dashboard-save-form]");
}

function indicator(window) {
  return window.document.querySelector("[data-dashboard-autosave-indicator]");
}

function undoButton(window) {
  return window.document.querySelector("[data-dashboard-autosave-undo]");
}

function fireChange(node) {
  node.dispatchEvent(new node.ownerDocument.defaultView.Event("change", { bubbles: true }));
}

// The keepalive flush the bundle installs for page unload runs the same runner
// the 2 s debounce does, so a test can reach the request without sitting out
// the debounce. What it cannot do is invent a request the runner would not
// make: an untouched form is still skipped, which is exactly the rail below.
function flushAutosave(window) {
  window.dispatchEvent(new window.Event("beforeunload"));
}

function settle() {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

function bodyOf(call) {
  return String(call.init.body || "");
}

// The runner serializes with URLSearchParams, so a space is "+", not "%20".
function formValue(name, value) {
  return new URLSearchParams([[name, value]]).toString();
}

test("an untouched dashboard sends nothing, even once the debounce window has passed", async () => {
  const { dom, calls } = await loadDashboard();
  try {
    // Real time, not a stubbed clock: the debounce is 2 s, so an autosave armed
    // at page load would have fired well inside this window.
    await new Promise((resolve) => setTimeout(resolve, 2400));
    flushAutosave(dom.window);
    await settle();

    assert.deepEqual(
      calls().map((call) => call.url),
      [],
      "a dashboard nobody touched must not write the day's default values back as observations"
    );
    assert.equal(form(dom.window).dataset.autosaveDirty, undefined);
    assert.equal(indicator(dom.window).getAttribute("data-autosave-state"), "idle");
    assert.equal(indicator(dom.window).textContent, "", "idle says nothing at all");
  } finally {
    dom.window.close();
  }
});

test("one changed control saves, and carries no value for the controls left alone", async () => {
  const { dom, calls } = await loadDashboard();
  try {
    const mood = dom.window.document.querySelector("input[name='mood'][value='4']");
    mood.checked = true;
    fireChange(mood);
    flushAutosave(dom.window);
    await settle();

    assert.equal(calls().length, 1, "one touched control, one save");
    const call = calls()[0];
    assert.equal(call.url, `/api/v1/days/${TODAY}`);
    assert.equal(call.init.method, "PUT");

    const body = bodyOf(call);
    assert.ok(body.includes("mood=4"), "the control the owner touched travels");
    assert.equal(
      body.includes("is_period"),
      false,
      "an untouched period toggle must not be recorded as an answer about the day"
    );
    assert.equal(body.includes("symptom_ids"), false, "no untouched symptom is recorded");
  } finally {
    dom.window.close();
  }
});

test("a saved day offers one step back, and taking it restores what the server held", async () => {
  const { dom, calls } = await loadDashboard({ entryExists: true, notes: "slept well" });
  try {
    const notes = dom.window.document.querySelector("#today-notes");
    notes.value = "slept badly";
    notes.dispatchEvent(new dom.window.Event("input", { bubbles: true }));
    flushAutosave(dom.window);
    await settle();

    assert.equal(calls().length, 1);
    assert.ok(bodyOf(calls()[0]).includes(formValue("notes", "slept badly")));

    assert.equal(indicator(dom.window).getAttribute("data-autosave-state"), "saved");
    assert.ok(
      indicator(dom.window).textContent.includes(SAVED_LABEL),
      "the saved state names itself"
    );

    const undo = undoButton(dom.window);
    assert.ok(undo, "a completed save offers the step back");
    assert.equal(undo.type, "button", "the undo must not submit the form it sits in");
    assert.equal(undo.textContent, UNDO_LABEL, "the control carries its own accessible name");
    assert.equal(undo.querySelectorAll("[aria-hidden='true']").length, 0);

    undo.dispatchEvent(new dom.window.MouseEvent("click", { bubbles: true, cancelable: true }));
    await settle();

    assert.equal(calls().length, 2, "the undo saves through the same path");
    const undoBody = bodyOf(calls()[1]);
    assert.ok(
      undoBody.includes(formValue("notes", "slept well")),
      "the undo sends the state the server held before the save"
    );
    assert.equal(
      undoBody.includes(formValue("notes", "slept badly")),
      false,
      "the undone value is gone from the wire"
    );
    assert.equal(notes.value, "slept well", "and from the screen");

    // Depth one: undoing an undo is not on offer.
    assert.equal(undoButton(dom.window), null);
  } finally {
    dom.window.close();
  }
});

test("undoing the first save of an empty day clears it instead of writing an empty entry", async () => {
  const { dom, calls } = await loadDashboard({ entryExists: false });
  try {
    const toggle = dom.window.document.querySelector("input[name='is_period']");
    toggle.checked = true;
    fireChange(toggle);
    flushAutosave(dom.window);
    await settle();

    assert.equal(calls().length, 1);
    undoButton(dom.window).dispatchEvent(
      new dom.window.MouseEvent("click", { bubbles: true, cancelable: true })
    );
    await settle();

    assert.equal(calls().length, 2);
    assert.equal(calls()[1].init.method, "DELETE", "an empty day is an absent entry, not an empty one");
    assert.equal(calls()[1].url, `/api/v1/days/${TODAY}?source=dashboard`);
    assert.equal(toggle.checked, false, "the screen goes back with it");
  } finally {
    dom.window.close();
  }
});

test("the undo snapshot lives in memory only — never in client storage", async () => {
  // The hard boundary, restated for the dashboard: a day entry is health data,
  // and nothing about it may outlive the page in a client store.
  const note = "cramps since the afternoon";
  const { dom } = await loadDashboard({ entryExists: true, notes: note });
  try {
    const notes = dom.window.document.querySelector("#today-notes");
    notes.value = "quiet day";
    notes.dispatchEvent(new dom.window.Event("input", { bubbles: true }));
    flushAutosave(dom.window);
    await settle();

    assert.ok(undoButton(dom.window), "the snapshot exists");
    for (const storage of [dom.window.localStorage, dom.window.sessionStorage]) {
      for (let index = 0; index < storage.length; index += 1) {
        const key = storage.key(index);
        const value = String(storage.getItem(key));
        assert.equal(
          value.includes(note) || value.includes("quiet day"),
          false,
          `no day-entry text may be persisted client-side (found under "${key}")`
        );
      }
    }
  } finally {
    dom.window.close();
  }
});
