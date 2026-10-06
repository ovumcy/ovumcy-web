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

// The dashboard journal saves itself, so its failures arrive on the fetch path
// rather than as htmx events — but the surface the owner sees must be the same
// one, or "a save that did not land" would mean two different things on two
// screens. Same fixture shape, same notice, same retry.
const DASHBOARD_DATE = "2026-08-12";
const DASHBOARD_PAGE = `<!doctype html><html><head><meta name="csrf-token" content="unit-test-token"></head><body>
  <div data-dashboard-editor>
    <form
      hx-put="/api/v1/days/${DASHBOARD_DATE}"
      hx-target="#save-status"
      hx-swap="innerHTML"
      data-save-feedback
      data-dashboard-save-form
      data-dashboard-date="${DASHBOARD_DATE}"
      data-today-entry-exists="true"
      data-autosave-clear-url="/api/v1/days/${DASHBOARD_DATE}?source=dashboard"
      data-autosave-saving="Saving..."
      data-autosave-saved="Saved"
      data-autosave-invalid="Fix the form errors to save"
      data-autosave-undo="Undo"
      data-day-save-failed-text="${FAILED_TEXT}"
      data-day-save-retry-label="${RETRY_LABEL}">
      <input type="hidden" name="csrf_token" value="unit-test-token">
      <input type="checkbox" name="is_period" value="true" checked>
      <textarea id="today-notes" name="notes" data-dashboard-notes>${TYPED_NOTE}</textarea>
      <div id="save-status" class="save-status" aria-live="polite"></div>
      <div class="dashboard-autosave-indicator" data-dashboard-autosave-indicator data-autosave-state="idle" aria-live="polite"></div>
    </form>
  </div>
</body></html>`;

function dayEditorForm(window) {
  return window.document.querySelector("[data-day-editor-form]");
}

function dashboardSaveForm(window) {
  return window.document.querySelector("[data-dashboard-save-form]");
}

function dashboardFailureNotice(window) {
  return window.document.querySelector("#save-status [data-day-save-failed]");
}

function assertDashboardEntryIntact(window) {
  const form = dashboardSaveForm(window);
  assert.equal(
    form.querySelector("#today-notes").value,
    TYPED_NOTE,
    "the typed note must survive the error path untouched"
  );
  assert.equal(form.querySelector("input[name='is_period']").checked, true);
}

/** Loads the dashboard fixture with a fetch that fails the given way. */
async function loadDashboardWithFailingSave(outcome) {
  const calls = [];
  let mode = outcome;
  const dom = await loadDOMWithScript(APP_BUNDLE, {
    html: DASHBOARD_PAGE,
    beforeRun: (window) => {
      window.fetch = (url, init) => {
        calls.push({ url: String(url), init: init || {} });
        if (mode === "offline") {
          return Promise.reject(new Error("network down"));
        }
        if (mode === "rejected") {
          return Promise.resolve({
            ok: false,
            status: 422,
            headers: { get: () => null },
            text: () =>
              Promise.resolve(
                '<div class="status-error" data-flash-key="error.invalid_payload">That temperature is out of range.</div>'
              ),
          });
        }
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: { get: () => null },
          text: () => Promise.resolve(""),
        });
      };
    },
  });

  return {
    dom,
    calls,
    set: (next) => {
      mode = next;
    },
  };
}

