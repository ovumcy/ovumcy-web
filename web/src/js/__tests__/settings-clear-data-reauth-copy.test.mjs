// Covers the clear-data re-authentication pre-check
// (web/src/js/app/22b-clear-data-confirm.js). The pre-check POSTs the password
// to /api/v1/users/current/data-wipe/validate before the confirm dialog opens;
// the shared re-auth budget answers an exhausted window with 429, and the
// status beside the control must say so in the same words the delete-account
// path uses — "try again" alone invites attempts that cannot succeed.

import test from "node:test";
import assert from "node:assert/strict";

import { loadDOMWithScript, readAppBundle } from "./_helpers.mjs";

const APP_BUNDLE = readAppBundle();

const COPY = {
  invalidPassword: "COPY-invalid-password",
  requestFailed: "COPY-request-failed",
  tooMany: "COPY-too-many-requests",
};

function dangerZonePage() {
  return `<!doctype html>
<html lang="en"><head><meta name="csrf-token" content="test-token"></head>
<body>
  <form
    action="/api/v1/users/current/data-wipe"
    method="post"
    data-clear-data-verify-form
    data-clear-data-status-target="#settings-clear-data-status"
    data-clear-data-validate-action="/api/v1/users/current/data-wipe/validate"
    data-clear-data-confirm-message="Clear?"
    data-clear-data-confirm-accept="Clear"
    data-clear-data-invalid-password="${COPY.invalidPassword}"
    data-clear-data-request-failed="${COPY.requestFailed}"
    data-clear-data-too-many="${COPY.tooMany}">
    <input type="password" id="settings-clear-data-password" name="password" value="wrong">
    <button type="submit">Clear all data</button>
  </form>
  <div id="settings-clear-data-status"></div>
</body></html>`;
}

async function precheckRefusedWith(status, body) {
  let requested = false;
  const dom = await loadDOMWithScript(APP_BUNDLE, {
    html: dangerZonePage(),
    beforeRun(window) {
      window.fetch = function () {
        requested = true;
        return Promise.resolve({
          ok: false,
          status,
          json: () => (body === undefined ? Promise.reject(new Error("no body")) : Promise.resolve(body)),
        });
      };
    },
  });
  const { window } = dom;
  const statusNode = window.document.querySelector("#settings-clear-data-status");

  window.document
    .querySelector("form[data-clear-data-verify-form]")
    .dispatchEvent(new window.Event("submit", { bubbles: true, cancelable: true }));

  for (let attempt = 0; attempt < 50 && statusNode.textContent.trim() === ""; attempt += 1) {
    await new Promise((resolve) => setTimeout(resolve, 10));
  }
  const text = statusNode.textContent.trim();
  dom.window.close();
  assert.ok(requested, "the pre-check must issue its request");
  return text;
}

function envelope(error, category) {
  return { error, error_detail: { key: error, category, target: "global" } };
}

test("an exhausted re-auth budget (429) shows the too-many-requests copy, not the generic failure", async () => {
  const text = await precheckRefusedWith(429, envelope("too many requests", "rate_limited"));
  assert.equal(text, COPY.tooMany);
});

test("a 429 without a readable body still shows the too-many-requests copy", async () => {
  const text = await precheckRefusedWith(429, undefined);
  assert.equal(text, COPY.tooMany);
});

test("a wrong password keeps its own copy", async () => {
  const text = await precheckRefusedWith(401, envelope("invalid password", "unauthorized"));
  assert.equal(text, COPY.invalidPassword);
});

test("any other refusal falls back to the generic request-failed copy", async () => {
  const text = await precheckRefusedWith(500, envelope("internal_error", "internal"));
  assert.equal(text, COPY.requestFailed);
});
