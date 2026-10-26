// A reader who asked the system for reduced motion must not be animated
// across the page. The hash-target scroll already honoured the preference;
// the dashboard's quick "jump to mood / symptoms" actions scrolled smoothly
// whatever the reader had asked for (WEB-262). Both paths are pinned here,
// against the shipped bundle, for both settings of the preference.

import test from "node:test";
import assert from "node:assert/strict";
import { readAppBundle, loadDOMWithScript } from "./_helpers.mjs";

const APP_BUNDLE = readAppBundle();

const PAGE = `<!doctype html><html><head></head><body>
  <div data-dashboard-editor>
    <button type="button" data-quick-action="mood">Mood</button>
    <button type="button" data-quick-action="symptom">Symptom</button>
    <fieldset id="mood-section" data-dashboard-section="mood">
      <input type="radio" name="mood" value="1">
    </fieldset>
    <fieldset data-dashboard-section="symptoms">
      <input type="checkbox" name="symptom_ids" value="7">
    </fieldset>
  </div>
</body></html>`;

// jsdom has neither matchMedia nor scrollIntoView. Both are installed before
// the bundle runs, so the hash scroll on load is recorded too.
async function loadWithMotionPreference(reduce, url) {
  const scrolls = [];
  const dom = await loadDOMWithScript(APP_BUNDLE, {
    html: PAGE,
    url,
    beforeRun(window) {
      window.matchMedia = (query) => ({
        matches: query === "(prefers-reduced-motion: reduce)" ? reduce : false,
        media: query,
        addEventListener() {},
        removeEventListener() {},
        addListener() {},
        removeListener() {},
      });
      window.Element.prototype.scrollIntoView = function (arg) {
        scrolls.push({ element: this, arg });
      };
    },
  });
  return { dom, scrolls };
}

for (const [action, sectionKey] of [
  ["mood", "mood"],
  ["symptom", "symptoms"],
]) {
  test(`the ${action} quick action jumps without animation when reduced motion is requested`, async () => {
    const { dom, scrolls } = await loadWithMotionPreference(true);
    try {
      const document = dom.window.document;
      document.querySelector(`[data-quick-action='${action}']`).click();
      const section = document.querySelector(`[data-dashboard-section='${sectionKey}']`);
      const toSection = scrolls.filter((entry) => entry.element === section);
      assert.equal(toSection.length, 1, "the quick action scrolls its section into view once");
      assert.equal(toSection[0].arg.behavior, undefined, "no smooth behaviour under reduced motion");
      assert.equal(toSection[0].arg.block, "center", "the section is still centred");
    } finally {
      dom.window.close();
    }
  });

  test(`the ${action} quick action still scrolls smoothly when motion is allowed`, async () => {
    const { dom, scrolls } = await loadWithMotionPreference(false);
    try {
      const document = dom.window.document;
      document.querySelector(`[data-quick-action='${action}']`).click();
      const section = document.querySelector(`[data-dashboard-section='${sectionKey}']`);
      const toSection = scrolls.filter((entry) => entry.element === section);
      assert.equal(toSection.length, 1, "the quick action scrolls its section into view once");
      assert.equal(toSection[0].arg.behavior, "smooth");
    } finally {
      dom.window.close();
    }
  });
}

for (const reduce of [true, false]) {
  test(`a hash target on load follows the same motion preference (reduce=${reduce})`, async () => {
    const { dom, scrolls } = await loadWithMotionPreference(reduce, "https://ovumcy.test/dashboard#mood-section");
    try {
      const target = dom.window.document.getElementById("mood-section");
      const toTarget = scrolls.filter((entry) => entry.element === target);
      assert.ok(toTarget.length >= 1, "the hash target is scrolled into view");
      for (const entry of toTarget) {
        assert.equal(entry.arg.block, "start");
        assert.equal(entry.arg.behavior, reduce ? undefined : "smooth");
      }
    } finally {
      dom.window.close();
    }
  });
}