// The unload flush runs the same runner the 2 s debounce does, so the request
// is reached without sitting out the debounce.
async function attemptDashboardSave(window) {
  const notes = window.document.querySelector("#today-notes");
  notes.dispatchEvent(new window.Event("input", { bubbles: true }));
  window.dispatchEvent(new window.Event("pagehide"));
  await new Promise((resolve) => setTimeout(resolve, 0));
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

test("an autosave that never lands shows the same neutral notice on the dashboard", async () => {
  const { dom, calls } = await loadDashboardWithFailingSave("offline");
  try {
    await attemptDashboardSave(dom.window);
    assert.equal(calls.length, 1, "the autosave must have reached the wire");

    const notice = dashboardFailureNotice(dom.window);
    assert.ok(notice, "a failed autosave is never a silent state attribute");
    assert.equal(notice.getAttribute("data-day-save-failed"), "unreachable");
    assert.ok(notice.textContent.includes(FAILED_TEXT));
    assert.ok(notice.classList.contains("status-notice"));
    assert.equal(notice.classList.contains("status-error"), false);
    assert.equal(dom.window.document.querySelectorAll("#save-status .status-ok").length, 0);
    assert.equal(
      dom.window.document.querySelector("#save-status").getAttribute("aria-live"),
      "polite"
    );
    assert.ok(notice.querySelector("[data-day-save-retry]"), "the retry rides with the notice");
    assertDashboardEntryIntact(dom.window);
  } finally {
    dom.window.close();
  }
});

test("the dashboard retry re-enters the autosave with what is on screen", async () => {
  const failing = await loadDashboardWithFailingSave("offline");
  const { dom, calls } = failing;
  try {
    await attemptDashboardSave(dom.window);
    assert.equal(calls.length, 1);

    failing.set("ok");
    dom.window.document
      .querySelector("#save-status [data-day-save-retry]")
      .dispatchEvent(new dom.window.MouseEvent("click", { bubbles: true, cancelable: true }));
    await new Promise((resolve) => setTimeout(resolve, 0));

    // Not an exact count: this harness delivers DOMContentLoaded twice, so the
    // click can reach more than one copy of the same listener. "Exactly one PUT
    // per retry" is pinned in the browser instead.
    assert.ok(calls.length > 1, "the retry sends the entry again");
    const retried = calls[calls.length - 1];
    assert.equal(retried.url, `/api/v1/days/${DASHBOARD_DATE}`);
    assert.equal(retried.init.method, "PUT");
    assert.ok(
      String(retried.init.body || "").includes(
        new URLSearchParams([["notes", TYPED_NOTE]]).toString()
      ),
      "the retry carries exactly what is on screen"
    );
    assert.equal(dashboardFailureNotice(dom.window), null, "a landed save clears the notice");
    assertDashboardEntryIntact(dom.window);
  } finally {
    dom.window.close();
  }
});

test("a rejected autosave shows the server's own message on the dashboard too", async () => {
  const { dom } = await loadDashboardWithFailingSave("rejected");
  try {
    await attemptDashboardSave(dom.window);

    const notice = dashboardFailureNotice(dom.window);
    assert.ok(notice);
    assert.equal(notice.getAttribute("data-day-save-failed"), "rejected");
    assert.ok(notice.textContent.includes("That temperature is out of range."));
    assert.equal(
      notice.querySelector("[data-notice-key]").getAttribute("data-notice-key"),
      "error.invalid_payload"
    );
    assertDashboardEntryIntact(dom.window);
  } finally {
    dom.window.close();
  }
});

test("an htmx-reported failure on the dashboard form renders the same notice", async () => {
  // The dashboard form still declares hx-put — the autosave runner and the e2e
  // helpers read it — so an htmx-driven attempt (implicit submission, a legacy
  // path) must land on the same surface rather than the generic error block.
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: DASHBOARD_PAGE });
  try {
    dashboardSaveForm(dom.window).dispatchEvent(
      new dom.window.CustomEvent("htmx:sendError", { detail: { xhr: {} }, bubbles: true })
    );

    const notice = dashboardFailureNotice(dom.window);
    assert.ok(notice, "one failure surface, both forms");
    assert.equal(notice.getAttribute("data-day-save-failed"), "unreachable");
    assertDashboardEntryIntact(dom.window);
  } finally {
    dom.window.close();
  }
});

