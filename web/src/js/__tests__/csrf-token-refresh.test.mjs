// A day save that sat behind a sign-in in another tab can come back to a CSRF
// token the server no longer knows: the token idled out, or the process
// restarted, and the other tab's first page load minted a new one. This tab's
// <meta> would keep the old token forever, so every retry and every later
// autosave would be refused 403 until a reload that discards the entry.
//
// These tests pin the recovery against the shipped bundle: a refused day write
// re-reads the token from the page the owner is on, adopts only the <meta>
// token from a same-origin answer, never re-sends the write itself, and leaves
// everything as it was when the refresh does not work.

import test from "node:test";
import assert from "node:assert/strict";
import { readAppBundle, loadDOMWithScript } from "./_helpers.mjs";

const APP_BUNDLE = readAppBundle();

const STALE_TOKEN = "stale-token-0000000000";
const FRESH_TOKEN = "fresh-token-1111111111";
const PAGE_URL = "https://ovumcy.test/calendar";
const TYPED_NOTE = "cramps since the afternoon";
const FORBIDDEN_FRAGMENT =
  '<div class="status-error" data-flash-key="common.error.forbidden">That action was refused. Reload the page and try again.</div>';
const OWN_BINDING = "binding-of-the-rendering-account";

const PAGE = `<!doctype html><html><head><meta name="csrf-token" content="${STALE_TOKEN}"></head><body>
  <form
    hx-put="/api/v1/days/2026-08-11"
    data-save-feedback
    data-day-editor-form
    data-day-editor-date="2026-08-11"
    data-day-save-failed-text="Couldn't save."
    data-day-save-retry-label="Try again">
    <input type="hidden" name="csrf_token" value="${STALE_TOKEN}">
    <input type="hidden" name="day_form_account" value="${OWN_BINDING}">
    <textarea id="calendar-notes" name="notes">${TYPED_NOTE}</textarea>
    <button type="submit" data-save-button>Save</button>
    <div id="calendar-save-status" class="save-status" aria-live="polite"></div>
  </form>
</body></html>`;

const DASHBOARD_PAGE = `<!doctype html><html><head><meta name="csrf-token" content="${STALE_TOKEN}"></head><body>
  <div data-dashboard-editor>
    <form
      hx-put="/api/v1/days/2026-08-12"
      hx-target="#save-status"
      hx-swap="innerHTML"
      data-save-feedback
      data-dashboard-save-form
      data-dashboard-date="2026-08-12"
      data-today-entry-exists="true"
      data-autosave-clear-url="/api/v1/days/2026-08-12?source=dashboard"
      data-autosave-saving="Saving..."
      data-autosave-saved="Saved"
      data-autosave-invalid="Fix the form errors to save"
      data-autosave-undo="Undo"
      data-day-save-failed-text="Couldn't save."
      data-day-save-retry-label="Try again">
      <input type="hidden" name="day_form_account" value="${OWN_BINDING}">
      <textarea id="today-notes" name="notes" data-dashboard-notes>${TYPED_NOTE}</textarea>
      <div id="save-status" class="save-status" aria-live="polite"></div>
      <div class="dashboard-autosave-indicator" data-dashboard-autosave-indicator data-autosave-state="idle" aria-live="polite"></div>
    </form>
  </div>
</body></html>`;

function pageWithToken(token, extra = "") {
  return `<!doctype html><html><head><meta name="csrf-token" content="${token}"></head><body>${extra}</body></html>`;
}

function okPage(text, url = PAGE_URL) {
  return { ok: true, status: 200, url, headers: { get: () => null }, text: () => Promise.resolve(text) };
}

const flush = () => new Promise((resolve) => setTimeout(resolve, 0));

function currentToken(window) {
  return window.document.querySelector('meta[name="csrf-token"]').getAttribute("content");
}

function dayForm(window) {
  return window.document.querySelector("[data-day-editor-form]");
}

function fireResponseError(window, xhr, pathInfo = { requestPath: "/api/v1/days/2026-08-11" }) {
  dayForm(window).dispatchEvent(
    new window.CustomEvent("htmx:responseError", {
      detail: { xhr, target: window.document.getElementById("calendar-save-status"), pathInfo },
      bubbles: true,
    })
  );
}

