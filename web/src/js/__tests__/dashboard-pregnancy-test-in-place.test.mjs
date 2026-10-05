// A pregnancy-test result changes more of the dashboard than the control the
// owner clicked: the field's own wording ("No result recorded" / the Remove
// action) and the status header (a positive result pauses the next-period
// estimate, removing it resumes it). The journal saves itself, so none of that
// may wait for a reload. The field follows the click in the browser; the header
// is computed server-side, so a changed result fetches the dashboard again and
// swaps only the blocks that depend on it. And when Remove hides its own button,
// focus must land on a control instead of falling back to the page body; Undo
// puts the field back with the value; a swap keeps the owner's place.

import test from "node:test";
import assert from "node:assert/strict";
import { readAppBundle, loadDOMWithScript } from "./_helpers.mjs";

const APP_BUNDLE = readAppBundle();

const TODAY = "2026-08-12";
const STALE_HEADER = "Period likely today";
const FRESH_HEADER = "Next period estimate paused";

const STALE_LINE = "Luteal phase";
const FRESH_LINE = "Predictions paused";

const ORDERED_HOOKS = [
  "[data-dashboard-cycle-ribbon]",
  "[data-dashboard-prediction-explainer]",
  "[data-dashboard-reminder-banner]",
  "[data-dashboard-cycle-warnings]",
  "[data-dashboard-prediction-disclaimer]",
  "[data-dashboard-factor-hint]",
];