// An explicit Save goes out on htmx's XMLHttpRequest, which the browser cancels
// with the page. Leaving while it is open asks first and sends nothing yet: an
// owner who cancels and stays must not have an older body on the wire,
// unordered against the saves they make next. Only once the page is really
// going (`pagehide`) does the very body that request carries leave on a
// keepalive request — once.
test("a calendar save still on the wire is re-sent with keepalive only once the leave is confirmed", async () => {
  const calls = [];
  const dom = await loadDOMWithScript(APP_BUNDLE, {
    html: PAGE,
    beforeRun: (window) => {
      window.fetch = (url, init) => {
        calls.push({ url: String(url), init: init || {} });
        return Promise.resolve({ ok: true, status: 200, headers: { get: () => null } });
      };
    },
  });
  const leave = () => {
    const event = new dom.window.Event("beforeunload", { cancelable: true });
    dom.window.dispatchEvent(event);
    return event.defaultPrevented;
  };
  const pageHide = () => {
    dom.window.dispatchEvent(new dom.window.Event("pagehide"));
  };
  try {
    const form = dayEditorForm(dom.window);
    const binding = dom.window.document.createElement("input");
    binding.type = "hidden";
    binding.name = "day_form_account";
    binding.value = "unit-test-unload-binding";
    form.insertBefore(binding, form.firstChild);
    fireOnForm(dom.window, "htmx:beforeSend", {
      requestConfig: {
        elt: form,
        verb: "put",
        path: "/api/v1/days/2026-08-11",
        headers: { "HX-Request": "true", "X-CSRF-Token": "unit-test-token" },
        formData: new dom.window.FormData(form),
      },
    });
    // What the request carries is what goes out again, not a later edit.
    form.querySelector("#calendar-notes").value = "typed after Save";

    assert.equal(leave(), true, "leaving while the save is open asks first");
    assert.equal(calls.length, 0, "nothing is sent before the owner answers the leave prompt");

    // The owner cancelled and stayed; the next leave goes through.
    assert.equal(leave(), true);
    assert.equal(calls.length, 0, "a cancelled leave sends nothing");
    pageHide();
    assert.equal(calls.length, 1, "the open save is re-sent on a request that outlives the page");
    assert.equal(calls[0].url, "/api/v1/days/2026-08-11");
    assert.equal(calls[0].init.method, "PUT");
    assert.equal(calls[0].init.keepalive, true);
    assert.equal(calls[0].init.headers["X-CSRF-Token"], "unit-test-token");
    assert.ok(
      String(calls[0].init.body).includes(new URLSearchParams([["notes", TYPED_NOTE]]).toString()),
      "the re-sent body is the one the open request carries"
    );
    assert.deepEqual(
      new URLSearchParams(String(calls[0].init.body)).getAll("day_form_account"),
      ["unit-test-unload-binding"],
      "the re-sent body still names the account the form was rendered for, or the server refuses it"
    );
    assert.ok(
      !String(calls[0].init.body).includes("typed+after+Save"),
      "an edit typed after Save and never saved is not sent behind the owner's back"
    );

    // A page restored from the back/forward cache fires pagehide again.
    pageHide();
    assert.equal(calls.length, 1, "the same save is not sent twice");

    fireOnForm(dom.window, "htmx:afterRequest", { xhr: {} });
    assert.equal(leave(), false, "a finished save leaves nothing to re-send or warn about");
    assert.equal(calls.length, 1);
  } finally {
    dom.window.close();
  }
});

// A session that ended while the owner was typing answers the save with the
// auth guard's 401 fragment. Retrying in place cannot fix that, and signing in
// in this tab would navigate away from the only copy of the entry, so the
// notice offers the sign-in page in a NEW tab and keeps the retry beside it.
const SESSION_EXPIRED_KEY = "common.error.unauthorized";
const SESSION_EXPIRED_FRAGMENT = `<div class="status-error" data-flash-key="${SESSION_EXPIRED_KEY}" data-flash-status="error">Vous n'êtes pas connecté.</div>`;
const SIGN_IN_LABEL = "Se connecter dans un nouvel onglet";