function notice(window) {
  return window.document.querySelector("#calendar-save-status [data-day-save-failed]");
}

/** Loads the page with a fetch whose every answer comes from `answer(url, init)`. */
async function load(answer, { html = PAGE } = {}) {
  const calls = [];
  const dom = await loadDOMWithScript(APP_BUNDLE, {
    html,
    url: PAGE_URL,
    beforeRun: (window) => {
      window.fetch = (url, init) => {
        calls.push({ url: String(url), init: init || {} });
        return answer(String(url), init || {});
      };
    },
  });
  return { dom, calls };
}

test("a 403 on a day save re-reads the token from the current page and keeps the entry", async () => {
  const { dom, calls } = await load(() => Promise.resolve(okPage(pageWithToken(FRESH_TOKEN))));
  try {
    fireResponseError(dom.window, { status: 403, responseText: FORBIDDEN_FRAGMENT });
    await flush();

    assert.equal(calls.length, 1, "exactly one refresh per refused write");
    assert.equal(calls[0].url, "/calendar", "the current path, no query, no origin of its own");
    assert.equal(calls[0].init.method, "GET");
    assert.equal(calls[0].init.credentials, "same-origin");
    assert.equal(currentToken(dom.window), FRESH_TOKEN);
    assert.equal(
      dom.window.document.querySelector('input[name="csrf_token"]').value,
      FRESH_TOKEN,
      "the hidden token input mirrors the meta"
    );

    assert.ok(notice(dom.window), "the failure notice is still rendered");
    assert.ok(notice(dom.window).querySelector("[data-day-save-retry]"), "with its Retry");
    assert.equal(dayForm(dom.window).querySelector("#calendar-notes").value, TYPED_NOTE);
  } finally {
    dom.window.close();
  }
});

test("the refresh never re-sends the write by itself", async () => {
  const { dom, calls } = await load(() => Promise.resolve(okPage(pageWithToken(FRESH_TOKEN))));
  try {
    let submissions = 0;
    dayForm(dom.window).requestSubmit = () => {
      submissions += 1;
    };
    fireResponseError(dom.window, { status: 403, responseText: FORBIDDEN_FRAGMENT });
    await flush();

    assert.equal(submissions, 0, "only the owner's Retry submits again");
    assert.deepEqual(
      calls.map((call) => call.init.method),
      ["GET"],
      "the only request is the token refresh, never a write"
    );
  } finally {
    dom.window.close();
  }
});

test("the Retry after a refresh sends the fresh token", async () => {
  const { dom } = await load(() => Promise.resolve(okPage(pageWithToken(FRESH_TOKEN))));
  try {
    fireResponseError(dom.window, { status: 403, responseText: FORBIDDEN_FRAGMENT });
    await flush();

    let submissions = 0;
    dayForm(dom.window).requestSubmit = () => {
      submissions += 1;
    };
    notice(dom.window)
      .querySelector("[data-day-save-retry]")
      .dispatchEvent(new dom.window.MouseEvent("click", { bubbles: true, cancelable: true }));
    assert.ok(submissions > 0, "Retry resubmits the form");

    const detail = { verb: "put", parameters: {}, headers: {} };
    dom.window.document.body.dispatchEvent(new dom.window.CustomEvent("htmx:configRequest", { detail }));
    assert.equal(detail.headers["X-CSRF-Token"], FRESH_TOKEN);
    assert.equal(detail.parameters.csrf_token, FRESH_TOKEN);
  } finally {
    dom.window.close();
  }
});

test("a Retry pressed before the refresh settles waits for it", async () => {
  let release;
  const gate = new Promise((resolve) => {
    release = resolve;
  });
  const { dom } = await load(() => gate);
  try {
    fireResponseError(dom.window, { status: 403, responseText: FORBIDDEN_FRAGMENT });
    await flush();

    let submissions = 0;
    dayForm(dom.window).requestSubmit = () => {
      submissions += 1;
    };
    notice(dom.window)
      .querySelector("[data-day-save-retry]")
      .dispatchEvent(new dom.window.MouseEvent("click", { bubbles: true, cancelable: true }));
    await flush();
    assert.equal(submissions, 0, "nothing is sent while the token is still stale");

    release(okPage(pageWithToken(FRESH_TOKEN)));
    await flush();
    assert.ok(submissions > 0, "the Retry goes out once the refresh has settled");
    assert.equal(currentToken(dom.window), FRESH_TOKEN, "and it goes out under the fresh token");
  } finally {
    dom.window.close();
  }
});