// The real header's structure: the blocks that depend on the pregnancy result
// beside the goal chip, a live <details> whose quick-switch forms are driven by
// htmx (hx-patch) — a node cloned out of a fetched page would never be bound.
function shell(text, { line, phase, explainer = "", warnings = "", headerAttrs = "" }) {
  return `<section data-dashboard-shell data-phase="${phase}">
    <section class="card dashboard-status-header reveal" data-dashboard-status-header data-dashboard-phase="${phase}"${headerAttrs}>
      <div class="dashboard-status-top">
        <p class="dashboard-cycle-lead"><span class="sr-only">Cycle day</span><span data-dashboard-cycle-day>12</span></p>
        <p class="dashboard-status-line" data-dashboard-status-line aria-live="polite">${line}</p>
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

// The stale header carries a state attribute the fresh one no longer has.
const STALE_SHELL = shell(STALE_HEADER, {
  line: STALE_LINE,
  phase: "luteal",
  explainer: "<p data-dashboard-prediction-explainer>Estimated from your cycles</p>",
  headerAttrs: ' data-cycle-stale="true"',
});
// What the server sends carries the entrance-animation class on its blocks.
const FRESH_SHELL = shell(FRESH_HEADER, {
  line: `<span class="reveal">${FRESH_LINE}</span>`,
  phase: "unknown",
  warnings:
    '<div class="reveal" data-dashboard-cycle-warnings><p class="reveal" data-dashboard-prediction-past>Past</p></div>',
});

function warningsShell(text, warnings) {
  return shell(text, { line: STALE_LINE, phase: "luteal", warnings });
}

function dashboardPage({ shellHTML = STALE_SHELL, recorded = false, entryExists = false } = {}) {
  const checked = (value) => (recorded && value === "positive" ? " checked" : "");
  return `<!doctype html><html><head><meta name="csrf-token" content="unit-test-token"></head><body>
  ${shellHTML}
  <div data-dashboard-editor>
    <form
      hx-put="/api/v1/days/${TODAY}"
      data-save-feedback
      data-dashboard-save-form
      data-dashboard-date="${TODAY}"
      data-today-entry-exists="${entryExists}"
      data-autosave-saving="Saving..."
      data-autosave-saved="Saved"
      data-autosave-undo="Undo">
      <input type="hidden" name="csrf_token" value="unit-test-token">
      <input type="radio" name="mood" value="4">
      <div data-pregnancy-test data-pregnancy-test-state="${recorded ? "recorded" : "absent"}">
        <label><input type="radio" name="pregnancy_test" value="negative"${checked("negative")}></label>
        <label><input type="radio" name="pregnancy_test" value="positive"${checked("positive")}></label>
        <input type="radio" name="pregnancy_test" value="none" data-pregnancy-test-unset${recorded ? "" : " checked"} hidden>
        <button type="button" data-pregnancy-test-remove${recorded ? "" : " hidden"}>Remove result</button>
        <p data-pregnancy-test-empty${recorded ? " hidden" : ""}>No result recorded</p>
      </div>
      <div class="save-status" aria-live="polite"></div>
      <div data-dashboard-autosave-indicator data-autosave-state="idle"></div>
    </form>
  </div>
</body></html>`;
}

function okResponse(text) {
  return { ok: true, status: 200, headers: { get: () => null }, text: () => Promise.resolve(text) };
}

function pageOf(shellHTML) {
  return `<!doctype html><html><body>${shellHTML}</body></html>`;
}

// Answers the day save with 200 and the dashboard page with a header that has
// moved on, recording every request in order.
function recorderFor(freshShell) {
  return (window, calls) => {
    window.fetch = (url, init) => {
      const request = { url: String(url), init: init || {} };
      calls.push(request);
      const isGet = (request.init.method || "GET") === "GET";
      return Promise.resolve(okResponse(isGet ? pageOf(freshShell) : ""));
    };
  };
}

async function loadDashboard({ install = recorderFor(FRESH_SHELL), ...page } = {}) {
  const calls = [];
  const dom = await loadDOMWithScript(APP_BUNDLE, {
    html: dashboardPage(page),
    url: "https://ovumcy.test/dashboard",
    beforeRun: (window) => install(window, calls),
  });
  return { dom, calls: () => calls };
}

function settle() {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

async function settleAll() {
  for (let round = 0; round < 3; round++) {
    await settle();
  }
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

function undoNow(window) {
  window.document
    .querySelector("[data-dashboard-autosave-undo]")
    .dispatchEvent(new window.MouseEvent("click", { bubbles: true, cancelable: true }));
}

function assertFieldReads(document, recorded) {
  const node = field(document);
  assert.equal(node.getAttribute("data-pregnancy-test-state"), recorded ? "recorded" : "absent");
  assert.equal(node.querySelector("[data-pregnancy-test-remove]").hasAttribute("hidden"), !recorded);
  assert.equal(node.querySelector("[data-pregnancy-test-empty]").hasAttribute("hidden"), recorded);
}

const gets = (calls) => calls().filter((call) => (call.init.method || "GET") === "GET");
const puts = (calls) => calls().filter((call) => call.init.method === "PUT");

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
    const statusLine = document.querySelector("[data-dashboard-status-line]");
    goalChip.open = true;

    pick(document, "positive");
    await saveNow(dom.window);

    assert.equal(
      document.querySelector("[data-dashboard-status-line]").textContent.trim(),
      FRESH_LINE,
      "the header text changed"
    );
    assert.equal(
      document.querySelector("[data-dashboard-status-line]"),
      statusLine,
      "the status line is a live region: its node stays and only its content changes"
    );
    assert.equal(statusLine.getAttribute("aria-live"), "polite");
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

test("a data attribute the fresh header no longer carries is removed from the live one", async () => {
  const { dom } = await loadDashboard();
  try {
    const document = dom.window.document;
    const header = document.querySelector("[data-dashboard-status-header]");
    assert.equal(header.getAttribute("data-cycle-stale"), "true");

    pick(document, "positive");
    await saveNow(dom.window);

    assert.equal(header.hasAttribute("data-cycle-stale"), false, "the dropped attribute is gone");
    assert.equal(header.getAttribute("data-dashboard-phase"), "unknown", "a kept attribute still takes the new value");
    assert.equal(header.classList.contains("reveal"), true, "an attribute that is not data-* is left alone");
  } finally {
    dom.window.close();
  }
});

test("a block the result adds or removes is placed in the template's order, without replaying its entrance", async () => {
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
    assert.equal(
      header.querySelectorAll(".reveal").length,
      0,
      "no swapped block, nor anything in one, keeps the entrance class the fetched page carried"
    );
    let present = 0;
    for (const hook of ORDERED_HOOKS) {
      const block = header.querySelector(hook);
      if (!block) {
        continue;
      }
      present += 1;
      assert.equal(block.parentElement, header, `${hook} is a direct child of the header, which the insertion assumes`);
    }
    assert.equal(present, 4, "ribbon, banner, warnings and disclaimer are the blocks this fixture renders");
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

    assert.equal(puts(calls).length, 1, "the mood saved");
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
    assert.equal(document.activeElement.value, "negative", "it is the first visible result, never the hidden carrier");

    await saveNow(dom.window);
    const sent = puts(calls);
    assert.ok(String(sent[sent.length - 1].init.body).includes("pregnancy_test=none"), "the removal is what is saved");
    assert.equal(gets(calls).length, 2, "the removal refreshes the header too");
  } finally {
    dom.window.close();
  }
});

test("undoing a pregnancy result puts the field back to absent, with the header refresh that follows", async () => {
  const { dom, calls } = await loadDashboard({ entryExists: true });
  try {
    const document = dom.window.document;
    pick(document, "positive");
    await saveNow(dom.window);
    assertFieldReads(document, true);

    undoNow(dom.window);
    assertFieldReads(document, false);
    assert.equal(document.querySelector("input[name='pregnancy_test'][value='positive']").checked, false);

    await settleAll();
    const sent = puts(calls);
    assert.equal(sent.length, 2, "the undo saved through the same path");
    assert.ok(String(sent[1].init.body).includes("pregnancy_test=none"), "the unset value is what is saved");
    assert.equal(gets(calls).length, 2, "the undone result refreshes the header too");
    assertFieldReads(document, false);
    assert.equal(field(document).getAttribute("data-pregnancy-test-state"), "absent");
  } finally {
    dom.window.close();
  }
});

test("undoing a Remove puts the recorded result and its Remove action back", async () => {
  const { dom, calls } = await loadDashboard({ recorded: true, entryExists: true });
  try {
    const document = dom.window.document;
    assertFieldReads(document, true);

    field(document).querySelector("[data-pregnancy-test-remove]").click();
    assertFieldReads(document, false);
    await saveNow(dom.window);

    undoNow(dom.window);
    assertFieldReads(document, true);
    assert.equal(document.querySelector("input[name='pregnancy_test'][value='positive']").checked, true);

    await settleAll();
    const sent = puts(calls);
    assert.ok(String(sent[sent.length - 1].init.body).includes("pregnancy_test=positive"), "the restored result is saved");
    assertFieldReads(document, true);
  } finally {
    dom.window.close();
  }
});

const LINK = '<a href="/settings/cycle" data-warning-link>Review your cycle</a>';

test("focus on a link in a swapped block moves to the same link in the new block", async () => {
  const stale = warningsShell(STALE_HEADER, `<div data-dashboard-cycle-warnings><p>Old</p>${LINK}</div>`);
  const fresh = warningsShell(FRESH_HEADER, `<div data-dashboard-cycle-warnings><p>New</p>${LINK}</div>`);
  const { dom } = await loadDashboard({ shellHTML: stale, install: recorderFor(fresh) });
  try {
    const document = dom.window.document;
    const oldLink = document.querySelector("[data-dashboard-cycle-warnings] a");
    oldLink.focus();
    assert.equal(document.activeElement, oldLink);

    pick(document, "positive");
    await saveNow(dom.window);

    const block = document.querySelector("[data-dashboard-cycle-warnings]");
    assert.equal(block.textContent.includes("New"), true, "the block was swapped");
    assert.equal(oldLink.isConnected, false, "the focused node itself was replaced");
    assert.equal(document.activeElement, block.querySelector("a"), "focus is on the equivalent link in the new block");
    assert.equal(document.activeElement.getAttribute("href"), "/settings/cycle");
  } finally {
    dom.window.close();
  }
});

test("focus on one of two links sharing a hook name moves to the link with the same href, not the first", async () => {
  const links =
    '<a href="#dashboard-cycle-start" data-late-cycle-action="cycle-start">Start</a>' +
    '<a href="#dashboard-pregnancy-test" data-late-cycle-action="pregnancy-test">Test</a>';
  const stale = warningsShell(STALE_HEADER, `<div data-dashboard-cycle-warnings><p>Old</p>${links}</div>`);
  const fresh = warningsShell(FRESH_HEADER, `<div data-dashboard-cycle-warnings><p>New</p>${links}</div>`);
  const { dom } = await loadDashboard({ shellHTML: stale, install: recorderFor(fresh) });
  try {
    const document = dom.window.document;
    document.querySelectorAll("[data-dashboard-cycle-warnings] a")[1].focus();

    pick(document, "positive");
    await saveNow(dom.window);

    const block = document.querySelector("[data-dashboard-cycle-warnings]");
    assert.equal(block.textContent.includes("New"), true, "the block was swapped");
    assert.equal(document.activeElement, block.querySelectorAll("a")[1], "focus is on the second link's equivalent");
    assert.equal(document.activeElement.getAttribute("href"), "#dashboard-pregnancy-test");
  } finally {
    dom.window.close();
  }
});

test("a hook with the same name and value finds the link when its href changed, and a different value does not", async () => {
  const stale = warningsShell(
    STALE_HEADER,
    '<div data-dashboard-cycle-warnings><p>Old</p><a href="#a" data-late-cycle-action="cycle-start">A</a><a href="#b" data-late-cycle-action="pregnancy-test">B</a></div>'
  );
  const fresh = warningsShell(
    FRESH_HEADER,
    '<div data-dashboard-cycle-warnings><p>New</p><a href="#x" data-late-cycle-action="cycle-start">A</a><a href="#y" data-late-cycle-action="pregnancy-test">B</a></div>'
  );
  const { dom } = await loadDashboard({ shellHTML: stale, install: recorderFor(fresh) });
  try {
    const document = dom.window.document;
    document.querySelectorAll("[data-dashboard-cycle-warnings] a")[1].focus();

    pick(document, "positive");
    await saveNow(dom.window);

    const block = document.querySelector("[data-dashboard-cycle-warnings]");
    assert.equal(document.activeElement, block.querySelectorAll("a")[1], "the same hook value wins over the first sharer");
  } finally {
    dom.window.close();
  }
});

test("focus with no equivalent in the new block lands on the block, then on the status line if it is gone", async () => {
  const stale = warningsShell(STALE_HEADER, `<div data-dashboard-cycle-warnings><p>Old</p>${LINK}</div>`);
  const noLink = warningsShell(FRESH_HEADER, "<div data-dashboard-cycle-warnings><p>New</p></div>");
  const gone = warningsShell(FRESH_HEADER, "");

  const kept = await loadDashboard({ shellHTML: stale, install: recorderFor(noLink) });
  try {
    const document = kept.dom.window.document;
    document.querySelector("[data-dashboard-cycle-warnings] a").focus();
    pick(document, "positive");
    await saveNow(kept.dom.window);

    const block = document.querySelector("[data-dashboard-cycle-warnings]");
    assert.equal(document.activeElement, block, "the block itself takes focus");
    assert.equal(block.getAttribute("tabindex"), "-1", "focusable by script only, never a tab stop");
    assert.equal(document.querySelector("[data-dashboard-status-line]").hasAttribute("tabindex"), false);

    document.querySelector("[data-usage-goal-chip]").focus();
    assert.notEqual(document.activeElement, block, "focus moved away from the block");
    assert.equal(block.hasAttribute("tabindex"), false, "the block's tabindex leaves no residue once it blurs");
  } finally {
    kept.dom.window.close();
  }

  const dropped = await loadDashboard({ shellHTML: stale, install: recorderFor(gone) });
  try {
    const document = dropped.dom.window.document;
    document.querySelector("[data-dashboard-cycle-warnings] a").focus();
    pick(document, "positive");
    await saveNow(dropped.dom.window);

    assert.equal(document.querySelector("[data-dashboard-cycle-warnings]"), null, "the block is gone");
    assert.equal(
      document.activeElement,
      document.querySelector("[data-dashboard-status-line]"),
      "focus does not fall back to the page body"
    );
  } finally {
    dropped.dom.window.close();
  }
});

test("focus outside the swapped blocks is left alone", async () => {
  const { dom } = await loadDashboard();
  try {
    const document = dom.window.document;
    const summary = document.querySelector("[data-usage-goal-chip]");
    summary.focus();

    pick(document, "positive");
    await saveNow(dom.window);

    assert.equal(document.activeElement, summary);
    assert.equal(summary.hasAttribute("tabindex"), false);
  } finally {
    dom.window.close();
  }
});

test("when two refreshes overlap, the newest answer wins even if the older one lands last", async () => {
  const pending = [];
  const install = (window, calls) => {
    window.fetch = (url, init) => {
      const request = { url: String(url), init: init || {} };
      calls.push(request);
      if ((request.init.method || "GET") !== "GET") {
        return Promise.resolve(okResponse(""));
      }
      return new Promise((resolve) => {
        pending.push((shellHTML) => resolve(okResponse(pageOf(shellHTML))));
      });
    };
  };
  const { dom, calls } = await loadDashboard({ install });
  try {
    const document = dom.window.document;
    pick(document, "positive");
    await saveNow(dom.window);
    pick(document, "negative");
    await saveNow(dom.window);
    assert.equal(gets(calls).length, 2, "two saves, two refreshes in flight");
    assert.equal(pending.length, 2);

    pending[1](shell("Second answer", { line: FRESH_LINE, phase: "unknown" }));
    await settleAll();
    assert.equal(bannerText(document), "Second answer");

    pending[0](shell("First answer", { line: STALE_LINE, phase: "luteal" }));
    await settleAll();
    assert.equal(bannerText(document), "Second answer", "the older response is stale and is not applied");
    assert.equal(document.querySelector("[data-dashboard-status-line]").textContent.trim(), FRESH_LINE);
  } finally {
    dom.window.close();
  }
});
