// The temperature field shows the language's own decimal separator (the server
// renders 98,6 on a Russian page). The field script must keep it through the
// sanitising on input and the formatting on blur, or it flips back to 98.60.

import test from "node:test";
import assert from "node:assert/strict";
import { readAppBundle, loadDOMWithScript } from "./_helpers.mjs";

const APP_BUNDLE = readAppBundle();

function page(separator, value) {
  return `<!doctype html><html><head></head><body>
  <form><input type="text" id="calendar-bbt" name="bbt" value="${value}" data-temperature-input
    data-temperature-separator="${separator}" data-temperature-min="93.2" data-temperature-max="109.4"
    data-temperature-range-error="range"></form>
</body></html>`;
}

async function settle(html, typed) {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html });
  try {
    const input = dom.window.document.getElementById("calendar-bbt");
    const atLoad = input.value;
    if (typed !== undefined) {
      input.value = typed;
      input.dispatchEvent(new dom.window.Event("input", { bubbles: true }));
    }
    input.dispatchEvent(new dom.window.Event("blur"));
    return { atLoad, afterBlur: input.value, valid: input.validity.valid };
  } finally {
    dom.window.close();
  }
}

test("a Russian field keeps its comma at load and after a blur", async () => {
  const result = await settle(page(",", "98,6"));
  assert.equal(result.atLoad, "98,6");
  assert.equal(result.afterBlur, "98,6");
  assert.equal(result.valid, true);
});

test("a dot typed into a Russian field comes back as a comma, trimmed", async () => {
  const result = await settle(page(",", ""), "98.60");
  assert.equal(result.afterBlur, "98,6");
});

test("an English field keeps its dot", async () => {
  const result = await settle(page(".", "98.6"), "98,75");
  assert.equal(result.atLoad, "98.6");
  assert.equal(result.afterBlur, "98.75");
});
