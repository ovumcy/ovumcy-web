// HIGH gap #4 from the JS coverage audit: writeTextToClipboard in
// web/src/js/app/40-shared-utils.js prefers navigator.clipboard.writeText
// and falls back to the deprecated `document.execCommand("copy")` path
// when the modern API throws or is missing. The fallback is what keeps
// recovery-code copy working in non-secure contexts (e.g. self-hosted on
// http://192.168.x.x without TLS).
//
// writeTextToClipboard is closure-scoped, so we exercise it through the
// only production caller: the click handler installed by
// bindRecoveryCodeTools on a `[data-recovery-code-tools]` root, where a
// child `[data-recovery-action="copy"]` button reads the code from the
// adjacent `[data-recovery-code-value]` element.

import test from "node:test";
import assert from "node:assert/strict";
import { readAppBundle, loadDOMWithScript } from "./_helpers.mjs";

const APP_BUNDLE = readAppBundle();

function buildRecoveryToolsMarkup(code) {
  return `<!doctype html><html><head></head><body>
    <div data-recovery-code-tools>
      <span data-recovery-code-value>${code}</span>
      <button type="button" data-recovery-action="copy">Copy</button>
    </div>
  </body></html>`;
}

function findTempClipboardTextarea(window) {
  return window.document.querySelector("textarea.clipboard-helper");
}

async function clickCopyButton(dom) {
  const button = dom.window.document.querySelector('[data-recovery-action="copy"]');
  assert.ok(button, "fixture must include a copy button");
  button.click();
  // The copy handler returns a Promise; wait one microtask tick so the
  // .then / .catch chains run before assertions.
  await new Promise((resolve) => setImmediate(resolve));
}

test("uses navigator.clipboard.writeText when the modern API resolves", async () => {
  const calls = [];
  const dom = await loadDOMWithScript(APP_BUNDLE, {
    html: buildRecoveryToolsMarkup("OVUM-AAAA-BBBB-CCCC"),
    beforeRun(window) {
      window.navigator.clipboard = {
        writeText(text) {
          calls.push({ api: "navigator.clipboard", text });
          return Promise.resolve();
        },
      };
      window.document.execCommand = function () {
        calls.push({ api: "execCommand" });
        return true;
      };
    },
  });
  try {
    await clickCopyButton(dom);
    assert.equal(calls.length, 1, `expected exactly one clipboard write, got ${JSON.stringify(calls)}`);
    assert.equal(calls[0].api, "navigator.clipboard");
    assert.equal(calls[0].text, "OVUM-AAAA-BBBB-CCCC");
  } finally {
    dom.window.close();
  }
});

test("falls back to document.execCommand when navigator.clipboard rejects", async () => {
  const calls = [];
  const dom = await loadDOMWithScript(APP_BUNDLE, {
    html: buildRecoveryToolsMarkup("OVUM-1111-2222-3333"),
    beforeRun(window) {
      window.navigator.clipboard = {
        writeText() {
          calls.push({ api: "navigator.clipboard" });
          return Promise.reject(new Error("clipboard permission denied"));
        },
      };
      window.document.execCommand = function (command) {
        // Assert at copy time: the temporary textarea must already hold
        // the exact code, selected end-to-end, before the browser is
        // asked to copy it.
        const textarea = findTempClipboardTextarea(window);
        assert.ok(textarea, "the temporary textarea must be in the DOM when execCommand runs");
        assert.equal(textarea.value, "OVUM-1111-2222-3333", "the temporary textarea must hold the exact code");
        assert.equal(textarea.selectionStart, 0, "the selection must start at the beginning of the code");
        assert.equal(
          textarea.selectionEnd,
          "OVUM-1111-2222-3333".length,
          "the selection must cover exactly the code, not more or less"
        );
        calls.push({ api: "execCommand", command });
        return true;
      };
    },
  });
  try {
    await clickCopyButton(dom);
    await new Promise((resolve) => setImmediate(resolve));
    assert.deepEqual(
      calls.map((c) => c.api),
      ["navigator.clipboard", "execCommand"],
      `expected fallback chain navigator.clipboard → execCommand, got ${JSON.stringify(calls)}`
    );
    assert.equal(calls[1].command, "copy");
    assert.equal(
      findTempClipboardTextarea(dom.window),
      null,
      "the temporary textarea must be removed from the DOM after copy"
    );
  } finally {
    dom.window.close();
  }
});

