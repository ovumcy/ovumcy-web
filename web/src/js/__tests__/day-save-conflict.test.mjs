// A day save states only the fields the owner changed, each beside the value
// the edit was based on, so a change made on another device to another field
// survives the save. When the other device changed the SAME field the server
// answers 409, and the owner chooses whose value the day keeps.

import test from "node:test";
import assert from "node:assert/strict";
import { readAppBundle, loadDOMWithScript } from "./_helpers.mjs";

const APP_BUNDLE = readAppBundle();
const TODAY = "2026-08-12";
const ACCOUNT_BINDING = "unit-test-account-binding";
const CONFLICT_KEY = "daylog.save_conflict";
const CONFLICT_TEXT = "Ce jour a été modifié sur un autre appareil.";
const KEEP_LABEL = "Garder la mienne";
const TAKE_LABEL = "Prendre l'autre";

function journalForm({ period = true, mood = "", notes = "", entryExists = true, fromStoredStart = false } = {}) {
  return `<form
      hx-put="/api/v1/days/${TODAY}"
      data-save-feedback
      data-dashboard-save-form
      data-today-entry-exists="${entryExists}"
      data-today-period-from-stored-start="${fromStoredStart}"
      data-autosave-saved="Saved"
      data-day-save-retry-label="Try again"
      data-day-conflict-refresh-url="/dashboard"
      data-day-conflict-keep-label="${KEEP_LABEL}"
      data-day-conflict-take-label="${TAKE_LABEL}">
      <input type="hidden" name="csrf_token" value="unit-test-token">
      <input type="hidden" name="_method" value="PUT">
      <input type="hidden" name="day_form_account" value="${ACCOUNT_BINDING}">
      <input type="checkbox" name="is_period" value="true" data-period-toggle ${period ? "checked" : ""}>
      <input type="radio" name="mood" value="3" ${mood === "3" ? "checked" : ""}>
      <input type="radio" name="mood" value="4" ${mood === "4" ? "checked" : ""}>
      <textarea id="today-notes" name="notes" data-dashboard-notes>${notes}</textarea>
      <div id="save-status" class="save-status" aria-live="polite"></div>
      <div data-dashboard-autosave-indicator data-autosave-state="idle"></div>
    </form>`;
}

function page(formOptions) {
  return `<!doctype html><html><head><meta name="csrf-token" content="unit-test-token"></head><body>
  <div data-dashboard-editor>${journalForm(formOptions)}</div></body></html>`;
}

const CONFLICT_FRAGMENT = `<div class="status-error" data-flash-key="${CONFLICT_KEY}" data-flash-status="error">${CONFLICT_TEXT}</div>`;

// PATCH answers come off `patchAnswers` in order; a GET answers the page the
// server would render now.
async function loadJournal({ formOptions, patchAnswers = [], freshPage = "" } = {}) {
  const calls = [];
  const dom = await loadDOMWithScript(APP_BUNDLE, {
    html: page(formOptions),
    beforeRun: (window) => {
      window.fetch = (url, init = {}) => {
        calls.push({ url: String(url), init });
        if ((init.method || "GET") === "GET") {
          return Promise.resolve({ ok: true, status: 200, headers: { get: () => null }, text: () => Promise.resolve(freshPage) });
        }
        const answer = patchAnswers.shift() || { status: 200, text: "" };
        return Promise.resolve({
          ok: answer.status < 300,
          status: answer.status,
          headers: { get: () => null },
          text: () => Promise.resolve(answer.text),
        });
      };
    },
  });
  return { dom, calls };
}

