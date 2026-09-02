import test from "node:test";
import assert from "node:assert/strict";

import { loadDOMWithScript, readAppBundle } from "./_helpers.mjs";

const APP_BUNDLE = readAppBundle();

// The settings page keeps an explicit Save/Discard shell: the save button is
// disabled until a control differs from what the server rendered, and leaving
// with an edit outstanding raises the unsaved-leave prompt. That prompt is a
// question whose "yes" means discard, so the unload path must stay silent — no
// request of any kind may leave the page while it is unwinding. This file pins
// that, so a later change cannot quietly turn the settings shell into an
// autosave surface the way the dashboard journal legitimately is.
//
// The three draft cards are the whole guarded set. Everything else on the page
// — password change, data wipe, account deletion, the webhook URL, the calendar
// feed — is rendered here too, precisely so the assertions below cover a page
// where those forms are present and edited.
function settingsPage() {
  return `<!doctype html>
<html lang="en"><head><meta name="csrf-token" content="test-token"></head>
<body>
  <div data-settings-sections>
    <details id="settings-cycle" data-settings-cycle-form data-settings-section open>
      <form
        action="/api/v1/users/current/cycle"
        method="post"
        hx-patch="/api/v1/users/current/cycle"
        data-settings-draft-form="cycle"
        data-settings-unsaved-prompt="Leave without saving?"
        data-settings-unsaved-accept="Discard">
        <input type="hidden" name="csrf_token" value="test-token">
        <input id="settings-cycle-length" type="range" min="15" max="90" name="cycle_length" value="28" data-settings-cycle-length>
        <span data-settings-cycle-length-value>28</span>
        <input id="settings-period-length" type="range" min="1" max="14" name="period_length" value="5" data-settings-period-length>
        <span data-settings-period-length-value>5</span>
        <label data-binary-toggle data-active="false">
          <input type="checkbox" name="unpredictable_cycle" value="true" data-binary-toggle-input>
        </label>
        <p data-settings-cycle-message="error" hidden></p>
        <p data-settings-cycle-message="warning" hidden></p>
        <p data-settings-cycle-message="adjusted" hidden></p>
        <p data-settings-cycle-message="period-long" hidden></p>
        <p data-settings-cycle-message="cycle-short" hidden></p>
        <button type="submit" data-settings-cycle-save>Save</button>
        <button type="button" data-settings-cycle-discard>Discard</button>
      </form>
    </details>

    <details id="settings-tracking" data-settings-section open>
      <form
        action="/api/v1/users/current/tracking"
        method="post"
        hx-patch="/api/v1/users/current/tracking"
        data-settings-draft-form="tracking"
        data-settings-unsaved-prompt="Leave without saving?"
        data-settings-unsaved-accept="Discard">
        <input type="hidden" name="csrf_token" value="test-token">
        <label data-binary-toggle data-active="false" data-tracking-setting="track-bbt">
          <input type="checkbox" name="track_bbt" value="true" data-binary-toggle-input>
        </label>
        <button type="submit" data-settings-tracking-save>Save</button>
        <button type="button" data-settings-tracking-discard>Discard</button>
      </form>
    </details>

    <details id="settings-interface" data-settings-section open>
      <form
        action="/api/v1/users/current/interface"
        method="post"
        hx-patch="/api/v1/users/current/interface"
        data-settings-draft-form="interface"
        data-settings-interface-form
        data-settings-unsaved-prompt="Leave without saving?"
        data-settings-unsaved-accept="Discard">
        <input type="hidden" name="csrf_token" value="test-token">
        <label data-settings-interface-language-option="en" data-selected="true">
          <input type="radio" name="language" value="en" checked>
        </label>
        <label data-settings-interface-language-option="ru" data-selected="false">
          <input type="radio" name="language" value="ru">
        </label>
        <label data-settings-interface-theme-option="light" data-selected="true">
          <input type="radio" name="theme" value="light" checked>
        </label>
        <label data-settings-interface-theme-option="dark" data-selected="false">
          <input type="radio" name="theme" value="dark">
        </label>
        <button type="submit" data-settings-interface-save>Save</button>
        <button type="button" data-settings-interface-discard>Discard</button>
      </form>
    </details>

    <details id="settings-account" data-settings-section open>
      <form id="settings-change-password-form" action="/api/v1/users/current/password" method="post" hx-put="/api/v1/users/current/password" novalidate>
        <input type="hidden" name="csrf_token" value="test-token">
        <input type="password" id="settings-current-password" name="current_password" autocomplete="current-password">
        <input type="password" id="settings-new-password" name="new_password" autocomplete="new-password">
        <button type="submit">Change password</button>
      </form>
    </details>

    <details id="settings-egress" data-settings-section open>
      <form action="/api/v1/users/current/webhook" method="post" hx-post="/api/v1/users/current/webhook" data-settings-webhook-form>
        <input type="hidden" name="csrf_token" value="test-token">
        <input type="url" id="settings-webhook-url" name="webhook_url" value="">
        <button type="submit">Save webhook</button>
      </form>
      <form hx-delete="/api/v1/users/current/webhook" hx-confirm="Withdraw?" data-settings-webhook-remove>
        <input type="hidden" name="csrf_token" value="test-token">
        <button type="submit">Withdraw endpoint</button>
      </form>
      <form hx-post="/api/v1/users/current/calendar-feed">
        <input type="hidden" name="csrf_token" value="test-token">
        <button type="submit">Enable feed</button>
      </form>
      <form hx-delete="/api/v1/users/current/calendar-feed" hx-confirm="Revoke?">
        <input type="hidden" name="csrf_token" value="test-token">
        <button type="submit">Revoke feed</button>
      </form>
    </details>

    <details id="settings-danger-zone" data-settings-section open>
      <form action="/api/v1/users/current/data-wipe" method="post" data-clear-data-verify-form data-clear-data-validate-action="/api/v1/users/current/data-wipe/validate">
        <input type="hidden" name="csrf_token" value="test-token">
        <input type="password" id="settings-clear-data-password" name="password" autocomplete="current-password">
        <button type="submit">Clear data</button>
      </form>
      <form hx-delete="/api/v1/users/current" hx-confirm="Delete account?">
        <input type="hidden" name="csrf_token" value="test-token">
        <input type="password" id="settings-delete-account-password" name="password" autocomplete="current-password">
        <button type="submit">Delete account</button>
      </form>
    </details>
  </div>
</body></html>`;
}

