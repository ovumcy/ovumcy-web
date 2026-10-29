// WEB-229: an in-page action that disables, hides or reloads the control the
// owner activated hands keyboard focus back through one helper (restoreFocus),
// instead of leaving it on BODY. The sites that drive it here are the save
// button held busy by a request, the late-cycle links into a plain section, and
// the control that asks for a reload (HX-Refresh).

import test from "node:test";
import assert from "node:assert/strict";
import { readAppBundle, loadDOMWithScript } from "./_helpers.mjs";

const APP_BUNDLE = readAppBundle();

const SAVE_PAGE = `<!doctype html><html><head></head><body>
  <form id="day-form" data-save-feedback data-day-editor-form hx-patch="/api/v1/days/2026-10-10">
    <textarea name="notes"></textarea>
    <button type="submit" data-save-button>Save</button>
    <div id="calendar-save-status" aria-live="polite"></div>
  </form>
</body></html>`;

function htmxEvent(window, name, form, detail = {}) {
  const event = new window.CustomEvent(name, {
    bubbles: true,
    detail: { elt: form, target: form, successful: true, xhr: null, ...detail },
  });
  form.dispatchEvent(event);
}

// A browser blurs a focused control the moment it becomes disabled; jsdom does
// not, so the test plays that part.
function blurLikeABrowser(button) {
  if (button.disabled) {
    // jsdom's blur() ignores a disabled element.
    button.disabled = false;
    button.blur();
    button.disabled = true;
  }
}

test("a save that disables the focused button gives it focus back when it is enabled again", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: SAVE_PAGE });
  try {
    const { window } = dom;
    const document = window.document;
    const form = document.getElementById("day-form");
    const button = form.querySelector("[data-save-button]");
    button.focus();

    htmxEvent(window, "htmx:beforeRequest", form);
    assert.equal(button.disabled, true);
    blurLikeABrowser(button);
    assert.equal(document.activeElement, document.body, "the disabled button lost focus, as in a browser");

    htmxEvent(window, "htmx:afterRequest", form);
    assert.equal(button.disabled, false);
    assert.equal(document.activeElement, button);
  } finally {
    dom.window.close();
  }
});

test("a save does not take focus from a field the owner moved to while it was in flight", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: SAVE_PAGE });
  try {
    const { window } = dom;
    const document = window.document;
    const form = document.getElementById("day-form");
    const button = form.querySelector("[data-save-button]");
    const notes = form.querySelector("textarea");
    button.focus();

    htmxEvent(window, "htmx:beforeRequest", form);
    blurLikeABrowser(button);
    notes.focus();
    htmxEvent(window, "htmx:afterRequest", form);

    assert.equal(document.activeElement, notes);
  } finally {
    dom.window.close();
  }
});

test("a save that never held focus does not grab it", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: SAVE_PAGE });
  try {
    const { window } = dom;
    const document = window.document;
    const form = document.getElementById("day-form");

    htmxEvent(window, "htmx:beforeRequest", form);
    htmxEvent(window, "htmx:afterRequest", form);

    assert.equal(document.activeElement, document.body);
  } finally {
    dom.window.close();
  }
});

const LATE_CYCLE_PAGE = `<!doctype html><html><head></head><body>
  <span data-dashboard-late-cycle-actions>
    <a href="#dashboard-symptoms" data-late-cycle-action="symptoms">Symptoms</a>
    <a href="#dashboard-skip" data-other>Other</a>
  </span>
  <details><summary>More</summary>
    <fieldset id="dashboard-symptoms"><legend>Symptoms</legend><input type="checkbox" name="symptom_ids"></fieldset>
  </details>
  <div id="dashboard-skip"></div>
</body></html>`;

