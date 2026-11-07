// The cycle map's two views (ring, ribbon) are a browser-only preference stored
// like the theme. Two halves are pinned here: theme-bootstrap.js, which runs
// before the stylesheet and writes the stored choice onto <html>, and the
// settings-interface controller, which edits, saves and discards it.
//
// The app bundle under web/static is a build artifact; these tests assemble the
// same sources in memory so they exercise the code a build would ship.

import test from "node:test";
import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";

import { loadDOMWithScript } from "./_helpers.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const jsRoot = path.resolve(here, "..");

const APP_SOURCE = readdirSync(path.join(jsRoot, "app"))
  .filter((name) => name.endsWith(".js"))
  .sort()
  .map((name) => readFileSync(path.join(jsRoot, "app", name), "utf8").trimEnd())
  .join("\n\n");
const BOOTSTRAP_SOURCE = readFileSync(path.join(jsRoot, "theme-bootstrap.js"), "utf8");

const RING_KEY = "ovumcy_cycle_ring";
const RIBBON_KEY = "ovumcy_cycle_ribbon";

function seed(stored, phone = false) {
  return (window) => {
    for (const [key, value] of Object.entries(stored)) {
      window.localStorage.setItem(key, value);
    }
    window.matchMedia = (query) => ({
      matches: phone && query === "(max-width: 640px)",
      media: query,
      addEventListener() {},
      removeEventListener() {},
      addListener() {},
      removeListener() {},
    });
  };
}

function attrs(dom) {
  const root = dom.window.document.documentElement;
  return { ring: root.getAttribute("data-cycle-ring"), ribbon: root.getAttribute("data-cycle-ribbon") };
}

async function bootstrap(stored) {
  return loadDOMWithScript(BOOTSTRAP_SOURCE, {
    html: "<!doctype html><html><head></head><body></body></html>",
    beforeRun: seed(stored),
  });
}

test("bootstrap writes nothing when no cycle view is stored", async () => {
  const dom = await bootstrap({});
  assert.deepEqual(attrs(dom), { ring: null, ribbon: null });
});

test("bootstrap writes the stored on/off value of each view", async () => {
  assert.deepEqual(attrs(await bootstrap({ [RING_KEY]: "off", [RIBBON_KEY]: "on" })), { ring: "off", ribbon: "on" });
  assert.deepEqual(attrs(await bootstrap({ [RING_KEY]: "on", [RIBBON_KEY]: "off" })), { ring: "on", ribbon: "off" });
  assert.deepEqual(attrs(await bootstrap({ [RIBBON_KEY]: "off" })), { ring: null, ribbon: "off" });
});

test("bootstrap ignores a stored value that is neither on nor off", async () => {
  assert.deepEqual(attrs(await bootstrap({ [RING_KEY]: "banana", [RIBBON_KEY]: "" })), { ring: null, ribbon: null });
});

test("bootstrap never leaves both views off: the ribbon counts as on", async () => {
  assert.deepEqual(attrs(await bootstrap({ [RING_KEY]: "off", [RIBBON_KEY]: "off" })), { ring: "off", ribbon: "on" });
});

function interfacePage() {
  return `<!doctype html>
<html lang="en"><head><meta name="csrf-token" content="t"></head>
<body>
  <div data-settings-sections>
    <details id="settings-interface" data-settings-section open>
      <form action="/api/v1/users/current/interface" method="post"
        data-settings-draft-form="interface" data-settings-interface-form>
        <label data-settings-interface-language-option="en" data-selected="true"><input type="radio" name="language" value="en" checked></label>
        <label data-settings-interface-theme-option="light" data-selected="false"><input type="radio" name="theme" value="light"></label>
        <label data-settings-interface-theme-option="system" data-selected="false"><input type="radio" name="theme" value="system" checked></label>
        <label data-settings-cycle-view-option="ring" data-selected="true"><input type="checkbox" data-settings-cycle-view-input="ring" checked></label>
        <label data-settings-cycle-view-option="ribbon" data-selected="true"><input type="checkbox" data-settings-cycle-view-input="ribbon" checked></label>
        <button type="submit" data-settings-interface-save>Save</button>
        <button type="button" data-settings-interface-discard>Discard</button>
      </form>
    </details>
  </div>
</body></html>`;
}

async function loadSettings(stored, phone = false) {
  return loadDOMWithScript(APP_SOURCE, {
    html: interfacePage(),
    url: "https://ovumcy.test/settings",
    beforeRun: seed(stored, phone),
  });
}

function boxes(dom) {
  const { document } = dom.window;
  return {
    form: document.querySelector("form[data-settings-interface-form]"),
    ring: document.querySelector('[data-settings-cycle-view-input="ring"]'),
    ribbon: document.querySelector('[data-settings-cycle-view-input="ribbon"]'),
    save: document.querySelector("[data-settings-interface-save]"),
    discard: document.querySelector("[data-settings-interface-discard]"),
  };
}

