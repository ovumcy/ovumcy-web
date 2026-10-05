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

const STALE_LINE = "Luteal phase";
const FRESH_LINE = "Predictions paused";

// The real header's structure: the blocks that depend on the pregnancy result
// beside the goal chip, a live <details> whose quick-switch forms are driven by
// htmx (hx-patch) — a node cloned out of a fetched page would never be bound.
function shell(text, { line, phase, explainer = "", warnings = "" }) {
  return `<section data-dashboard-shell data-phase="${phase}">
    <section class="card dashboard-status-header reveal" data-dashboard-status-header data-dashboard-phase="${phase}">
      <div class="dashboard-status-top">
        <p class="dashboard-cycle-lead"><span class="sr-only">Cycle day</span><span data-dashboard-cycle-day>12</span></p>
        <p class="dashboard-status-line" data-dashboard-status-line>${line}</p>
        <details class="dashboard-goal-chip" data-usage-goal-summary>
          <summary data-usage-goal-chip>Avoiding pregnancy</summary>
          <div role="group" data-usage-goal-quick-switch>
            <form
              action="/api/v1/users/current/cycle?source=dashboard"
              method="post"
              hx-patch="/api/v1/users/current/cycle?source=dashboard"
              hx-target="#dashboard-usage-goal-status"
              data-usage-goal-quick-switch-form>
              <input type="hidden" name="usage_goal" value="trying_to_conceive">
              <button type="submit" data-usage-goal-choice="trying_to_conceive">Trying to conceive</button>
            </form>
            <div id="dashboard-usage-goal-status" class="save-status" aria-live="polite"></div>
          </div>
        </details>
      </div>
      <div data-dashboard-cycle-ribbon></div>
      ${explainer}
      <p data-dashboard-reminder-banner>${text}</p>
      ${warnings}
      <p data-dashboard-prediction-disclaimer>Not medical advice</p>
    </section>
  </section>`;
}

const STALE_SHELL = shell(STALE_HEADER, {
  line: STALE_LINE,
  phase: "luteal",
  explainer: "<p data-dashboard-prediction-explainer>Estimated from your cycles</p>",
});
const FRESH_SHELL = shell(FRESH_HEADER, {
  line: FRESH_LINE,
  phase: "unknown",
  warnings: "<div data-dashboard-cycle-warnings><p data-dashboard-prediction-past>Past</p></div>",
});

function dashboardPage() {
  return `<!doctype html><html><head><meta name="csrf-token" content="unit-test-token"></head><body>
  ${STALE_SHELL}
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
      text: () => Promise.resolve(isGet ? `<!doctype html><html><body>${FRESH_SHELL}</body></html>` : ""),
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
    assert.ok(document.querySelector("[data-dashboard-save-form]"), "the journal form is left in place");
  } finally {
    dom.window.close();
  }
});

test("the swap leaves the goal switch, the shell and the header as the same live nodes", async () => {
  const { dom } = await loadDashboard();
  try {
    const document = dom.window.document;
    const shellNode = document.querySelector("[data-dashboard-shell]");
    const header = document.querySelector("[data-dashboard-status-header]");
    const goalChip = document.querySelector("[data-usage-goal-summary]");
    const goalForm = document.querySelector("[data-usage-goal-quick-switch-form]");
    goalChip.open = true;

    pick(document, "positive");
    await saveNow(dom.window);

    assert.equal(
      document.querySelector("[data-dashboard-status-line]").textContent.trim(),
      FRESH_LINE,
      "the header text changed"
    );
    assert.equal(document.querySelector("[data-dashboard-shell]"), shellNode, "the shell is not replaced");
    assert.equal(document.querySelector("[data-dashboard-status-header]"), header, "the header is not replaced");
    assert.equal(
      document.querySelector("[data-usage-goal-quick-switch-form]"),
      goalForm,
      "the htmx-driven goal switch form is the same node, still bound"
    );
    assert.equal(document.querySelector("[data-usage-goal-summary]"), goalChip);
    assert.equal(goalChip.open, true, "an open goal chip stays open");
    assert.equal(header.getAttribute("data-dashboard-phase"), "unknown", "the header's own state attributes follow");
    assert.equal(header.classList.contains("reveal"), true, "the header is not re-created, so its entrance is not replayed");
  } finally {
    dom.window.close();
  }
});

test("a block the result adds or removes is placed in the template's order", async () => {
  const { dom } = await loadDashboard();
  try {
    const document = dom.window.document;
    const header = document.querySelector("[data-dashboard-status-header]");
    assert.ok(header.querySelector("[data-dashboard-prediction-explainer]"));
    assert.equal(header.querySelector("[data-dashboard-cycle-warnings]"), null);

    pick(document, "positive");
    await saveNow(dom.window);

    assert.equal(header.querySelector("[data-dashboard-prediction-explainer]"), null, "a block the server dropped is removed");
    const warnings = header.querySelector("[data-dashboard-cycle-warnings]");
    assert.ok(warnings, "a block the server added appears");
    assert.equal(
      warnings.nextElementSibling.hasAttribute("data-dashboard-prediction-disclaimer"),
      true,
      "it sits before the static disclaimer, where the template renders it"
    );
    assert.equal(
      warnings.previousElementSibling.hasAttribute("data-dashboard-reminder-banner"),
      true,
      "and after the banner"
    );
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