function withSignInLabel(html) {
  return html.replaceAll(
    `data-day-save-retry-label="${RETRY_LABEL}"`,
    `data-day-save-retry-label="${RETRY_LABEL}" data-day-save-sign-in-label="${SIGN_IN_LABEL}"`
  );
}

function assertSignInLink(notice, surface) {
  const link = notice.querySelector("a[data-day-save-sign-in]");
  assert.ok(link, `${surface}: a session-expired refusal must offer a way to sign in`);
  assert.equal(link.getAttribute("href"), "/login", `${surface}: the link is the fixed same-origin sign-in path`);
  assert.equal(link.getAttribute("target"), "_blank", `${surface}: signing in here would navigate away from the entry`);
  assert.equal(link.getAttribute("rel"), "noopener", `${surface}: the sign-in tab gets no handle on this page`);
  assert.equal(link.textContent, SIGN_IN_LABEL, `${surface}: the link reads the localized label the template supplied`);
  assert.ok(notice.querySelector("[data-day-save-retry]"), `${surface}: the retry stays beside the link`);
}

function assertNothingStored(window) {
  for (const storage of [window.localStorage, window.sessionStorage]) {
    assert.equal(storage.length, 0, "nothing may be stashed client-side to survive the sign-in");
  }
  assert.equal(window.document.cookie, "", "nothing may be stashed in a cookie either");
}

test("a session-expired calendar save offers sign-in in a new tab and keeps the entry", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: withSignInLabel(PAGE) });
  try {
    fireOnForm(dom.window, "htmx:responseError", {
      target: dom.window.document.getElementById("calendar-save-status"),
      xhr: { status: 401, responseText: SESSION_EXPIRED_FRAGMENT },
    });

    const notice = failureNotice(dom.window);
    assert.ok(notice);
    assertSignInLink(notice, "calendar");
    assertEntryIntact(dom.window);
    assertNothingStored(dom.window);
  } finally {
    dom.window.close();
  }
});

test("a session-expired autosave offers sign-in, and the retry after it re-sends the entry", async () => {
  const failing = await loadDashboardWithFailingSave("ok");
  const { dom, calls } = failing;
  try {
    // The fixture's fetch answers the 401 with the auth guard's fragment.
    dom.window.fetch = (url, init) => {
      calls.push({ url: String(url), init: init || {} });
      return Promise.resolve({
        ok: false,
        status: 401,
        headers: { get: () => null },
        text: () => Promise.resolve(SESSION_EXPIRED_FRAGMENT),
      });
    };
    dashboardSaveForm(dom.window).setAttribute("data-day-save-sign-in-label", SIGN_IN_LABEL);
    await attemptDashboardSave(dom.window);

    const notice = dashboardFailureNotice(dom.window);
    assert.ok(notice, "the 401 renders the failure notice");
    assertSignInLink(notice, "dashboard");
    assertDashboardEntryIntact(dom.window);
    assertNothingStored(dom.window);

    // Signed in again in the other tab: the same page's retry now lands, with
    // the entry on screen and the CSRF token the page holds.
    dom.window.fetch = (url, init) => {
      calls.push({ url: String(url), init: init || {} });
      return Promise.resolve({ ok: true, status: 200, headers: { get: () => null }, text: () => Promise.resolve("") });
    };
    const before = calls.length;
    notice
      .querySelector("[data-day-save-retry]")
      .dispatchEvent(new dom.window.MouseEvent("click", { bubbles: true, cancelable: true }));
    await new Promise((resolve) => setTimeout(resolve, 0));

    assert.ok(calls.length > before, "the retry sends the entry again");
    const retried = calls[calls.length - 1];
    assert.ok(String(retried.init.body || "").includes(new URLSearchParams([["notes", TYPED_NOTE]]).toString()));
    assert.equal(retried.init.headers["X-CSRF-Token"], "unit-test-token");
    assert.equal(dashboardFailureNotice(dom.window), null, "a landed save clears the notice");
  } finally {
    dom.window.close();
  }
});

