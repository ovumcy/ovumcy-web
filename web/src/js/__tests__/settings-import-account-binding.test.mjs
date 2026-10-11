// Covers the settings restore client (web/src/js/settings-import/00-restore.js,
// bundled into web/static/js/settings-import.js). The restore is a fetch with a
// JSON body, which the server reads as a client that renders no page — such a
// client may omit the account binding. The restore IS sent from a rendered
// page, so it must carry the binding of the account that rendered it on every
// request: a page left open after another account signed in elsewhere would
// otherwise restore its file into that other account.

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

const BINDING_HEADER = "X-Ovumcy-Day-Form-Account";

function importPage({ binding } = {}) {
  const bindingAttr = binding === undefined ? "" : ` data-import-account="${binding}"`;
  return `<!doctype html><html><head></head><body>
    <details open data-import-section data-import-csrf="csrf-abc"${bindingAttr}>
      <form data-import-form>
        <input type="file" data-import-file>
        <button type="submit" data-import-submit>Restore</button>
      </form>
    </details>
  </body></html>`;
}

async function submitRestore(binding) {
  const calls = [];
  const dom = await loadDOMWithScript(IMPORT_BUNDLE, {
    html: importPage({ binding }),
    beforeRun(window) {
      window.showToast = function () {};
      window.fetch = function (url, options) {
        calls.push({ url, options });
        return Promise.resolve({
          ok: true,
          json: () => Promise.resolve({ ok: true, added: 1, skipped: 0, rejected: 0 }),
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

  for (let attempt = 0; attempt < 50 && calls.length === 0; attempt += 1) {
    await new Promise((resolve) => setTimeout(resolve, 10));
  }
  dom.window.close();
  return calls;
}

test("the restore request carries the page's account binding in the header", async () => {
  const calls = await submitRestore("binding-of-the-rendering-account");

  assert.ok(calls.length > 0, "the restore must issue its request");
  assert.equal(calls[0].url, "/api/v1/imports/json");
  assert.equal(calls[0].options.headers[BINDING_HEADER], "binding-of-the-rendering-account");
  assert.equal(calls[0].options.headers["X-CSRF-Token"], "csrf-abc");
});

test("a page that lost its binding still sends the header, so the server refuses it", async () => {
  const calls = await submitRestore(undefined);

  assert.ok(calls.length > 0, "the restore must issue its request");
  assert.ok(
    Object.prototype.hasOwnProperty.call(calls[0].options.headers, BINDING_HEADER),
    "the binding header must be sent even when the page carries no binding"
  );
  assert.equal(calls[0].options.headers[BINDING_HEADER], "");
});
