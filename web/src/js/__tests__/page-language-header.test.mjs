// WEB-232: every htmx request names the language its page was rendered in, so
// the server can answer HX-Refresh when another tab has switched the language
// cookie instead of swapping a fragment in a different language into the page.

import test from "node:test";
import assert from "node:assert/strict";
import { readAppBundle, loadDOMWithScript } from "./_helpers.mjs";

const APP_BUNDLE = readAppBundle();

function dispatchConfigRequest(window, verb) {
  const detail = { verb, parameters: {}, headers: {} };
  window.document.body.dispatchEvent(new window.CustomEvent("htmx:configRequest", { detail }));
  return detail;
}

for (const verb of ["get", "post"]) {
  test(`htmx:configRequest sends the page language on a ${verb.toUpperCase()}`, async () => {
    const dom = await loadDOMWithScript(APP_BUNDLE, {
      html: `<!doctype html><html lang="fr"><head></head><body></body></html>`,
    });
    try {
      const detail = dispatchConfigRequest(dom.window, verb);
      assert.equal(detail.headers["X-Ovumcy-Page-Lang"], "fr");
      assert.equal(detail.parameters["X-Ovumcy-Page-Lang"], undefined, "the language travels as a header, not a parameter");
    } finally {
      dom.window.close();
    }
  });
}

test("htmx:configRequest sends no page language when the page declares none", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, {
    html: `<!doctype html><html><head></head><body></body></html>`,
  });
  try {
    const detail = dispatchConfigRequest(dom.window, "post");
    assert.equal(Object.hasOwn(detail.headers, "X-Ovumcy-Page-Lang"), false);
  } finally {
    dom.window.close();
  }
});