function submit(dom, form) {
  form.dispatchEvent(new dom.window.Event("submit", { bubbles: true, cancelable: true }));
}

test("with nothing stored both boxes start checked, on a wide screen and a phone alike", async () => {
  for (const phone of [false, true]) {
    const view = boxes(await loadSettings({}, phone));
    assert.equal(view.ring.checked, true);
    assert.equal(view.ribbon.checked, true);
    assert.equal(view.form.dataset.settingsDraftDirty, "false");
    assert.equal(view.save.disabled, true);
  }
});

test("stored choices pre-fill the boxes", async () => {
  const { ring, ribbon } = boxes(await loadSettings({ [RING_KEY]: "off", [RIBBON_KEY]: "on" }));
  assert.equal(ring.checked, false);
  assert.equal(ribbon.checked, true);
});

test("unchecking a view is a draft: it applies at once, and nothing is stored until Save", async () => {
  const dom = await loadSettings({});
  const { form, ring, save, discard } = boxes(dom);

  ring.click();

  assert.equal(form.dataset.settingsDraftDirty, "true");
  assert.equal(save.disabled, false);
  assert.equal(discard.disabled, false);
  assert.deepEqual(attrs(dom), { ring: "off", ribbon: "on" });
  assert.equal(dom.window.localStorage.getItem(RING_KEY), null);
  assert.equal(
    dom.window.document.querySelector('[data-settings-cycle-view-option="ring"]').dataset.selected,
    "false",
  );
});

test("save stores both views explicitly and the page keeps what it applied", async () => {
  const dom = await loadSettings({});
  const { form, ring } = boxes(dom);

  ring.click();
  submit(dom, form);

  assert.equal(dom.window.localStorage.getItem(RING_KEY), "off");
  assert.equal(dom.window.localStorage.getItem(RIBBON_KEY), "on");
  assert.deepEqual(attrs(dom), { ring: "off", ribbon: "on" });
});

test("a save that leaves the views untouched stores no view, so each device keeps its default", async () => {
  const dom = await loadSettings({});
  const { form } = boxes(dom);

  submit(dom, form);

  assert.equal(dom.window.localStorage.getItem(RING_KEY), null);
  assert.equal(dom.window.localStorage.getItem(RIBBON_KEY), null);
});

test("turning the ribbon off on a phone stores ring-only", async () => {
  const dom = await loadSettings({}, true);
  const { form, ribbon } = boxes(dom);

  ribbon.click();
  submit(dom, form);

  assert.equal(dom.window.localStorage.getItem(RING_KEY), "on");
  assert.equal(dom.window.localStorage.getItem(RIBBON_KEY), "off");
});

test("the last checked box is disabled, and re-checking the other frees it", async () => {
  const dom = await loadSettings({});
  const { ring, ribbon } = boxes(dom);

  ring.click();
  assert.equal(ribbon.checked, true);
  assert.equal(ribbon.disabled, true, "the only remaining view cannot be unchecked");
  assert.equal(ring.disabled, false);

  ring.click();
  assert.equal(ring.checked, true);
  assert.equal(ribbon.disabled, false);
});

test("a script that clears both boxes gets the baseline back", async () => {
  const dom = await loadSettings({ [RING_KEY]: "on", [RIBBON_KEY]: "off" });
  const { form, ring } = boxes(dom);

  ring.checked = false;
  form.dispatchEvent(new dom.window.Event("change", { bubbles: true }));
  // the change handler is bound to inputs; dispatch from the input itself
  ring.dispatchEvent(new dom.window.Event("change", { bubbles: true }));

  assert.equal(ring.checked, true);
  assert.deepEqual(attrs(dom), { ring: "on", ribbon: "off" });
});

test("discard restores the saved views, the boxes and the page", async () => {
  const dom = await loadSettings({ [RING_KEY]: "on", [RIBBON_KEY]: "on" });
  const { form, ring, ribbon, discard } = boxes(dom);

  ring.click();
  assert.deepEqual(attrs(dom), { ring: "off", ribbon: "on" });

  discard.click();

  assert.equal(ring.checked, true);
  assert.equal(ribbon.checked, true);
  assert.equal(ribbon.disabled, false);
  assert.deepEqual(attrs(dom), { ring: "on", ribbon: "on" });
  assert.equal(form.dataset.settingsDraftDirty, "false");
  assert.equal(dom.window.localStorage.getItem(RING_KEY), "on");
});

test("the cycle-view hooks the template renders are read by the controller", () => {
  const controller = readFileSync(path.join(jsRoot, "app", "55-settings-interface.js"), "utf8");
  const template = readFileSync(
    path.resolve(jsRoot, "..", "..", "..", "internal", "templates", "components", "settings_interface.html"),
    "utf8",
  );
  for (const hook of ["data-settings-cycle-view-input", "data-settings-cycle-view-option"]) {
    assert.ok(template.includes(hook), `the settings panel must render ${hook}`);
    assert.ok(controller.includes(hook), `the controller must read ${hook}`);
  }
});