test("a late-cycle link hands focus to the section it points at, inside a closed disclosure too", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: LATE_CYCLE_PAGE });
  try {
    const { window } = dom;
    const document = window.document;
    const link = document.querySelector("[data-late-cycle-action]");
    const section = document.getElementById("dashboard-symptoms");
    link.focus();

    link.dispatchEvent(new window.MouseEvent("click", { bubbles: true, cancelable: true, button: 0 }));

    assert.equal(document.activeElement, section);
    assert.equal(section.getAttribute("tabindex"), "-1", "focusable by script, never a tab stop");
    assert.equal(document.querySelector("details").open, true);
  } finally {
    dom.window.close();
  }
});

test("an ordinary in-page link keeps the browser's own focus behaviour", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: LATE_CYCLE_PAGE });
  try {
    const { window } = dom;
    const document = window.document;
    const link = document.querySelector("[data-other]");
    link.focus();

    link.dispatchEvent(new window.MouseEvent("click", { bubbles: true, cancelable: true, button: 0 }));

    assert.equal(document.activeElement, link);
    assert.equal(document.getElementById("dashboard-skip").hasAttribute("tabindex"), false);
  } finally {
    dom.window.close();
  }
});

const RELOAD_FORM_PAGE = `<!doctype html><html><head></head><body>
  <form id="goal" data-focus-after-reload="[data-usage-goal-chip]"><button type="submit">Switch</button></form>
  <form id="plain"><button type="submit">Plain</button></form>
</body></html>`;

function refreshResponse(value) {
  return { getResponseHeader: (name) => (name === "HX-Refresh" ? value : null) };
}

test("a reload asked by a control that names a focus target carries the name across the reload", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: RELOAD_FORM_PAGE });
  try {
    const { window } = dom;
    const form = window.document.getElementById("goal");
    htmxEvent(window, "htmx:beforeOnLoad", form, { xhr: refreshResponse("true") });
    assert.equal(window.sessionStorage.getItem("ovumcy:focus-after-reload"), "[data-usage-goal-chip]");
  } finally {
    dom.window.close();
  }
});

test("a reload from a control with no focus target, or an answer without HX-Refresh, remembers nothing", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: RELOAD_FORM_PAGE });
  try {
    const { window } = dom;
    htmxEvent(window, "htmx:beforeOnLoad", window.document.getElementById("plain"), { xhr: refreshResponse("true") });
    htmxEvent(window, "htmx:beforeOnLoad", window.document.getElementById("goal"), { xhr: refreshResponse(null) });
    assert.equal(window.sessionStorage.getItem("ovumcy:focus-after-reload"), null);
  } finally {
    dom.window.close();
  }
});

test("the reloaded page focuses the remembered target once and forgets it", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, {
    html: `<!doctype html><html><head></head><body><details><summary data-usage-goal-chip>Health</summary></details></body></html>`,
    beforeRun(window) {
      window.sessionStorage.setItem("ovumcy:focus-after-reload", "[data-usage-goal-chip]");
    },
  });
  try {
    const { window } = dom;
    assert.equal(window.document.activeElement, window.document.querySelector("[data-usage-goal-chip]"));
    assert.equal(window.sessionStorage.getItem("ovumcy:focus-after-reload"), null);
  } finally {
    dom.window.close();
  }
});

test("a target that arrives with a later swap is focused when it settles", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, {
    html: `<!doctype html><html><head></head><body><div id="day-editor"></div></body></html>`,
    beforeRun(window) {
      window.sessionStorage.setItem("ovumcy:focus-after-reload", "[data-day-cycle-start-button]");
    },
  });
  try {
    const { window } = dom;
    const document = window.document;
    assert.equal(document.activeElement, document.body, "nothing to focus yet");

    const editor = document.getElementById("day-editor");
    editor.innerHTML = `<button type="submit" data-day-cycle-start-button>Mark</button>`;
    editor.dispatchEvent(new window.CustomEvent("htmx:afterSettle", { bubbles: true, detail: { target: editor } }));

    assert.equal(document.activeElement, editor.querySelector("button"));
  } finally {
    dom.window.close();
  }
});
