// A day-entry save that never lands must not cost the owner what they typed.
// The recovery mechanism is deliberately the live form and nothing else: no
// draft in localStorage, no offline cache, no service worker — day entries are
// health data, and persisting them client-side is a privacy decision this layer
// does not get to take on its own.
//
// These tests pin the four properties of the failure path against the shipped
// bundle: the entry survives, no success is announced, the notice is the
// neutral one rather than the red error surface, and the retry resubmits the
// same form node.

import test from "node:test";
import assert from "node:assert/strict";
import { readAppBundle, loadDOMWithScript } from "./_helpers.mjs";

const APP_BUNDLE = readAppBundle();

const TYPED_NOTE = "cramps since the afternoon";
const FAILED_TEXT = "Couldn't save — check your connection. Your entry is still here.";
const RETRY_LABEL = "Try again";

const PAGE = `<!doctype html><html><head></head><body>
  <form
    hx-put="/api/v1/days/2026-08-11"
    data-save-feedback
    data-day-editor-form
    data-day-editor-date="2026-08-11"
    data-day-save-failed-text="${FAILED_TEXT}"
    data-day-save-retry-label="${RETRY_LABEL}">
    <input type="checkbox" name="is_period" value="true" checked>
    <textarea id="calendar-notes" name="notes">${TYPED_NOTE}</textarea>
    <button type="submit" data-save-button>Save</button>
    <div id="calendar-save-status" class="save-status" aria-live="polite"></div>
  </form>
</body></html>`;

function dayEditorForm(window) {
  return window.document.querySelector("[data-day-editor-form]");
}

function fireOnForm(window, name, detail) {
  const form = dayEditorForm(window);
  form.dispatchEvent(new window.CustomEvent(name, { detail, bubbles: true }));
  return form;
}

function failureNotice(window) {
  return window.document.querySelector("#calendar-save-status [data-day-save-failed]");
}

function assertEntryIntact(window) {
  const form = dayEditorForm(window);
  assert.equal(
    form.querySelector("#calendar-notes").value,
    TYPED_NOTE,
    "the typed note must survive the error path untouched"
  );
  assert.equal(
    form.querySelector("input[name='is_period']").checked,
    true,
    "a checked field must survive the error path untouched"
  );
}

test("a transport failure renders the neutral retry notice and keeps the entry", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: PAGE });
  try {
    fireOnForm(dom.window, "htmx:sendError", { xhr: {} });

    const notice = failureNotice(dom.window);
    assert.ok(notice, "the failure path must render a notice into the save-status container");
    assert.equal(notice.getAttribute("data-day-save-failed"), "unreachable");
    assert.ok(
      notice.textContent.includes(FAILED_TEXT),
      "the notice shows the localized copy the template supplied"
    );

    assertEntryIntact(dom.window);
  } finally {
    dom.window.close();
  }
});

test("a failed save never announces a success", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: PAGE });
  try {
    fireOnForm(dom.window, "htmx:sendError", { xhr: {} });

    assert.equal(
      dom.window.document.querySelectorAll("#calendar-save-status .status-ok").length,
      0,
      "no success status may appear for a save the server never confirmed"
    );
  } finally {
    dom.window.close();
  }
});

test("the failure notice is the neutral surface, not the error one", async () => {
  // A red alert on a health surface reads as a finding about the owner's body.
  // A save that did not reach a self-hosted server is a transport event, and
  // the markup has to say so.
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: PAGE });
  try {
    fireOnForm(dom.window, "htmx:sendError", { xhr: {} });

    const notice = failureNotice(dom.window);
    assert.ok(notice.classList.contains("status-notice"));
    assert.equal(notice.classList.contains("status-error"), false);
    assert.equal(
      dom.window.document.querySelectorAll("#calendar-save-status .status-error").length,
      0,
      "the day-entry failure path must not fall back to the red error block"
    );
    assert.equal(
      dom.window.document.querySelector("#calendar-save-status").getAttribute("aria-live"),
      "polite",
      "the notice lands in the existing polite live region"
    );
  } finally {
    dom.window.close();
  }
});