function settle() {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

function patches(calls) {
  return calls.filter((call) => call.init.method === "PATCH").map((call) => new URLSearchParams(String(call.init.body)));
}

async function editNotes(window, value) {
  const notes = window.document.querySelector("#today-notes");
  notes.value = value;
  notes.dispatchEvent(new window.Event("input", { bubbles: true }));
  window.dispatchEvent(new window.Event("pagehide"));
  await settle();
}

test("a save names only the changed fields, each with its base, and turns the period off explicitly", async () => {
  const { dom, calls } = await loadJournal({ formOptions: { period: true, mood: "3", notes: "kept elsewhere" } });
  try {
    const toggle = dom.window.document.querySelector("input[name='is_period']");
    toggle.checked = false;
    toggle.dispatchEvent(new dom.window.Event("change", { bubbles: true }));
    dom.window.dispatchEvent(new dom.window.Event("pagehide"));
    await settle();

    const [body] = patches(calls);
    assert.ok(body, "the change is saved as a partial write");
    assert.deepEqual(body.getAll("is_period"), ["false"], "an unchecked period is sent as an explicit false");
    assert.deepEqual(body.getAll("base_is_period"), ["true"], "beside the value the page was rendered with");
    for (const untouched of ["mood", "notes", "base_mood", "base_notes", "_method"]) {
      assert.equal(body.has(untouched), false, `${untouched} was not changed and must not be written`);
    }
    assert.equal(body.get("day_form_account"), ACCOUNT_BINDING, "the account binding rides on every save");
  } finally {
    dom.window.close();
  }
});

test("a 409 offers keep-mine and take-theirs instead of a retry", async () => {
  const { dom } = await loadJournal({
    formOptions: { notes: "rendered" },
    patchAnswers: [{ status: 409, text: CONFLICT_FRAGMENT }],
  });
  try {
    await editNotes(dom.window, "mine");
    const notice = dom.window.document.querySelector("#save-status [data-day-save-conflict]");
    assert.ok(notice, "the conflict renders its own notice");
    assert.equal(notice.querySelector("[data-notice-key]").getAttribute("data-notice-key"), CONFLICT_KEY);
    assert.equal(notice.querySelector("[data-day-conflict-choice='mine']").textContent, KEEP_LABEL);
    assert.equal(notice.querySelector("[data-day-conflict-choice='theirs']").textContent, TAKE_LABEL);
    assert.equal(notice.querySelector("[data-day-save-retry]"), null, "a plain retry would be refused again");
    assert.equal(dom.window.document.querySelector("#today-notes").value, "mine", "the entry stays in the form");
  } finally {
    dom.window.close();
  }
});

function freshDashboard(formOptions) {
  return `<!doctype html><html><body><div data-dashboard-editor>${journalForm(formOptions)}</div></body></html>`;
}

test("keep mine re-sends the owner's value against the stored day, and loads what else changed", async () => {
  const { dom, calls } = await loadJournal({
    formOptions: { notes: "rendered" },
    patchAnswers: [{ status: 409, text: CONFLICT_FRAGMENT }, { status: 200, text: "" }],
    freshPage: freshDashboard({ mood: "4", notes: "remote" }),
  });
  try {
    await editNotes(dom.window, "mine");
    dom.window.document
      .querySelector("[data-day-conflict-choice='mine']")
      .dispatchEvent(new dom.window.MouseEvent("click", { bubbles: true, cancelable: true }));
    await settle();
    await settle();

    const resent = patches(calls).at(-1);
    assert.deepEqual(resent.getAll("notes"), ["mine"], "the owner's value wins");
    assert.deepEqual(resent.getAll("base_notes"), ["remote"], "against the value now stored");
    assert.equal(resent.has("mood"), false, "a field only the other device changed is not written back");
    assert.equal(dom.window.document.querySelector("input[name='mood'][value='4']").checked, true, "it is loaded into the form");
    assert.equal(dom.window.document.querySelector("#today-notes").value, "mine");
  } finally {
    dom.window.close();
  }
});

test("take theirs loads the stored value and sends nothing more", async () => {
  const { dom, calls } = await loadJournal({
    formOptions: { notes: "rendered" },
    patchAnswers: [{ status: 409, text: CONFLICT_FRAGMENT }],
    freshPage: freshDashboard({ notes: "remote" }),
  });
  try {
    await editNotes(dom.window, "mine");
    dom.window.document
      .querySelector("[data-day-conflict-choice='theirs']")
      .dispatchEvent(new dom.window.MouseEvent("click", { bubbles: true, cancelable: true }));
    await settle();
    await settle();

    assert.equal(dom.window.document.querySelector("#today-notes").value, "remote");
    assert.equal(patches(calls).length, 1, "nothing is left to save");
    assert.equal(dom.window.document.querySelector("[data-day-save-conflict]"), null, "the notice is answered");
    assert.equal(dom.window.document.querySelector("[data-dashboard-save-form]").dataset.autosaveDirty, undefined);
  } finally {
    dom.window.close();
  }
});

// The stored day the answer reads may be a row-less stored onboarding start,
// rendered ticked. It is not an empty day: an undo back to it must never
// DELETE the day, which would withdraw the start.
test("a stored start read back after a conflict is not taken for an empty day", async () => {
  const { dom } = await loadJournal({
    formOptions: { notes: "rendered" },
    patchAnswers: [{ status: 409, text: CONFLICT_FRAGMENT }],
    freshPage: freshDashboard({ entryExists: false, fromStoredStart: true }),
  });
  try {
    await editNotes(dom.window, "mine");
    dom.window.document
      .querySelector("[data-day-conflict-choice='theirs']")
      .dispatchEvent(new dom.window.MouseEvent("click", { bubbles: true, cancelable: true }));
    await settle();
    await settle();

    const form = dom.window.document.querySelector("[data-dashboard-save-form]");
    assert.equal(form.__ovumcyPersistedState.empty, false);
  } finally {
    dom.window.close();
  }
});

test("an htmx day save goes out as the partial write", async () => {
  const { dom } = await loadJournal({ formOptions: { notes: "rendered" } });
  try {
    const form = dom.window.document.querySelector("[data-dashboard-save-form]");
    form.querySelector("#today-notes").value = "typed";
    const detail = { elt: form, verb: "put", formData: new dom.window.FormData(form), parameters: {}, headers: {} };
    form.dispatchEvent(new dom.window.CustomEvent("htmx:configRequest", { bubbles: true, detail }));

    assert.equal(detail.verb, "patch");
    const body = new URLSearchParams(detail.formData);
    assert.deepEqual(body.getAll("notes"), ["typed"]);
    assert.deepEqual(body.getAll("base_notes"), ["rendered"]);
    assert.equal(body.has("is_period"), false, "the untouched period is not restated");
    assert.equal(body.has("_method"), false);
    assert.deepEqual(detail.parameters.notes, "typed", "htmx builds its body from the parameters");
  } finally {
    dom.window.close();
  }
});
