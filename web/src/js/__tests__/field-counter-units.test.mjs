// A character counter beside a field with a `maxlength` is only honest when it
// counts in the unit the browser enforces. `maxlength` counts UTF-16 code
// units, so an emoji (a surrogate pair) takes two of them. A counter counting
// code points read "1000/2000" on a note the browser had already stopped
// accepting, and the owner could not tell why typing had no effect.
//
// Both counters on the page read the same shared length helper — the note
// counter (dashboard and day editor) and the custom symptom-name counter — so
// each is driven here through its own input event.

import test from "node:test";
import assert from "node:assert/strict";
import { readAppBundle, loadDOMWithScript } from "./_helpers.mjs";

const APP_BUNDLE = readAppBundle();

// U+1F338 CHERRY BLOSSOM: one code point, two UTF-16 code units.
const ASTRAL = "\u{1F338}";

function counterPage() {
  return `<!doctype html><html><head></head><body>
  <div data-dashboard-notes-field-group>
    <textarea name="notes" maxlength="2000" data-dashboard-notes></textarea>
    <p data-dashboard-notes-count></p>
  </div>
  <div class="settings-symptom-name-field">
    <input type="text" name="name" maxlength="40" data-symptom-name-input>
    <p data-symptom-name-count></p>
  </div>
</body></html>`;
}

function typeInto(node, value) {
  node.value = value;
  node.dispatchEvent(new node.ownerDocument.defaultView.Event("input", { bubbles: true }));
}

const COUNTERS = [
  { name: "note", input: "[data-dashboard-notes]", counter: "[data-dashboard-notes-count]", max: 2000 },
  { name: "symptom name", input: "[data-symptom-name-input]", counter: "[data-symptom-name-count]", max: 40 },
];

for (const subject of COUNTERS) {
  test(`the ${subject.name} counter counts UTF-16 code units, the unit maxlength enforces`, async () => {
    const dom = await loadDOMWithScript(APP_BUNDLE, { html: counterPage() });
    const document = dom.window.document;
    const input = document.querySelector(subject.input);
    const counter = document.querySelector(subject.counter);

    const value = "ab" + ASTRAL + ASTRAL;
    typeInto(input, value);

    assert.equal(value.length, 6, "the fixture's value is six UTF-16 code units");
    assert.equal(
      counter.textContent,
      `6/${subject.max}`,
      "each astral character counts as the two units maxlength charges for it"
    );
    dom.window.close();
  });

  test(`the ${subject.name} counter reads full exactly where maxlength stops the field`, async () => {
    const dom = await loadDOMWithScript(APP_BUNDLE, { html: counterPage() });
    const document = dom.window.document;
    const input = document.querySelector(subject.input);
    const counter = document.querySelector(subject.counter);

    // Half the limit in emoji fills the field to the browser's cutoff.
    typeInto(input, ASTRAL.repeat(subject.max / 2));

    assert.equal(counter.textContent, `${subject.max}/${subject.max}`);
    dom.window.close();
  });
}

test("a plain BMP note counts one per character", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: counterPage() });
  const document = dom.window.document;
  const input = document.querySelector("[data-dashboard-notes]");
  const counter = document.querySelector("[data-dashboard-notes-count]");

  typeInto(input, "Привет");

  assert.equal(counter.textContent, "6/2000");
  dom.window.close();
});