test("a second refusal while a refresh is in flight shares it", async () => {
  let release;
  const gate = new Promise((resolve) => {
    release = resolve;
  });
  const { dom, calls } = await load(() => gate);
  try {
    fireResponseError(dom.window, { status: 403, responseText: FORBIDDEN_FRAGMENT });
    fireResponseError(dom.window, { status: 403, responseText: FORBIDDEN_FRAGMENT });
    await flush();
    assert.equal(calls.length, 1);
    release(okPage(pageWithToken(FRESH_TOKEN)));
    await flush();
  } finally {
    dom.window.close();
  }
});

test("a refresh that fails changes nothing", async () => {
  const outcomes = {
    "network down": () => Promise.reject(new Error("network down")),
    "server error": () => Promise.resolve({ ...okPage(""), ok: false, status: 500 }),
    "no token in the page": () => Promise.resolve(okPage("<!doctype html><html><head></head><body>hi</body></html>")),
    "an empty token": () => Promise.resolve(okPage(pageWithToken(""))),
    "a token of the wrong shape": () => Promise.resolve(okPage(pageWithToken('x"><script>1</script>'))),
    "an off-origin answer": () =>
      Promise.resolve(okPage(pageWithToken(FRESH_TOKEN), "https://elsewhere.example/calendar")),
    "an unreadable body": () => Promise.resolve({ ...okPage(""), text: () => Promise.reject(new Error("aborted")) }),
  };

  for (const [name, answer] of Object.entries(outcomes)) {
    const { dom, calls } = await load(answer);
    try {
      fireResponseError(dom.window, { status: 403, responseText: FORBIDDEN_FRAGMENT });
      await flush();

      assert.equal(calls.length, 1, `${name}: one attempt, no loop`);
      assert.equal(currentToken(dom.window), STALE_TOKEN, `${name}: the token is left as it was`);
      assert.ok(
        notice(dom.window).querySelector("[data-day-save-retry]"),
        `${name}: the Retry is still offered`
      );
      assert.equal(dayForm(dom.window).querySelector("#calendar-notes").value, TYPED_NOTE, `${name}: entry intact`);
    } finally {
      dom.window.close();
    }
  }
});

test("a 403 without a recognisable body still gets its Retry and one refresh", async () => {
  const bodies = {
    "plain text": "Forbidden",
    "empty body": "",
    "an error block under another key": '<div class="status-error" data-flash-key="something.else">Nope.</div>',
    "an error block with no key": '<div class="status-error">Nope.</div>',
    "no body at all": undefined,
  };

  for (const [name, responseText] of Object.entries(bodies)) {
    const { dom, calls } = await load(() => Promise.resolve(okPage(pageWithToken(FRESH_TOKEN))));
    try {
      fireResponseError(dom.window, { status: 403, responseText });
      await flush();

      assert.ok(notice(dom.window), `${name}: a failure notice is rendered`);
      assert.ok(notice(dom.window).querySelector("[data-day-save-retry]"), `${name}: with a Retry`);
      assert.equal(calls.length, 1, `${name}: one refresh`);
      assert.equal(currentToken(dom.window), FRESH_TOKEN, `${name}: the token is replaced`);
    } finally {
      dom.window.close();
    }
  }
});

test("only a 403 triggers a refresh", async () => {
  for (const status of [0, 400, 401, 409, 422, 429, 500, 503]) {
    const { dom, calls } = await load(() => Promise.resolve(okPage(pageWithToken(FRESH_TOKEN))));
    try {
      fireResponseError(dom.window, { status, responseText: FORBIDDEN_FRAGMENT });
      await flush();
      assert.equal(calls.length, 0, `status ${status} must not fetch`);
      assert.equal(currentToken(dom.window), STALE_TOKEN);
    } finally {
      dom.window.close();
    }
  }
});

