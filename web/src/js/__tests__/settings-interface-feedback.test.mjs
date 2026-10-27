import test from "node:test";
import assert from "node:assert/strict";

import { loadDOMWithScript, readAppBundle } from "./_helpers.mjs";

const APP_BUNDLE = readAppBundle();

function interfacePage(withFlash) {
  return `<!doctype html>
<html lang="en"><head><meta name="csrf-token" content="t"></head>
<body>
  <div data-settings-sections>
    <details id="settings-interface" data-settings-section open>
      <form action="/api/v1/users/current/interface" method="post"
        hx-patch="/api/v1/users/current/interface" hx-target="#settings-interface-status"
        data-settings-draft-form="interface" data-settings-interface-form>
        <label data-settings-interface-language-option="en" data-selected="true"><input type="radio" name="language" value="en" checked></label>
        <label data-settings-interface-language-option="de" data-selected="false"><input type="radio" name="language" value="de"></label>
        <label data-settings-interface-theme-option="light" data-selected="false"><input type="radio" name="theme" value="light"></label>
        <label data-settings-interface-theme-option="dark" data-selected="false"><input type="radio" name="theme" value="dark"></label>
        <label data-settings-interface-theme-option="system" data-selected="false"><input type="radio" name="theme" value="system" checked></label>
        <button type="submit" data-settings-interface-save>Save</button>
        <button type="button" data-settings-interface-discard>Discard</button>
        <div id="settings-interface-status" aria-live="polite">${
          withFlash
            ? '<div class="status-ok" role="status" tabindex="-1" data-settings-flash-focus data-flash-key="settings.success.interface_updated"><span class="toast-message">Saved</span></div>'
            : ""
        }</div>
      </form>
    </details>
  </div>
</body></html>`;
}

async function load(withFlash) {
  return loadDOMWithScript(APP_BUNDLE, {
    html: interfacePage(withFlash),
    url: "https://ovumcy.test/settings#settings-interface",
  });
}

test("the interface confirmation rendered after a reload takes focus, so focus is not left on BODY", async () => {
  const dom = await load(true);
  const { document } = dom.window;
  const flash = document.querySelector("[data-settings-flash-focus]");
  assert.equal(document.activeElement, flash);
});

test("a settings load without a confirmation takes no focus", async () => {
  const dom = await load(false);
  assert.equal(dom.window.document.activeElement, dom.window.document.body);
});

test("a successful in-place save makes the saved selection the clean baseline", async () => {
  const dom = await load(false);
  const { document } = dom.window;
  const form = document.querySelector("form[data-settings-interface-form]");

  document.querySelector('input[name="theme"][value="dark"]').click();
  assert.equal(form.dataset.settingsDraftDirty, "true");

  const event = new dom.window.CustomEvent("htmx:afterRequest", {
    bubbles: true,
    detail: { elt: form, successful: true },
  });
  form.dispatchEvent(event);
  assert.equal(form.dataset.settingsDraftDirty, "false");
});
