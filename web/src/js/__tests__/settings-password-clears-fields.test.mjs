// A password form that succeeded must not leave the typed passwords in the page.
// The server marks such an answer with data-status-kind="clears-fields"; the
// bundle empties the form that owns the island and fires an input event on each
// visible field, so a checklist or mismatch hint bound to them reads the empty
// value. These tests pin that against the shipped bundle: the marked success
// clears, an unmarked success leaves the fields alone, and an island that is not
// inside a form changes nothing.

import test from "node:test";
import assert from "node:assert/strict";
import { readAppBundle, loadDOMWithScript } from "./_helpers.mjs";

const APP_BUNDLE = readAppBundle();

const PAGE = `<!doctype html><html><head></head><body>
  <form id="settings-change-password-form" hx-post="/api/v1/users/current/password">
    <input type="password" id="cp-current" name="current_password" value="">
    <input type="password" id="cp-new" name="new_password" value="">
    <input type="password" id="cp-confirm" name="confirm_password" value="">
    <div id="settings-change-password-status" class="save-status" aria-live="polite"></div>
  </form>
  <input type="password" id="loose-field" value="">
  <div id="loose-status" class="save-status" aria-live="polite"></div>
</body></html>`;

const FIELD_IDS = ["cp-current", "cp-new", "cp-confirm"];

function watchInputEvents(window) {
  const counts = {};
  for (const id of [...FIELD_IDS, "loose-field"]) {
    counts[id] = 0;
    window.document.getElementById(id).addEventListener("input", () => {
      counts[id] += 1;
    });
  }
  return counts;
}

function typePasswords(window) {
  for (const id of [...FIELD_IDS, "loose-field"]) {
    window.document.getElementById(id).value = "Typed-Secret-1";
  }
}

function swapSuccess(window, islandId, kind) {
  const island = window.document.getElementById(islandId);
  const kindAttr = kind ? ` data-status-kind="${kind}"` : "";
  island.innerHTML = `<div class="status-ok"${kindAttr} data-flash-key="settings.success.password_changed">Saved.</div>`;
  island.dispatchEvent(new window.CustomEvent("htmx:afterSwap", { bubbles: true, detail: { target: island } }));
}

test("a success that clears-fields empties the form's password inputs and fires input on each", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: PAGE });
  try {
    typePasswords(dom.window);
    const counts = watchInputEvents(dom.window);

    swapSuccess(dom.window, "settings-change-password-status", "clears-fields");

    for (const id of FIELD_IDS) {
      assert.equal(dom.window.document.getElementById(id).value, "", `${id} must be emptied`);
      assert.ok(counts[id] >= 1, `${id} must receive an input event`);
    }
    assert.equal(dom.window.document.getElementById("loose-field").value, "Typed-Secret-1");
  } finally {
    dom.window.close();
  }
});

test("clearing the fields keeps the success message the form's input handler would empty", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: PAGE });
  try {
    typePasswords(dom.window);
    const island = dom.window.document.getElementById("settings-change-password-status");
    // The real form empties its status island on every input event.
    dom.window.document.getElementById("settings-change-password-form").addEventListener("input", () => {
      island.textContent = "";
    });

    swapSuccess(dom.window, "settings-change-password-status", "clears-fields");

    assert.ok(island.querySelector(".status-ok"), "the success message must survive the field clearing");
    assert.equal(dom.window.document.getElementById("cp-new").value, "");
  } finally {
    dom.window.close();
  }
});

test("a success without the kind leaves the typed fields alone", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: PAGE });
  try {
    typePasswords(dom.window);
    const counts = watchInputEvents(dom.window);

    swapSuccess(dom.window, "settings-change-password-status", "");

    for (const id of FIELD_IDS) {
      assert.equal(dom.window.document.getElementById(id).value, "Typed-Secret-1", `${id} must keep its value`);
      assert.equal(counts[id], 0, `${id} must receive no input event`);
    }
  } finally {
    dom.window.close();
  }
});

test("a clears-fields island that is not inside a form is a no-op", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: PAGE });
  try {
    typePasswords(dom.window);
    const counts = watchInputEvents(dom.window);

    swapSuccess(dom.window, "loose-status", "clears-fields");

    for (const id of [...FIELD_IDS, "loose-field"]) {
      assert.equal(dom.window.document.getElementById(id).value, "Typed-Secret-1", `${id} must keep its value`);
      assert.equal(counts[id], 0, `${id} must receive no input event`);
    }
  } finally {
    dom.window.close();
  }
});
