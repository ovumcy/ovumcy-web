// The dashboard is today's page, and every day URL on it — the autosave PUT,
// the clear and undo DELETEs, the cycle-start toggle — names the date the
// server rendered it for. A dashboard left open across the owner's midnight
// went on writing the new day's entries into yesterday, with nothing on screen
// saying so. Once the owner's local date has moved on, the page reloads onto
// the new day.
//
// The reload never costs typed text: while an edit is outstanding — dirty,
// on the wire, or refused and waiting for a retry — the page stays. The edit
// is saved to the day the page shows, and the reload follows that save.

import test from "node:test";
import assert from "node:assert/strict";
import { readAppBundle, loadDOMWithScript } from "./_helpers.mjs";

const APP_BUNDLE = readAppBundle();

const RENDERED_DAY = "2026-08-12";
// Local wall-clock instants, in the same zone the bundle's Date reads.
const BEFORE_MIDNIGHT = new Date(2026, 7, 12, 23, 58, 0).getTime();
const LATER_SAME_DAY = new Date(2026, 7, 12, 23, 59, 30).getTime();
const AFTER_MIDNIGHT = new Date(2026, 7, 13, 0, 1, 0).getTime();

// jsdom implements no navigation: a reload reaches its virtual console as this
// exact "not implemented" report. It is captured as the reload it stands for,
// and only on the page this suite builds — every other jsdom report still
// fails the test through the shared harness.
const RELOAD_REPORT = "Not implemented: navigation to another Document";

