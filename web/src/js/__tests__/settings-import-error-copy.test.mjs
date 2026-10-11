// Covers the refusal copy of the settings restore client
// (web/src/js/settings-import/00-restore.js, bundled into
// web/static/js/settings-import.js). Every API refusal answers the shared
// envelope {error, error_detail: {key, category, target}}; the client resolves
// the refusal's own message from that key, and falls back to the generic
// "restore failed" copy only for a key it has no message for.

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

const COPY = {
  failed: "COPY-restore-failed",
  invalidFile: "COPY-invalid-file",
  tooLarge: "COPY-too-large",
  accountChanged: "COPY-account-changed",
};

function importPage() {
  return `<!doctype html><html><head></head><body>
    <details open data-import-section data-import-csrf="csrf-abc" data-import-account="binding"
      data-import-failed="${COPY.failed}"
      data-import-invalid-file="${COPY.invalidFile}"
      data-import-too-large="${COPY.tooLarge}"
      data-import-account-changed="${COPY.accountChanged}">
      <form data-import-form>
        <input type="file" data-import-file>
        <button type="submit" data-import-submit>Restore</button>
      </form>
    </details>
  </body></html>`;
}

// The envelope the server writes for a refusal with the given key.
function envelope(key) {
  return { error: key, error_detail: { key, category: "validation", target: "global" } };
}

async function restoreRefusedWith(status, body) {
  const toasts = [];
  let requested = false;
  const dom = await loadDOMWithScript(IMPORT_BUNDLE, {
    html: importPage(),
    beforeRun(window) {
      window.showToast = function (message, kind) {
        toasts.push({ message, kind });
      };
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
  const fileInput = window.document.querySelector("[data-import-file]");
  const file = new window.File(['{"entries":[]}'], "export.json", { type: "application/json" });
  Object.defineProperty(fileInput, "files", { value: [file] });

  window.document
    .querySelector("[data-import-form]")
    .dispatchEvent(new window.Event("submit", { bubbles: true, cancelable: true }));

  for (let attempt = 0; attempt < 50 && toasts.length === 0; attempt += 1) {
    await new Promise((resolve) => setTimeout(resolve, 10));
  }
  dom.window.close();
  assert.ok(requested, "the restore must issue its request");
  assert.equal(toasts.length, 1, "exactly one toast answers the refusal");
  assert.equal(toasts[0].kind, "error");
  return toasts[0].message;
}

// One row per refusal key the restore client has copy for. The keys are the
// server's stable error keys on POST /api/v1/imports/json: the service refusals
// ("invalid import file", "import file too large"), the account-binding guard
// ("settings form account changed") and the body-size cap that answers before
// the handler runs ("request_too_large").
const MAPPED = [
  { key: "invalid import file", status: 400, copy: COPY.invalidFile },
  { key: "import file too large", status: 413, copy: COPY.tooLarge },
  { key: "request_too_large", status: 413, copy: COPY.tooLarge },
  { key: "settings form account changed", status: 409, copy: COPY.accountChanged },
];

for (const { key, status, copy } of MAPPED) {
  test(`a restore refused with "${key}" shows its own copy, read from error_detail.key`, async () => {
    const message = await restoreRefusedWith(status, envelope(key));
    assert.equal(message, copy);
  });
}

test("the top-level error string is read when error_detail is absent", async () => {
  const message = await restoreRefusedWith(409, { error: "settings form account changed" });
  assert.equal(message, COPY.accountChanged);
});

test("error_detail.key wins over the top-level error string", async () => {
  const message = await restoreRefusedWith(400, {
    error: "internal_error",
    error_detail: { key: "invalid import file" },
  });
  assert.equal(message, COPY.invalidFile);
});

test("a key with no message of its own shows the generic restore-failed copy", async () => {
  const message = await restoreRefusedWith(500, envelope("failed to import data"));
  assert.equal(message, COPY.failed);
});

test("a refusal with an unreadable body shows the generic restore-failed copy", async () => {
  const message = await restoreRefusedWith(502, undefined);
  assert.equal(message, COPY.failed);
});

test("the legacy payload.key shape is not a refusal key", async () => {
  const message = await restoreRefusedWith(400, { key: "invalid import file" });
  assert.equal(message, COPY.failed);
});
