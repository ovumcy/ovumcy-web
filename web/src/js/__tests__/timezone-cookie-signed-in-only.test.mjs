// Covers the signed-in gate on the client half of the timezone cookie.
//
// Signing out retracts `ovumcy_tz` server-side (clearSessionEndCookies). That
// retraction only holds if the browser stops volunteering the timezone while
// nobody is signed in: the bundle used to write the cookie on every page load
// and attach X-Ovumcy-Timezone to every htmx request, either of which puts the
// cookie straight back — on the login page the sign-out redirect lands on.
//
// The signed-in signal is `data-persisted-timezone` on <body>, which base.html
// renders only for a session and is the same signal the sync module reads. The
// early `timezone-bootstrap.js` is gated server-side instead (base.html omits
// the tag entirely for an anonymous page), because it runs before <body> exists.

import test from "node:test";
import assert from "node:assert/strict";
import { readAppBundle, loadDOMWithScript } from "./_helpers.mjs";

const APP_BUNDLE = readAppBundle();

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

// `persisted === null` omits data-persisted-timezone entirely — the anonymous
// page. A string renders it, including the empty string a brand-new owner has.
function page(persisted) {
  const attr = persisted === null ? "" : ` data-persisted-timezone="${persisted}"`;
  return `<!doctype html><html><head></head><body${attr}></body></html>`;
}

function cookieValue(window, name) {
  const match = String(window.document.cookie || "")
    .split(";")
    .map((part) => part.trim())
    .find((part) => part.startsWith(name + "="));
  return match ? match.slice(name.length + 1) : null;
}

test("writes ovumcy_tz on a page rendered for a signed-in owner", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, {
    html: page("America/Toronto"),
    beforeRun: stubTimezone("Europe/Belgrade"),
  });
  try {
    assert.equal(
      cookieValue(dom.window, "ovumcy_tz"),
      "Europe/Belgrade",
      "a signed-in page must still cache the detected zone",
    );
  } finally {
    dom.window.close();
  }
});

test("writes no ovumcy_tz on an anonymous page", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, {
    html: page(null),
    beforeRun: stubTimezone("Europe/Belgrade"),
  });
  try {
    assert.equal(
      cookieValue(dom.window, "ovumcy_tz"),
      null,
      "an anonymous page must not re-create the cookie the sign-out retracted",
    );
  } finally {
    dom.window.close();
  }
});

test("attaches no timezone header to an htmx request from an anonymous page", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, {
    html: `<!doctype html><html><head><meta name="csrf-token" content="csrf-token-abc-123"></head><body></body></html>`,
    beforeRun: stubTimezone("Europe/Belgrade"),
  });
  try {
    const detail = { verb: "post", headers: {}, parameters: {} };
    const event = new dom.window.Event("htmx:configRequest", { bubbles: true });
    event.detail = detail;
    dom.window.document.body.dispatchEvent(event);

    assert.equal(
      detail.headers["X-Ovumcy-Timezone"],
      undefined,
      "the header the middleware re-issues the cookie from must not ride on an anonymous request",
    );
    assert.equal(
      detail.headers["X-CSRF-Token"],
      "csrf-token-abc-123",
      "the CSRF header is the anchor: a listener that stopped running entirely would otherwise pass this test",
    );
  } finally {
    dom.window.close();
  }
});

test("attaches the timezone header to an htmx request from a signed-in page", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, {
    html: `<!doctype html><html><head><meta name="csrf-token" content="csrf-token-abc-123"></head><body data-persisted-timezone="America/Toronto"></body></html>`,
    beforeRun: stubTimezone("Europe/Belgrade"),
  });
  try {
    const detail = { verb: "post", headers: {}, parameters: {} };
    const event = new dom.window.Event("htmx:configRequest", { bubbles: true });
    event.detail = detail;
    dom.window.document.body.dispatchEvent(event);

    assert.equal(
      detail.headers["X-Ovumcy-Timezone"],
      "Europe/Belgrade",
      "a signed-in page must keep sending the zone the server renders dates in",
    );
  } finally {
    dom.window.close();
  }
});
