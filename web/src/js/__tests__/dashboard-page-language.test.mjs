// WEB-232 on the dashboard's own fetches: the autosave is not an htmx request,
// so it names the page language itself and answers the server's HX-Refresh the
// way htmx does. A reload asked for by an answer that has arrived owes no
// "a save is still on the wire" prompt — the save has landed.

import test from "node:test";
import assert from "node:assert/strict";
import { readAppBundle, loadDOMWithScript } from "./_helpers.mjs";

const APP_BUNDLE = readAppBundle();
const DAY = "2026-08-12";
// jsdom implements no navigation; a reload reaches its virtual console as this
// exact report, captured here as the reload it stands for.
const RELOAD_REPORT = "Not implemented: navigation to another Document";

function dashboardPage() {
  return `<!doctype html><html lang="fr"><head><meta name="csrf-token" content="unit-test-token"></head><body>
  <div data-dashboard-editor>
    <form
      hx-put="/api/v1/days/${DAY}"
      data-save-feedback
      data-dashboard-save-form
      data-dashboard-date="${DAY}"
      data-today-entry-exists="false"
      data-autosave-clear-url="/api/v1/days/${DAY}?source=dashboard"
      data-autosave-saving="Saving..."
      data-autosave-saved="Saved"
      data-autosave-invalid="Fix the form errors to save"
      data-autosave-undo="Undo"
      data-day-save-failed-text="Couldn't save. Your entry is still here."
      data-day-save-retry-label="Try again">
      <input type="hidden" name="csrf_token" value="unit-test-token">
      <input type="hidden" name="day_form_account" value="unit-test-account-binding">
      <textarea id="today-notes" name="notes" data-dashboard-notes></textarea>
      <div id="save-status" class="save-status" aria-live="polite"></div>
      <div class="dashboard-autosave-indicator" data-dashboard-autosave-indicator data-autosave-state="idle" aria-live="polite"></div>
    </form>
  </div>
</body></html>`;
}

// A real reload dispatches beforeunload first; jsdom does not, so the recorder
// does, and keeps whether a leave prompt was asked for.
function installReloadRecorder(window, reloads) {
  const virtualConsole = window._settings && window._settings.virtualConsole;
  assert.ok(virtualConsole, "jsdom's virtual console is where a reload is reported");
  const forward = virtualConsole.emit.bind(virtualConsole);
  virtualConsole.emit = (type, error, ...rest) => {
    if (type === "jsdomError" && error && error.message === RELOAD_REPORT) {
      const leaving = new window.Event("beforeunload", { cancelable: true });
      window.dispatchEvent(leaving);
      reloads.push({ prompted: leaving.defaultPrevented });
      return true;
    }
    return forward(type, error, ...rest);
  };
}

function installFetchRecorder(window, calls, refresh) {
  window.fetch = (url, init) => {
    calls.push({ url: String(url), init: init || {} });
    return Promise.resolve({
      ok: true,
      status: 200,
      headers: { get: (name) => (refresh && name === "HX-Refresh" ? "true" : null) },
      text: () => Promise.resolve(""),
    });
  };
}

async function loadDashboard({ refresh }) {
  const reloads = [];
  const calls = [];
  const dom = await loadDOMWithScript(APP_BUNDLE, {
    html: dashboardPage(),
    url: "https://ovumcy.test/dashboard",
    beforeRun: (window) => {
      installReloadRecorder(window, reloads);
      installFetchRecorder(window, calls, refresh);
    },
  });
  return { dom, reloads, calls };
}

function settle() {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

async function autosaveNote(window, value) {
  const notes = window.document.querySelector("[data-dashboard-notes]");
  notes.value = value;
  notes.dispatchEvent(new window.Event("input", { bubbles: true }));
  await window.__ovumcyRetryDashboardAutosave(window.document.querySelector("[data-dashboard-save-form]"));
  await settle();
}

test("a dashboard autosave names the page language and reloads, unprompted, on HX-Refresh", async () => {
  const { dom, reloads, calls } = await loadDashboard({ refresh: true });
  try {
    await autosaveNote(dom.window, "noté avant le changement de langue");

    assert.equal(calls.length, 1);
    assert.equal(calls[0].init.headers["X-Ovumcy-Page-Lang"], "fr");
    assert.deepEqual(reloads, [{ prompted: false }], "the save has landed, so the reload asks nothing");
  } finally {
    dom.window.close();
  }
});

test("a dashboard autosave answered without HX-Refresh does not reload", async () => {
  const { dom, reloads, calls } = await loadDashboard({ refresh: false });
  try {
    await autosaveNote(dom.window, "même langue");

    assert.equal(calls.length, 1);
    assert.deepEqual(reloads, []);
  } finally {
    dom.window.close();
  }
});

function beginHTMXDaySave(window) {
  const journal = window.document.querySelector("[data-dashboard-save-form]");
  journal.dispatchEvent(
    new window.CustomEvent("htmx:beforeSend", {
      bubbles: true,
      detail: {
        requestConfig: {
          elt: journal,
          verb: "put",
          path: `/api/v1/days/${DAY}`,
          headers: { "HX-Request": "true" },
          formData: new window.FormData(journal),
        },
      },
    })
  );
  return journal;
}

for (const refresh of [true, false]) {
  test(`an htmx day save answered ${refresh ? "with" : "without"} HX-Refresh ${refresh ? "leaves unprompted" : "still guards the leave"}`, async () => {
    const { dom } = await loadDashboard({ refresh: false });
    try {
      const journal = beginHTMXDaySave(dom.window);
      journal.dispatchEvent(
        new dom.window.CustomEvent("htmx:beforeOnLoad", {
          bubbles: true,
          detail: { xhr: { getResponseHeader: (name) => (refresh && name === "HX-Refresh" ? "true" : null) } },
        })
      );

      const leaving = new dom.window.Event("beforeunload", { cancelable: true });
      dom.window.dispatchEvent(leaving);
      assert.equal(leaving.defaultPrevented, !refresh);
    } finally {
      dom.window.close();
    }
  });
}