test("a 403 on another day write control refreshes too, and one off the day routes does not", async () => {
  const { dom, calls } = await load(() => Promise.resolve(okPage(pageWithToken(FRESH_TOKEN))));
  try {
    const other = dom.window.document.createElement("button");
    dom.window.document.body.appendChild(other);
    const fire = (requestPath) =>
      other.dispatchEvent(
        new dom.window.CustomEvent("htmx:responseError", {
          detail: { xhr: { status: 403, responseText: "" }, target: other, pathInfo: { requestPath } },
          bubbles: true,
        })
      );

    fire("/api/v1/sessions/current");
    await flush();
    assert.equal(calls.length, 0, "a refusal off the day routes is not this recovery's business");

    fire("/api/v1/days/2026-08-11");
    await flush();
    assert.equal(calls.length, 1);
    assert.equal(currentToken(dom.window), FRESH_TOKEN);
  } finally {
    dom.window.close();
  }
});

test("only the token is adopted: nothing else in the fetched page enters this one", async () => {
  const hostile = pageWithToken(
    FRESH_TOKEN,
    `<input type="hidden" name="day_form_account" value="binding-of-another-account">
     <script>window.__refreshXSSFired = true</script>
     <div id="injected">from the fetched page</div>`
  );
  const { dom } = await load(() => Promise.resolve(okPage(hostile)));
  try {
    fireResponseError(dom.window, { status: 403, responseText: FORBIDDEN_FRAGMENT });
    await flush();

    assert.equal(currentToken(dom.window), FRESH_TOKEN);
    assert.equal(
      dom.window.document.querySelector('input[name="day_form_account"]').value,
      OWN_BINDING,
      "the account binding is never refreshed: a write after another account signed in must still be refused"
    );
    assert.equal(dom.window.document.getElementById("injected"), null);
    assert.equal(dom.window.__refreshXSSFired, undefined);
  } finally {
    dom.window.close();
  }
});

test("a refused dashboard autosave refreshes the token, then Retry sends it", async () => {
  let writes = 0;
  const { dom, calls } = await load(
    (url, init) => {
      if (init.method === "GET") {
        return Promise.resolve(okPage(pageWithToken(FRESH_TOKEN)));
      }
      writes += 1;
      if (writes === 1) {
        return Promise.resolve({
          ok: false,
          status: 403,
          headers: { get: () => null },
          text: () => Promise.resolve(FORBIDDEN_FRAGMENT),
        });
      }
      return Promise.resolve({ ok: true, status: 200, headers: { get: () => null }, text: () => Promise.resolve("") });
    },
    { html: DASHBOARD_PAGE }
  );
  try {
    const notes = dom.window.document.querySelector("#today-notes");
    notes.dispatchEvent(new dom.window.Event("input", { bubbles: true }));
    dom.window.dispatchEvent(new dom.window.Event("pagehide"));
    await flush();
    await flush();

    assert.deepEqual(
      calls.map((call) => call.init.method),
      ["PUT", "GET"],
      "one refused write, then one token refresh, and no automatic re-send"
    );
    assert.equal(calls[0].init.headers["X-CSRF-Token"], STALE_TOKEN);
    assert.equal(currentToken(dom.window), FRESH_TOKEN);

    const retry = dom.window.document.querySelector("#save-status [data-day-save-retry]");
    assert.ok(retry, "the dashboard notice offers its Retry");
    retry.dispatchEvent(new dom.window.MouseEvent("click", { bubbles: true, cancelable: true }));
    await flush();

    assert.equal(calls.length, 3);
    assert.equal(calls[2].init.method, "PUT");
    assert.equal(calls[2].init.headers["X-CSRF-Token"], FRESH_TOKEN, "the retry carries the refreshed token");
    assert.equal(
      calls[2].init.headers["X-Ovumcy-Day-Form-Account"],
      OWN_BINDING,
      "and still the binding of the account that rendered the page"
    );
    assert.equal(notes.value, TYPED_NOTE);
  } finally {
    dom.window.close();
  }
});
