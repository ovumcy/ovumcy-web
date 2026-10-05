// A day save answers with the server's own sentence about the day, and after a
// positive pregnancy test that sentence carries medical-safety guidance: the
// predictions are paused, and bleeding, pain or dizziness mean seeking care
// promptly. The calendar editor shows it because htmx swaps the fragment into
// its status region. The dashboard journal saves through fetch instead, so the
// sentence reaches the page only if the autosave reads the response and puts
// it there — as text, never as markup from the response.

import test from "node:test";
import assert from "node:assert/strict";
import { readAppBundle, loadDOMWithScript } from "./_helpers.mjs";

const APP_BUNDLE = readAppBundle();

const TODAY = "2026-08-12";
const PAUSED_SENTENCE =
  "Saved. Cycle predictions are paused after a positive pregnancy test. If you experience bleeding, pain, or dizziness, seek medical care promptly.";

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
      data-today-entry-exists="false"
      data-autosave-saving="Saving..."
      data-autosave-saved="Saved"
      data-day-save-failed-text="Couldn't save. Your entry is still here."
      data-day-save-retry-label="Try again">
      <input type="hidden" name="csrf_token" value="unit-test-token">
      <input type="radio" name="pregnancy_test" value="negative">
      <input type="radio" name="pregnancy_test" value="positive">
      <div id="save-status" class="save-status" aria-live="polite"></div>
      <div class="dashboard-autosave-indicator" data-dashboard-autosave-indicator data-autosave-state="idle" aria-live="polite"></div>
    </form>
  </div>
</body></html>`;
}

// The fragment the server sends for an htmx day save (httpx's dismissible
// success markup): the sentence arrives HTML-escaped inside .toast-message.
function statusOKFragment(escapedMessage) {
  return (
    '<div class="status-ok"><div class="toast-body"><span class="toast-message-wrap">' +
    '<span class="toast-icon" aria-hidden="true">✓</span>' +
    `<span class="toast-message">${escapedMessage}</span></span>` +
    '<button type="button" class="toast-close" data-dismiss-status aria-label="Close">×</button>' +
    "</div></div>"
  );
}

function escapeHTML(text) {
  return text
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&#34;")
    .replace(/'/g, "&#39;");
}

async function loadDashboard(responseBody) {
  const calls = [];
  const dom = await loadDOMWithScript(APP_BUNDLE, {
    html: dashboardPage(),
    beforeRun: (window) => {
      window.fetch = (url, init) => {
        calls.push({ url: String(url), init: init || {} });
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: { get: () => null },
          text: () => Promise.resolve(responseBody),
        });
      };
    },
  });
  return { dom, calls };
}

// Mark the positive result and let the runner send it. `pagehide` reaches the
// same runner the 2 s debounce does, without sitting the debounce out.
async function savePositiveTest(window) {
  const positive = window.document.querySelector("input[name='pregnancy_test'][value='positive']");
  positive.checked = true;
  positive.dispatchEvent(new window.Event("change", { bubbles: true }));
  window.dispatchEvent(new window.Event("pagehide"));
  await new Promise((resolve) => setTimeout(resolve, 0));
}

function saveStatus(window) {
  return window.document.querySelector("#save-status");
}

test("a positive-test save shows the server's safety sentence in the dashboard status region", async () => {
  const { dom, calls } = await loadDashboard(statusOKFragment(escapeHTML(PAUSED_SENTENCE)));
  try {
    await savePositiveTest(dom.window);

    assert.equal(calls.length, 1, "the positive result is saved");
    assert.ok(String(calls[0].init.body).includes("pregnancy_test=positive"));
    assert.equal(
      dom.window.document.querySelector("[data-dashboard-autosave-indicator]").getAttribute("data-autosave-state"),
      "saved"
    );

    const message = saveStatus(dom.window).querySelector(".status-ok .toast-message");
    assert.ok(message, "the save's feedback must land in the dashboard's status region");
    assert.equal(
      message.textContent,
      PAUSED_SENTENCE,
      "the red-flag guidance the server composed is shown word for word"
    );
    assert.ok(
      saveStatus(dom.window).querySelector(".status-ok [data-dismiss-status]"),
      "the shown status is the same dismissible one the calendar editor renders"
    );
  } finally {
    dom.window.close();
  }
});

test("a save whose response carries no feedback adds nothing to the status region", async () => {
  for (const body of ["", '{"date":"2026-08-12"}', '<div class="status-ok"><span class="toast-message">  </span></div>']) {
    const { dom, calls } = await loadDashboard(body);
    try {
      await savePositiveTest(dom.window);

      assert.equal(calls.length, 1);
      assert.equal(
        saveStatus(dom.window).childNodes.length,
        0,
        `a response without a sentence must not add anything (body ${JSON.stringify(body)})`
      );
    } finally {
      dom.window.close();
    }
  }
});

test("markup in the feedback renders as text, never as elements", async () => {
  const escaped = 'Saved. <img src="x" data-injected="escaped">';
  const raw = 'Saved. <img src="x" data-injected="raw"><b data-injected="raw">bold</b>';
  const cases = [
    // What the server sends: the sentence escaped once, so it reads as characters.
    { body: statusOKFragment(escapeHTML(escaped)), text: escaped },
    // A regression that let unescaped markup into the fragment: only its text
    // may be adopted.
    { body: statusOKFragment(raw), text: "Saved. bold" },
  ];
  for (const { body, text } of cases) {
    const { dom } = await loadDashboard(body);
    try {
      await savePositiveTest(dom.window);

      const region = saveStatus(dom.window);
      assert.equal(region.querySelector(".status-ok .toast-message").textContent, text);
      assert.equal(region.querySelector("[data-injected]"), null, "no element from the response reaches the page");
      assert.equal(region.querySelector("img, b"), null);
    } finally {
      dom.window.close();
    }
  }
});
