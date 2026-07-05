// Covers the session-once timezone sync installed by
// web/src/js/app/25-timezone-sync.js. The module POSTs the browser-detected
// IANA timezone to POST /api/v1/users/current/timezone only when it is safe,
// differs from the server-persisted value (data-persisted-timezone on <body>),
// carries a CSRF token, and has not already been synced this session. A
// regression that drops the CSRF header, syncs an unchanged value, or fires on
// an anonymous page (no csrf meta) would surface here.

import test from "node:test";
import assert from "node:assert/strict";
import { readAppBundle, loadDOMWithScript } from "./_helpers.mjs";

const APP_BUNDLE = readAppBundle();

// stubTimezone forces Intl.DateTimeFormat().resolvedOptions().timeZone to a
// fixed value so the detected client timezone is deterministic in jsdom.
function stubTimezone(zone) {
  return (window) => {
    window.Intl = {
      DateTimeFormat: function () {
        return {
          resolvedOptions: function () {
            return { timeZone: zone };
          },
        };
      },
    };
  };
}

// captureFetch installs a fake window.fetch that records calls and resolves ok.
function captureFetch(window, { ok = true } = {}) {
  const calls = [];
  window.fetch = function (url, options) {
    calls.push({ url, options });
    return Promise.resolve({ ok });
  };
  return calls;
}

function pageWithPersistedTz(persisted, { csrf = "csrf-token-abc-123" } = {}) {
  const csrfMeta = csrf === null ? "" : `<meta name="csrf-token" content="${csrf}">`;
  const persistedAttr = persisted === null ? "" : ` data-persisted-timezone="${persisted}"`;
  return `<!doctype html><html><head>${csrfMeta}</head><body${persistedAttr}></body></html>`;
}

// flushMicrotasks lets the fetch promise .then/.catch chain settle.
function flushMicrotasks() {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

test("posts the detected timezone with the CSRF header when it differs from the persisted value", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, {
    html: pageWithPersistedTz("America/Toronto"),
    beforeRun: stubTimezone("Europe/Belgrade"),
  });
  const calls = captureFetch(dom.window);
  try {
    // The sync runs inside onDocumentReady; re-fire after fetch is installed.
    dom.window.document.dispatchEvent(new dom.window.Event("DOMContentLoaded", { bubbles: true }));
    await flushMicrotasks();

    assert.equal(calls.length, 1, "expected exactly one sync POST");
    assert.equal(calls[0].url, "/api/v1/users/current/timezone");
    assert.equal(calls[0].options.method, "POST");
    assert.equal(calls[0].options.headers["X-CSRF-Token"], "csrf-token-abc-123", "the CSRF token must ride on the sync POST");
    assert.match(calls[0].options.body, /timezone=Europe%2FBelgrade/, "the detected IANA zone must be in the form body");
    assert.equal(calls[0].options.credentials, "same-origin");
  } finally {
    dom.window.close();
  }
});

test("does not post when the detected timezone already matches the persisted value", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, {
    html: pageWithPersistedTz("Europe/Belgrade"),
    beforeRun: stubTimezone("Europe/Belgrade"),
  });
  const calls = captureFetch(dom.window);
  try {
    dom.window.document.dispatchEvent(new dom.window.Event("DOMContentLoaded", { bubbles: true }));
    await flushMicrotasks();
    assert.equal(calls.length, 0, "an unchanged timezone must not trigger a POST");
  } finally {
    dom.window.close();
  }
});

test("does not post on an anonymous page (no csrf token)", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, {
    html: pageWithPersistedTz(null, { csrf: null }),
    beforeRun: stubTimezone("Europe/Belgrade"),
  });
  const calls = captureFetch(dom.window);
  try {
    dom.window.document.dispatchEvent(new dom.window.Event("DOMContentLoaded", { bubbles: true }));
    await flushMicrotasks();
    assert.equal(calls.length, 0, "without a csrf token there is no authenticated owner to sync for");
  } finally {
    dom.window.close();
  }
});

test("does not re-post the same detected timezone twice in one session", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, {
    html: pageWithPersistedTz("America/Toronto"),
    beforeRun: stubTimezone("Europe/Belgrade"),
  });
  const calls = captureFetch(dom.window);
  try {
    dom.window.document.dispatchEvent(new dom.window.Event("DOMContentLoaded", { bubbles: true }));
    await flushMicrotasks();
    // Second navigation within the same session (sessionStorage persists).
    dom.window.document.dispatchEvent(new dom.window.Event("DOMContentLoaded", { bubbles: true }));
    await flushMicrotasks();
    assert.equal(calls.length, 1, "a successful sync must not repeat for the same value this session");
  } finally {
    dom.window.close();
  }
});
