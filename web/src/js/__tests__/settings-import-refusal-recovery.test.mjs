// Covers the restore client (web/src/js/settings-import/00-restore.js) on the
// two refusals a plain retry cannot get past: the CSRF refusal (403, or a 403
// with no key — a proxy's page standing in front of the app) and the expired
// session (401). The first re-reads the token from /privacy so the retry the
// copy asks for can succeed; the second tells the person to sign in again.

import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";
import { loadDOMWithScript } from "./_helpers.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const IMPORT_BUNDLE = readFileSync(
  path.resolve(here, "..", "..", "..", "static", "js", "settings-import.js"),
  "utf8"
);

const ORIGIN = "https://ovumcy.test";
const STALE_TOKEN = "stale-token-abc";
const FRESH_TOKEN = "fresh-token-xyz";
const COPY = {
  failed: "COPY-restore-failed",
  csrfRefused: "COPY-csrf-refused",
  unauthorized: "COPY-unauthorized",
};

function importPage() {
  return `<!doctype html><html><head><meta name="csrf-token" content="${STALE_TOKEN}"></head><body>
    <details open data-import-section data-import-csrf="${STALE_TOKEN}" data-import-account="binding"
      data-import-failed="${COPY.failed}"
      data-import-csrf-refused="${COPY.csrfRefused}"
      data-import-unauthorized="${COPY.unauthorized}">
      <form data-import-form>
        <input type="file" data-import-file>
        <button type="submit" data-import-submit>Restore</button>
      </form>
    </details>
  </body></html>`;
}

function privacyPage(token) {
  return `<!doctype html><html><head><meta name="csrf-token" content="${token}"></head><body></body></html>`;
}

// refusal: {status, body} the import endpoint answers on the first POST; later
// POSTs succeed. privacy: {url, ok, token} the /privacy re-read answers.
async function restoreScenario({ refusal, privacy = { url: `${ORIGIN}/privacy`, ok: true, token: FRESH_TOKEN } }) {
  const toasts = [];
  const calls = [];
  const dom = await loadDOMWithScript(IMPORT_BUNDLE, {
    html: importPage(),
    beforeRun(window) {
      window.showToast = function (message, kind) {
        toasts.push({ message, kind });
      };
      window.fetch = function (url, options) {
        const method = (options && options.method) || "GET";
        calls.push({ url: String(url), method, token: options && options.headers ? options.headers["X-CSRF-Token"] : undefined });
        if (String(url) === "/privacy") {
          return Promise.resolve({
            ok: privacy.ok,
            status: privacy.ok ? 200 : 500,
            url: privacy.url,
            text: () => Promise.resolve(privacyPage(privacy.token)),
          });
        }
        const posts = calls.filter((call) => call.method === "POST").length;
        if (posts === 1) {
          return Promise.resolve({
            ok: false,
            status: refusal.status,
            json: () => (refusal.body === undefined ? Promise.reject(new Error("no body")) : Promise.resolve(refusal.body)),
          });
        }
        return Promise.resolve({ ok: true, status: 200, json: () => Promise.resolve({ added: 1, skipped: 0, rejected: 0 }) });
      };
    },
  });
  const { window } = dom;
  const fileInput = window.document.querySelector("[data-import-file]");
  const file = new window.File(['{"entries":[]}'], "export.json", { type: "application/json" });
  Object.defineProperty(fileInput, "files", { value: [file] });
  const form = window.document.querySelector("[data-import-form]");
  const submitButton = window.document.querySelector("[data-import-submit]");

  async function submitAndSettle(expectedToasts) {
    form.dispatchEvent(new window.Event("submit", { bubbles: true, cancelable: true }));
    for (let attempt = 0; attempt < 100 && (toasts.length < expectedToasts || submitButton.disabled); attempt += 1) {
      await new Promise((resolve) => setTimeout(resolve, 10));
    }
  }

  await submitAndSettle(1);
  const section = window.document.querySelector("[data-import-section]");
  const afterRefusal = {
    toast: toasts[0],
    sectionToken: section.getAttribute("data-import-csrf"),
    metaToken: window.document.querySelector('meta[name="csrf-token"]').getAttribute("content"),
    privacyReads: calls.filter((call) => call.url === "/privacy").length,
  };
  await submitAndSettle(2);
  const posts = calls.filter((call) => call.method === "POST");
  dom.window.close();
  return { afterRefusal, retryToken: posts[1] ? posts[1].token : undefined, firstToken: posts[0].token };
}

function csrfEnvelope() {
  return { error: "csrf_refused", error_detail: { key: "csrf_refused", category: "forbidden", target: "global" } };
}

test("a CSRF refusal shows the refusal copy and the retry sends a fresh token", async () => {
  const result = await restoreScenario({ refusal: { status: 403, body: csrfEnvelope() } });
  assert.equal(result.firstToken, STALE_TOKEN);
  assert.deepEqual(result.afterRefusal.toast, { message: COPY.csrfRefused, kind: "error" });
  assert.equal(result.afterRefusal.privacyReads, 1, "the token is re-read once, from /privacy");
  assert.equal(result.afterRefusal.sectionToken, FRESH_TOKEN);
  assert.equal(result.afterRefusal.metaToken, FRESH_TOKEN);
  assert.equal(result.retryToken, FRESH_TOKEN);
});

test("a 403 with no key (a proxy's page) is treated as the CSRF refusal", async () => {
  const result = await restoreScenario({ refusal: { status: 403, body: undefined } });
  assert.equal(result.afterRefusal.toast.message, COPY.csrfRefused);
  assert.equal(result.retryToken, FRESH_TOKEN);
});

test("a 403 under another key is not a CSRF refusal and re-reads nothing", async () => {
  const result = await restoreScenario({
    refusal: { status: 403, body: { error: "forbidden", error_detail: { key: "forbidden" } } },
  });
  assert.equal(result.afterRefusal.toast.message, COPY.failed);
  assert.equal(result.afterRefusal.privacyReads, 0);
  assert.equal(result.retryToken, STALE_TOKEN);
});

test("an expired session (401) says to sign in again and re-reads nothing", async () => {
  const result = await restoreScenario({
    refusal: { status: 401, body: { error: "unauthorized", error_detail: { key: "unauthorized" } } },
  });
  assert.equal(result.afterRefusal.toast.message, COPY.unauthorized);
  assert.equal(result.afterRefusal.privacyReads, 0);
});

test("a 401 with no readable body still says to sign in again", async () => {
  const result = await restoreScenario({ refusal: { status: 401, body: undefined } });
  assert.equal(result.afterRefusal.toast.message, COPY.unauthorized);
});

test("a token from a cross-origin answer is never adopted", async () => {
  const result = await restoreScenario({
    refusal: { status: 403, body: csrfEnvelope() },
    privacy: { url: "https://evil.example/privacy", ok: true, token: FRESH_TOKEN },
  });
  assert.equal(result.afterRefusal.toast.message, COPY.csrfRefused);
  assert.equal(result.afterRefusal.sectionToken, STALE_TOKEN);
  assert.equal(result.retryToken, STALE_TOKEN);
});

test("a malformed token from /privacy is never adopted", async () => {
  const result = await restoreScenario({
    refusal: { status: 403, body: csrfEnvelope() },
    privacy: { url: `${ORIGIN}/privacy`, ok: true, token: "<bad token>" },
  });
  assert.equal(result.afterRefusal.sectionToken, STALE_TOKEN);
  assert.equal(result.retryToken, STALE_TOKEN);
});
