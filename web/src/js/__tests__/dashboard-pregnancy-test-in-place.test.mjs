// A pregnancy-test result changes more of the dashboard than the control the
// owner clicked: the field's own wording ("No result recorded" / the Remove
// action) and the status header (a positive result pauses the next-period
// estimate, removing it resumes it). The journal saves itself, so none of that
// may wait for a reload. The field follows the click in the browser; the header
// is computed server-side, so a changed result fetches the dashboard again and
// swaps only that block. And when Remove hides its own button, focus must land
// on a control instead of falling back to the page body.

import test from "node:test";
import assert from "node:assert/strict";
import { readAppBundle, loadDOMWithScript } from "./_helpers.mjs";

const APP_BUNDLE = readAppBundle();

const TODAY = "2026-08-12";
const STALE_HEADER = "Period likely today";
const FRESH_HEADER = "Next period estimate paused";

function shell(text, extraClass = "") {
  return `<section data-dashboard-shell>
    <section class="card dashboard-status-header${extraClass}" data-dashboard-status-header>
      <p data-dashboard-reminder-banner>${text}</p>
    </section>
  </section>`;
}

function dashboardPage() {
  return `<!doctype html><html><head><meta name="csrf-token" content="unit-test-token"></head><body>
  ${shell(STALE_HEADER, " reveal")}
  <div data-dashboard-editor>
    <form
      hx-put="/api/v1/days/${TODAY}"
      data-save-feedback
      data-dashboard-save-form
      data-dashboard-date="${TODAY}"
      data-today-entry-exists="false"
      data-autosave-saving="Saving..."
      data-autosave-saved="Saved">
      <input type="hidden" name="csrf_token" value="unit-test-token">
      <input type="radio" name="mood" value="4">
      <div data-pregnancy-test data-pregnancy-test-state="absent">
        <label><input type="radio" name="pregnancy_test" value="negative"></label>
        <label><input type="radio" name="pregnancy_test" value="positive"></label>
        <input type="radio" name="pregnancy_test" value="none" data-pregnancy-test-unset checked hidden>
        <button type="button" data-pregnancy-test-remove hidden>Remove result</button>
        <p data-pregnancy-test-empty>No result recorded</p>
      </div>
      <div class="save-status" aria-live="polite"></div>
      <div data-dashboard-autosave-indicator data-autosave-state="idle"></div>
    </form>
  </div>
</body></html>`;
}

// Answers the day save with 200 and the dashboard page with a header that has
// moved on, recording every request in order.
function installRecorder(window) {
  const calls = [];
  window.fetch = (url, init) => {
    const request = { url: String(url), init: init || {} };
    calls.push(request);
    const isGet = (request.init.method || "GET") === "GET";
    return Promise.resolve({
      ok: true,
      status: 200,
      headers: { get: () => null },
      text: () => Promise.resolve(isGet ? `<!doctype html><html><body>${shell(FRESH_HEADER, " reveal")}</body></html>` : ""),
    });
  };
  return calls;
}

async function loadDashboard() {
  let calls = [];
  const dom = await loadDOMWithScript(APP_BUNDLE, {
    html: dashboardPage(),
    url: "https://ovumcy.test/dashboard",
    beforeRun: (window) => {
      calls = installRecorder(window);
    },
  });
  return { dom, calls: () => calls };
}

function settle() {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

// The unload flush is the one path that sends a dirty journal at once, and it
// writes from pagehide only (beforeunload decides the leave prompt, nothing more).
async function saveNow(window) {
  window.dispatchEvent(new window.Event("pagehide"));
  await settle();
  await settle();
}

function fireChange(node) {
  node.dispatchEvent(new node.ownerDocument.defaultView.Event("change", { bubbles: true }));
}

function pick(document, value) {
  const radio = document.querySelector(`input[name='pregnancy_test'][value='${value}']`);
  radio.checked = true;
  fireChange(radio);
}

function field(document) {
  return document.querySelector("[data-pregnancy-test]");
}

function bannerText(document) {
  return document.querySelector("[data-dashboard-reminder-banner]").textContent.trim();
}

const gets = (calls) => calls().filter((call) => (call.init.method || "GET") === "GET");

test("picking a result offers Remove and drops the empty wording, with no reload", async () => {
  const { dom } = await loadDashboard();
  try {
    const document = dom.window.document;
    pick(document, "positive");

    assert.equal(field(document).getAttribute("data-pregnancy-test-state"), "recorded");
    assert.equal(field(document).querySelector("[data-pregnancy-test-remove]").hasAttribute("hidden"), false);
    assert.equal(field(document).querySelector("[data-pregnancy-test-empty]").hasAttribute("hidden"), true);
  } finally {
    dom.window.close();
  }
});

test("a saved pregnancy result fetches the dashboard again and swaps only the status header", async () => {
  const { dom, calls } = await loadDashboard();
  try {
    const document = dom.window.document;
    assert.equal(bannerText(document), STALE_HEADER);

    pick(document, "positive");
    await saveNow(dom.window);

    assert.equal(gets(calls).length, 1, "one refresh for the one changed result");
    assert.equal(gets(calls)[0].url, "/dashboard");
    assert.equal(bannerText(document), FRESH_HEADER, "the header now carries the server's answer");
    assert.equal(document.querySelectorAll("[data-dashboard-shell]").length, 1);
    assert.equal(
      document.querySelector("[data-dashboard-status-header]").classList.contains("reveal"),
      false,
      "the entrance animation is not replayed on a refresh"
    );
    assert.ok(document.querySelector("[data-dashboard-save-form]"), "the journal form is left in place");
  } finally {
    dom.window.close();
  }
});

test("a save that leaves the pregnancy result alone does not refetch the dashboard", async () => {
  const { dom, calls } = await loadDashboard();
  try {
    const document = dom.window.document;
    const mood = document.querySelector("input[name='mood']");
    mood.checked = true;
    fireChange(mood);
    await saveNow(dom.window);

    assert.equal(calls().filter((call) => call.init.method === "PUT").length, 1, "the mood saved");
    assert.equal(gets(calls).length, 0, "no header refresh for an unrelated field");
    assert.equal(bannerText(document), STALE_HEADER);
  } finally {
    dom.window.close();
  }
});

test("Remove result restores the empty wording, keeps focus on a control and refreshes the header", async () => {
  const { dom, calls } = await loadDashboard();
  try {
    const document = dom.window.document;
    pick(document, "positive");
    await saveNow(dom.window);

    const remove = field(document).querySelector("[data-pregnancy-test-remove]");
    remove.focus();
    remove.click();

    assert.equal(field(document).getAttribute("data-pregnancy-test-state"), "absent");
    assert.equal(remove.hasAttribute("hidden"), true);
    assert.equal(field(document).querySelector("[data-pregnancy-test-empty]").hasAttribute("hidden"), false);
    assert.notEqual(document.activeElement, document.body, "focus must not fall back to the page body");
    assert.equal(document.activeElement.getAttribute("name"), "pregnancy_test", "focus lands on the result control");

    await saveNow(dom.window);
    const puts = calls().filter((call) => call.init.method === "PUT");
    assert.ok(String(puts[puts.length - 1].init.body).includes("pregnancy_test=none"), "the removal is what is saved");
    assert.equal(gets(calls).length, 2, "the removal refreshes the header too");
  } finally {
    dom.window.close();
  }
});