test("only a session-expired refusal offers the sign-in link", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: withSignInLabel(PAGE) });
  try {
    fireOnForm(dom.window, "htmx:responseError", {
      target: dom.window.document.getElementById("calendar-save-status"),
      xhr: {
        responseText:
          '<div class="status-error" data-flash-key="error.invalid_payload">That temperature is out of range.</div>',
      },
    });
    assert.equal(failureNotice(dom.window).querySelector("[data-day-save-sign-in]"), null);

    fireOnForm(dom.window, "htmx:sendError", { xhr: {} });
    assert.equal(failureNotice(dom.window).querySelector("[data-day-save-sign-in]"), null);
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

// If the account signed in from the other tab is not the one the form was
// rendered for, the server refuses the save with 409 rather than write the
// entry into that account. Retrying cannot land it and signing in again is not
// what is missing, so the notice offers neither — and the entry stays put.
const ACCOUNT_CHANGED_KEY = "daylog.save_account_changed";
const ACCOUNT_CHANGED_TEXT = "You're now signed in to a different account. This entry was not saved.";
const ACCOUNT_CHANGED_FRAGMENT = `<div class="status-error" data-flash-key="${ACCOUNT_CHANGED_KEY}" data-flash-status="error">${ACCOUNT_CHANGED_TEXT}</div>`;
const FORM_ACCOUNT = "opaque-binding-of-the-rendering-account";

function assertAccountChangedNotice(notice, surface) {
  assert.ok(notice, `${surface}: the 409 renders a notice`);
  assert.equal(notice.textContent, ACCOUNT_CHANGED_TEXT, `${surface}: the notice reads the server's localized copy`);
  assert.equal(notice.querySelector("[data-day-save-retry]"), null, `${surface}: a retry cannot land, so none is offered`);
  assert.equal(notice.querySelector("[data-day-save-sign-in]"), null, `${surface}: signing in is not what is missing`);
}

test("a calendar save refused for another account offers no retry and keeps the entry", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: withSignInLabel(PAGE) });
  try {
    fireOnForm(dom.window, "htmx:responseError", {
      target: dom.window.document.getElementById("calendar-save-status"),
      xhr: { status: 409, responseText: ACCOUNT_CHANGED_FRAGMENT },
    });

    assertAccountChangedNotice(failureNotice(dom.window), "calendar");
    assertEntryIntact(dom.window);
    assertNothingStored(dom.window);
  } finally {
    dom.window.close();
  }
});

test("a dashboard save carries the form's account binding and a 409 for another account offers no retry", async () => {
  const failing = await loadDashboardWithFailingSave("ok");
  const { dom, calls } = failing;
  try {
    const form = dashboardSaveForm(dom.window);
    form.setAttribute("data-day-save-sign-in-label", SIGN_IN_LABEL);
    const binding = dom.window.document.createElement("input");
    binding.type = "hidden";
    binding.name = "day_form_account";
    binding.value = FORM_ACCOUNT;
    form.insertBefore(binding, form.firstChild);

    dom.window.fetch = (url, init) => {
      calls.push({ url: String(url), init: init || {} });
      return Promise.resolve({
        ok: false,
        status: 409,
        headers: { get: () => null },
        text: () => Promise.resolve(ACCOUNT_CHANGED_FRAGMENT),
      });
    };
    const before = calls.length;
    await attemptDashboardSave(dom.window);

    assert.ok(calls.length > before, "the save went out");
    const sent = new URLSearchParams(String(calls[calls.length - 1].init.body || ""));
    assert.deepEqual(sent.getAll("day_form_account"), [FORM_ACCOUNT], "the body names the account the form was rendered for");

    assertAccountChangedNotice(dashboardFailureNotice(dom.window), "dashboard");
    assertDashboardEntryIntact(dom.window);
    assertNothingStored(dom.window);
  } finally {
    dom.window.close();
  }
});