// Every outbound channel the bundle could reach for, not only `fetch`: a flush
// smuggled out through `sendBeacon`, an XHR or a native form submit would be the
// same defect wearing a different API.
function installEgressRecorder(window) {
  const calls = [];

  window.fetch = (url, init) => {
    calls.push({ channel: "fetch", url: String(url), init: init || {} });
    return Promise.resolve({
      ok: true,
      status: 200,
      headers: { get: () => null },
      text: () => Promise.resolve(""),
    });
  };

  Object.defineProperty(window.navigator, "sendBeacon", {
    configurable: true,
    value: (url) => {
      calls.push({ channel: "sendBeacon", url: String(url), init: {} });
      return true;
    },
  });

  const nativeOpen = window.XMLHttpRequest.prototype.open;
  window.XMLHttpRequest.prototype.open = function (method, url, ...rest) {
    calls.push({ channel: "xhr", url: String(url), init: { method: String(method) } });
    return nativeOpen.call(this, method, url, ...rest);
  };

  window.HTMLFormElement.prototype.submit = function () {
    calls.push({ channel: "form.submit", url: String(this.getAttribute("action") || ""), init: {} });
  };

  return calls;
}

async function loadSettings() {
  let calls = [];
  const dom = await loadDOMWithScript(APP_BUNDLE, {
    html: settingsPage(),
    url: "https://ovumcy.test/settings",
    beforeRun: (window) => {
      calls = installEgressRecorder(window);
    },
  });
  return { dom, calls: () => calls };
}

function draftForm(window, name) {
  return window.document.querySelector(`form[data-settings-draft-form="${name}"]`);
}

function fire(node, type) {
  node.dispatchEvent(new node.ownerDocument.defaultView.Event(type, { bubbles: true }));
}

// Returns whether the unsaved-leave prompt was raised: the handler cancels the
// event, which is the only signal a page gets to hand the browser.
function unload(window) {
  const event = new window.Event("beforeunload", { cancelable: true });
  window.dispatchEvent(event);
  return event.defaultPrevented;
}