test("the retry resubmits the same form node with the same values", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: PAGE });
  try {
    const form = fireOnForm(dom.window, "htmx:sendError", { xhr: {} });

    let submittedNote = null;
    let submissions = 0;
    form.requestSubmit = () => {
      submissions += 1;
      submittedNote = form.querySelector("#calendar-notes").value;
    };

    const retry = dom.window.document.querySelector("#calendar-save-status [data-day-save-retry]");
    assert.ok(retry, "the failure notice carries a visible retry control");
    assert.equal(retry.type, "button", "the retry must not be a second submit button inside the form");
    assert.equal(retry.textContent, RETRY_LABEL);

    retry.dispatchEvent(new dom.window.MouseEvent("click", { bubbles: true, cancelable: true }));

    // Not an exact count: this harness delivers DOMContentLoaded twice (jsdom's
    // own plus the one _helpers.mjs dispatches), so every listener the bundle
    // installs on document ready is registered twice here. "Exactly one PUT per
    // retry" is pinned in the browser instead —
    // e2e/calendar-save-resilience.spec.ts counts the intercepted attempts.
    assert.ok(submissions > 0, "the retry resubmits the form");
    assert.equal(submittedNote, TYPED_NOTE, "the retry carries what is on screen, unchanged");
    assertEntryIntact(dom.window);
  } finally {
    dom.window.close();
  }
});

test("a rejected save shows the server's own message and still offers a retry", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: PAGE });
  try {
    fireOnForm(dom.window, "htmx:responseError", {
      target: dom.window.document.getElementById("calendar-save-status"),
      xhr: {
        responseText:
          '<div class="status-error" data-flash-key="error.invalid_payload">That temperature is out of range.</div>',
      },
    });

    const notice = failureNotice(dom.window);
    assert.ok(notice, "a rejected save renders the same notice shape");
    assert.equal(notice.getAttribute("data-day-save-failed"), "rejected");
    assert.ok(notice.textContent.includes("That temperature is out of range."));
    assert.equal(
      notice.querySelector("[data-notice-key]").getAttribute("data-notice-key"),
      "error.invalid_payload",
      "the server's stable key travels with its message"
    );
    assert.ok(notice.querySelector("[data-day-save-retry]"), "a rejected save is retryable too");
    assertEntryIntact(dom.window);
  } finally {
    dom.window.close();
  }
});

test("a rejected save never scripts the server response into the page", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: PAGE });
  try {
    dom.window.__unitTestXSSCanary = () => {
      dom.window.__unitTestXSSFired = true;
    };

    fireOnForm(dom.window, "htmx:responseError", {
      target: dom.window.document.getElementById("calendar-save-status"),
      xhr: {
        responseText:
          '<div class="status-error">prefix<script>window.__unitTestXSSCanary()</script><img src=x onerror="window.__attrXSSFired=true">suffix</div>',
      },
    });

    const container = dom.window.document.getElementById("calendar-save-status");
    assert.equal(container.querySelectorAll("script").length, 0);
    assert.equal(container.querySelectorAll("img").length, 0);
    assert.equal(dom.window.__unitTestXSSFired, undefined);
    assert.equal(dom.window.__attrXSSFired, undefined);
  } finally {
    dom.window.close();
  }
});

test("a failed save writes no draft to client storage", async () => {
  // The hard boundary: the form surviving in the DOM is the whole mechanism.
  // If a future "helpful" draft cache lands, this test is the one that says no.
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: PAGE });
  try {
    fireOnForm(dom.window, "htmx:sendError", { xhr: {} });
    const retry = dom.window.document.querySelector("#calendar-save-status [data-day-save-retry]");
    dom.window.document.querySelector("#calendar-notes").value = TYPED_NOTE;
    retry.dispatchEvent(new dom.window.MouseEvent("click", { bubbles: true, cancelable: true }));

    for (const storage of [dom.window.localStorage, dom.window.sessionStorage]) {
      for (let index = 0; index < storage.length; index += 1) {
        const key = storage.key(index);
        assert.equal(
          String(storage.getItem(key)).includes(TYPED_NOTE),
          false,
          `no day-entry text may be persisted client-side (found under "${key}")`
        );
      }
    }
  } finally {
    dom.window.close();
  }
});