function dashboardPage() {
  return `<!doctype html><html><head><meta name="csrf-token" content="unit-test-token"></head><body>
  <div data-dashboard-editor>
    <form
      hx-put="/api/v1/days/${RENDERED_DAY}"
      data-save-feedback
      data-dashboard-save-form
      data-dashboard-date="${RENDERED_DAY}"
      data-today-entry-exists="false"
      data-autosave-clear-url="/api/v1/days/${RENDERED_DAY}?source=dashboard"
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

function installClock(window, startAt) {
  window.__testClock = { now: startAt };
  window.eval(`(function () {
    var RealDate = Date;
    var clock = window.__testClock;
    class TestDate extends RealDate {
      constructor(...args) {
        if (args.length === 0) {
          super(clock.now);
        } else {
          super(...args);
        }
      }
      static now() {
        return clock.now;
      }
    }
    window.Date = TestDate;
  })();`);
}

function installReloadRecorder(window, reloads) {
  const virtualConsole = window._settings && window._settings.virtualConsole;
  assert.ok(virtualConsole, "jsdom's virtual console is where a reload is reported");
  const forward = virtualConsole.emit.bind(virtualConsole);
  virtualConsole.emit = (type, error, ...rest) => {
    if (type === "jsdomError" && error && error.message === RELOAD_REPORT) {
      reloads.push(String(window.location.href));
      return true;
    }
    return forward(type, error, ...rest);
  };
}

function installFetchRecorder(window, calls, { ok = true } = {}) {
  window.fetch = (url, init) => {
    calls.push({ url: String(url), init: init || {} });
    return Promise.resolve({
      ok,
      status: ok ? 200 : 500,
      headers: { get: () => null },
      text: () => Promise.resolve(""),
    });
  };
}

function captureIntervals(window, intervals) {
  const realSetInterval = window.setInterval.bind(window);
  window.setInterval = (callback, delay, ...rest) => {
    intervals.push(callback);
    return realSetInterval(callback, delay, ...rest);
  };
}

async function loadDashboard({ fetchOk = true } = {}) {
  const reloads = [];
  const calls = [];
  const intervals = [];
  const dom = await loadDOMWithScript(APP_BUNDLE, {
    html: dashboardPage(),
    url: "https://ovumcy.test/dashboard",
    beforeRun: (window) => {
      installClock(window, BEFORE_MIDNIGHT);
      installReloadRecorder(window, reloads);
      installFetchRecorder(window, calls, { ok: fetchOk });
      captureIntervals(window, intervals);
    },
  });
  return { dom, reloads, calls, intervals };
}

function setClock(window, at) {
  window.__testClock.now = at;
}

function pageBecomesVisible(window) {
  window.document.dispatchEvent(new window.Event("visibilitychange"));
}

function typeNote(window, value) {
  const notes = window.document.querySelector("[data-dashboard-notes]");
  notes.value = value;
  notes.dispatchEvent(new window.Event("input", { bubbles: true }));
  return notes;
}

function saveForm(window) {
  return window.document.querySelector("[data-dashboard-save-form]");
}

function settle() {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

test("an idle dashboard reloads onto the new day once the owner's date has changed", async () => {
  const { dom, reloads, calls } = await loadDashboard();
  setClock(dom.window, AFTER_MIDNIGHT);
  pageBecomesVisible(dom.window);
  await settle();

  assert.deepEqual(reloads, ["https://ovumcy.test/dashboard"], "the page must reload onto the new day");
  assert.deepEqual(calls, [], "an idle page writes nothing on its way to the new day");
  dom.window.close();
});

test("the clock alone reloads the page: no event from the owner is needed", async () => {
  const { dom, reloads, intervals } = await loadDashboard();
  setClock(dom.window, AFTER_MIDNIGHT);
  intervals.forEach((tick) => tick());
  await settle();

  assert.equal(reloads.length, 1, "a periodic check must notice the new day on a page left in view");
  dom.window.close();
});

test("focusing a field after midnight reloads the page before anything is typed", async () => {
  const { dom, reloads } = await loadDashboard();
  setClock(dom.window, AFTER_MIDNIGHT);
  const notes = dom.window.document.querySelector("[data-dashboard-notes]");
  notes.dispatchEvent(new dom.window.FocusEvent("focusin", { bubbles: true }));
  await settle();

  assert.equal(reloads.length, 1);
  dom.window.close();
});

test("the same day never reloads the page", async () => {
  const { dom, reloads, intervals } = await loadDashboard();
  setClock(dom.window, LATER_SAME_DAY);
  pageBecomesVisible(dom.window);
  intervals.forEach((tick) => tick());
  await settle();

  assert.deepEqual(reloads, []);
  dom.window.close();
});

test("a pending edit holds the reload, is saved to the day the page shows, and the reload follows", async () => {
  const { dom, reloads, calls } = await loadDashboard();
  const notes = typeNote(dom.window, "late-night note");
  setClock(dom.window, AFTER_MIDNIGHT);
  pageBecomesVisible(dom.window);
  await settle();

  assert.deepEqual(reloads, [], "a reload now would drop the typed note");
  assert.equal(notes.value, "late-night note");

  await dom.window.__ovumcyRetryDashboardAutosave(saveForm(dom.window));
  await settle();

  assert.equal(calls.length, 1);
  assert.equal(calls[0].url, `/api/v1/days/${RENDERED_DAY}`, "the edit belongs to the day the page shows");
  assert.ok(String(calls[0].init.body).includes("notes=late-night+note"));
  assert.equal(reloads.length, 1, "once the edit has landed, the page moves to the new day");
  dom.window.close();
});

test("a refused save keeps the page, and the typed text, across midnight", async () => {
  const { dom, reloads } = await loadDashboard({ fetchOk: false });
  const notes = typeNote(dom.window, "kept through a failure");
  await dom.window.__ovumcyRetryDashboardAutosave(saveForm(dom.window));
  setClock(dom.window, AFTER_MIDNIGHT);
  pageBecomesVisible(dom.window);
  await settle();

  assert.equal(saveForm(dom.window).dataset.autosaveDirty, "true", "the refused edit is still outstanding");
  assert.deepEqual(reloads, [], "a reload would drop an edit the server never took");
  assert.equal(notes.value, "kept through a failure");
  dom.window.close();
});

test("a request still in flight elsewhere on the page holds the reload", async () => {
  const { dom, reloads } = await loadDashboard();
  const busy = dom.window.document.createElement("form");
  busy.className = "htmx-request";
  dom.window.document.body.appendChild(busy);
  setClock(dom.window, AFTER_MIDNIGHT);
  pageBecomesVisible(dom.window);
  await settle();

  assert.deepEqual(reloads, []);

  busy.classList.remove("htmx-request");
  pageBecomesVisible(dom.window);
  await settle();
  assert.equal(reloads.length, 1, "the reload goes ahead once the request has finished");
  dom.window.close();
});