function settle() {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

function describe(calls) {
  return calls.map((call) => `${call.channel} ${call.init.method || ""} ${call.url}`.trim());
}

test("a dirty cycle draft arms the leave prompt and puts nothing on the wire", async () => {
  const { dom, calls } = await loadSettings();
  try {
    const cycleLength = dom.window.document.querySelector("#settings-cycle-length");
    cycleLength.value = "31";
    fire(cycleLength, "input");

    assert.equal(
      draftForm(dom.window, "cycle").dataset.settingsDraftDirty,
      "true",
      "an edited cycle length is an outstanding draft"
    );
    assert.equal(unload(dom.window), true, "leaving with an unsaved draft must raise the prompt");
    await settle();

    assert.deepEqual(
      describe(calls()),
      [],
      "the settings shell writes on Save only: an unload must not persist a value the owner never confirmed"
    );
    assert.equal(
      dom.window.document.querySelector("#settings-cycle-length").value,
      "31",
      "the edit stays on screen for a cancelled navigation"
    );
  } finally {
    dom.window.close();
  }
});

test("a dirty tracking draft arms the leave prompt and puts nothing on the wire", async () => {
  const { dom, calls } = await loadSettings();
  try {
    const bbt = draftForm(dom.window, "tracking").querySelector('input[name="track_bbt"]');
    bbt.checked = true;
    fire(bbt, "change");

    assert.equal(draftForm(dom.window, "tracking").dataset.settingsDraftDirty, "true");
    assert.equal(unload(dom.window), true);
    await settle();

    assert.deepEqual(describe(calls()), []);
  } finally {
    dom.window.close();
  }
});

// The interface card previews the theme live and persists it only on Save, so a
// flush here would record a theme the owner was merely looking at — and a
// language they had not chosen yet.
test("a dirty interface draft arms the leave prompt and puts nothing on the wire", async () => {
  const { dom, calls } = await loadSettings();
  try {
    const dark = draftForm(dom.window, "interface").querySelector('input[name="theme"][value="dark"]');
    dark.checked = true;
    fire(dark, "change");

    assert.equal(draftForm(dom.window, "interface").dataset.settingsDraftDirty, "true");
    assert.equal(unload(dom.window), true);
    await settle();

    assert.deepEqual(describe(calls()), []);
  } finally {
    dom.window.close();
  }
});

test("an untouched settings page neither prompts nor sends", async () => {
  const { dom, calls } = await loadSettings();
  try {
    // Real time, well past the dashboard journal's 2 s debounce: nothing on this
    // page arms a timer that would fire behind the owner.
    await new Promise((resolve) => setTimeout(resolve, 2400));

    assert.equal(unload(dom.window), false, "a page with no outstanding edit must not block navigation");
    await settle();

    assert.deepEqual(describe(calls()), []);
  } finally {
    dom.window.close();
  }
});

// The destructive and security-relevant forms are outside the draft shell by
// construction: none of them carries `data-settings-draft-form`, so none is
// dirty-tracked, and the unload path cannot reach a credential, a wipe, an
// account deletion, a webhook URL or the calendar feed even in principle. Both
// halves are asserted — the membership rule, and the behaviour with every one of
// those fields filled in.
test("no destructive or security-relevant settings form joins the draft shell", async () => {
  const { dom, calls } = await loadSettings();
  try {
    const guarded = Array.from(
      dom.window.document.querySelectorAll("form[data-settings-draft-form]")
    ).map((form) => form.getAttribute("data-settings-draft-form"));
    assert.deepEqual(
      guarded.sort(),
      ["cycle", "interface", "tracking"],
      "only the three preference cards are dirty-tracked; adding a credential or erasure form here would put it on the unsaved-leave path"
    );

    for (const selector of [
      "#settings-current-password",
      "#settings-new-password",
      "#settings-webhook-url",
      "#settings-clear-data-password",
      "#settings-delete-account-password",
    ]) {
      const field = dom.window.document.querySelector(selector);
      field.value = selector === "#settings-webhook-url" ? "https://example.test/hook" : "correct horse battery staple";
      fire(field, "input");
      fire(field, "change");
    }

    assert.equal(
      dom.window.document.querySelectorAll('form[data-settings-draft-form][data-settings-draft-dirty="true"]').length,
      0
    );
    assert.equal(unload(dom.window), false, "a filled password field is not an unsaved draft");
    await settle();

    assert.deepEqual(
      describe(calls()),
      [],
      "no credential, wipe, deletion, webhook or feed request may ever leave on navigation"
    );
  } finally {
    dom.window.close();
  }
});

// A save already under way is the one moment the guard stands down (the form is
// marked navigating for 1.5 s so the prompt does not fight htmx). Leaving in that
// window must still add nothing of its own to the wire.
test("leaving while a settings save is under way adds no second request", async () => {
  const { dom, calls } = await loadSettings();
  try {
    const form = draftForm(dom.window, "tracking");
    const bbt = form.querySelector('input[name="track_bbt"]');
    bbt.checked = true;
    fire(bbt, "change");
    fire(form, "submit");

    assert.equal(form.dataset.settingsDraftNavigating, "1", "the submit hands the save to htmx");
    assert.equal(unload(dom.window), false, "a save in flight is not an unsaved draft");
    await settle();

    assert.deepEqual(describe(calls()), [], "htmx owns that request; the unload path issues none of its own");
  } finally {
    dom.window.close();
  }
});