test("falls back to document.execCommand when navigator.clipboard is undefined", async () => {
  const calls = [];
  const dom = await loadDOMWithScript(APP_BUNDLE, {
    html: buildRecoveryToolsMarkup("OVUM-9999-8888-7777"),
    beforeRun(window) {
      // Simulate a browser without the modern clipboard API at all (or
      // a non-secure context where navigator.clipboard is suppressed).
      Object.defineProperty(window.navigator, "clipboard", { value: undefined, configurable: true });
      window.document.execCommand = function (command) {
        // Same copy-time contract as the reject path above: the code must
        // be exactly present and exactly selected before the copy runs.
        const textarea = findTempClipboardTextarea(window);
        assert.ok(textarea, "the temporary textarea must be in the DOM when execCommand runs");
        assert.equal(textarea.value, "OVUM-9999-8888-7777", "the temporary textarea must hold the exact code");
        assert.equal(textarea.selectionStart, 0, "the selection must start at the beginning of the code");
        assert.equal(
          textarea.selectionEnd,
          "OVUM-9999-8888-7777".length,
          "the selection must cover exactly the code, not more or less"
        );
        calls.push({ api: "execCommand", command });
        return true;
      };
    },
  });
  try {
    await clickCopyButton(dom);
    assert.equal(calls.length, 1, `expected one execCommand call, got ${JSON.stringify(calls)}`);
    assert.equal(calls[0].command, "copy");
    assert.equal(
      findTempClipboardTextarea(dom.window),
      null,
      "the temporary textarea must be removed from the DOM after copy"
    );
  } finally {
    dom.window.close();
  }
});

test("surfaces a copy failure when both clipboard paths fail", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, {
    html: buildRecoveryToolsMarkup("OVUM-FFFF-EEEE-DDDD"),
    beforeRun(window) {
      window.navigator.clipboard = {
        writeText() {
          return Promise.reject(new Error("clipboard permission denied"));
        },
      };
      window.document.execCommand = function () {
        // execCommand returns false to indicate the copy did not happen.
        // The fallback must reject the promise so the recovery-code UI
        // can render the "copy-failed" status instead of silently lying.
        return false;
      };
    },
  });
  try {
    await clickCopyButton(dom);
    await new Promise((resolve) => setImmediate(resolve));
    // The DOM contract: when both paths fail, the recovery tools render
    // a status node with class "recovery-status-copy-failed" (or a status
    // node that contains "copy-failed" as a class fragment). Locking this
    // in protects the "we definitely told the user the copy failed"
    // contract.
    const statusNode = dom.window.document.querySelector(
      "[data-recovery-status], .recovery-status, .status-error, [data-status]"
    );
    // We do not assert the exact selector because the markup may evolve;
    // we DO assert that SOMETHING signals the failure state, by ensuring
    // the root acquired a status-related class or attribute that did not
    // exist before the click.
    const root = dom.window.document.querySelector("[data-recovery-code-tools]");
    const surfacedFailure =
      (statusNode && statusNode.textContent.length > 0) ||
      root.outerHTML.includes("copy-failed") ||
      root.outerHTML.includes("status-error");
    assert.ok(
      surfacedFailure,
      `expected the recovery tools to surface a copy-failed state when both clipboard paths fail; root was: ${root.outerHTML}`
    );
  } finally {
    dom.window.close();
  }
});
