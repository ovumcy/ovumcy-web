// A settings save must not send the owner to the top of the page, and must not
// leave them guessing: its feedback sits at the form, focus never drops to BODY,
// and a request that got no answer says so. Each test pins one such property
// against the shipped bundle.

import test from "node:test";
import assert from "node:assert/strict";
import { readAppBundle, loadDOMWithScript } from "./_helpers.mjs";

const APP_BUNDLE = readAppBundle();

const DRAFT_PAGE = `<!doctype html><html lang="en"><head><meta name="csrf-token" content="t"></head>
<body data-request-failed="Request failed. Please try again.">
  <div data-settings-sections>
    <details id="settings-cycle" data-settings-cycle-form data-settings-section data-status-island-host open>
      <form
        action="/api/v1/users/current/cycle" method="post"
        hx-patch="/api/v1/users/current/cycle"
        data-settings-draft-form="cycle"
        data-settings-unsaved-prompt="Leave without saving?"
        data-settings-unsaved-accept="Discard">
        <input id="settings-cycle-length" type="range" min="15" max="90" name="cycle_length" value="28" data-settings-cycle-length>
        <span data-settings-cycle-length-value>28</span>
        <input id="settings-period-length" type="range" min="1" max="14" name="period_length" value="5" data-settings-period-length>
        <span data-settings-period-length-value>5</span>
        <button type="submit" data-settings-cycle-save>Save</button>
        <button type="button" data-settings-cycle-discard>Discard</button>
      </form>
    </details>
    <details id="settings-tracking" data-settings-section open>
      <form
        id="tracking-form"
        action="/api/v1/users/current/tracking" method="post"
        hx-patch="/api/v1/users/current/tracking"
        data-settings-draft-form="tracking"
        data-settings-unsaved-prompt="Leave without saving?"
        data-settings-unsaved-accept="Discard">
        <label data-binary-toggle data-active="false" data-tracking-setting="track-bbt">
          <input type="checkbox" name="track_bbt" value="true" data-binary-toggle-input>
        </label>
        <button type="submit" data-settings-tracking-save>Save</button>
        <button type="button" data-settings-tracking-discard>Discard</button>
      </form>
    </details>
  </div>
</body></html>`;

function fire(node, type) {
  node.dispatchEvent(new node.ownerDocument.defaultView.Event(type, { bubbles: true }));
}

function settle() {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

// Dirties the cycle draft, then submits the tracking form. Returns what the
// submit did: whether it reached the form (the point at which htmx starts the
// request) and whether the default was cancelled.
function submitTrackingWithDirtyCycle(window, answer) {
  const doc = window.document;
  const prompts = [];
  window.__ovumcyOpenConfirm = (message) => {
    prompts.push(message);
    return Promise.resolve(answer);
  };
  const length = doc.querySelector("#settings-cycle-length");
  length.value = "31";
  fire(length, "input");
  assert.equal(doc.querySelector('form[data-settings-draft-form="cycle"]').dataset.settingsDraftDirty, "true");

  const form = doc.querySelector("#tracking-form");
  let reached = 0;
  form.addEventListener("submit", () => {
    reached += 1;
  });
  const event = new window.Event("submit", { bubbles: true, cancelable: true });
  form.dispatchEvent(event);
  return { prompts, event, reached: () => reached };
}

test("saving one draft form asks before it discards another form's unsaved draft", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: DRAFT_PAGE, url: "https://ovumcy.test/settings" });
  try {
    const run = submitTrackingWithDirtyCycle(dom.window, false);
    await settle();

    assert.deepEqual(run.prompts, ["Leave without saving?"], "the in-app prompt is raised once");
    assert.equal(run.event.defaultPrevented, true);
    assert.equal(run.reached(), 0, "the save does not start while the question is open");
    assert.equal(dom.window.document.querySelector("#settings-cycle-length").value, "31", "cancel keeps the other draft");
  } finally {
    dom.window.close();
  }
});

test("accepting the prompt discards the other draft and lets the save through", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: DRAFT_PAGE, url: "https://ovumcy.test/settings" });
  try {
    dom.window.HTMLFormElement.prototype.requestSubmit = function () {
      this.dispatchEvent(new dom.window.Event("submit", { bubbles: true, cancelable: true }));
    };
    const run = submitTrackingWithDirtyCycle(dom.window, true);
    await settle();

    assert.equal(run.reached(), 1, "the save proceeds once, after the answer");
    assert.equal(dom.window.document.querySelector("#settings-cycle-length").value, "28", "the other draft is discarded");
  } finally {
    dom.window.close();
  }
});

test("a draft form saving its own draft is not asked about it", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: DRAFT_PAGE, url: "https://ovumcy.test/settings" });
  try {
    const prompts = [];
    dom.window.__ovumcyOpenConfirm = (message) => {
      prompts.push(message);
      return Promise.resolve(false);
    };
    const bbt = dom.window.document.querySelector('input[name="track_bbt"]');
    bbt.checked = true;
    fire(bbt, "change");
    const form = dom.window.document.querySelector("#tracking-form");
    const event = new dom.window.Event("submit", { bubbles: true, cancelable: true });
    form.dispatchEvent(event);

    assert.deepEqual(prompts, []);
    assert.equal(event.defaultPrevented, false);
  } finally {
    dom.window.close();
  }
});

const REMINDERS_PAGE = `<!doctype html><html lang="en"><head></head>
<body data-request-failed="Request failed. Please try again.">
  <details id="settings-cycle" data-settings-section open>
    <form id="reminders" hx-patch="/api/v1/users/current/reminders" hx-target="#settings-reminders-status" data-save-feedback>
      <button type="submit" data-save-button>Save</button>
      <div id="settings-reminders-status" class="save-status" aria-live="polite"><div class="status-ok">Saved</div></div>
    </form>
  </details>
  <div id="outside" class="save-status"></div>
</body></html>`;

for (const name of ["htmx:sendError", "htmx:sendAbort", "htmx:timeout"]) {
  test(`${name} on a settings form replaces a stale success with the failure copy`, async () => {
    const dom = await loadDOMWithScript(APP_BUNDLE, { html: REMINDERS_PAGE });
    try {
      const doc = dom.window.document;
      const island = doc.getElementById("settings-reminders-status");
      const form = doc.getElementById("reminders");
      form.querySelector("[data-save-button]").disabled = true;
      form.dispatchEvent(
        new dom.window.CustomEvent(name, { bubbles: true, detail: { target: island, elt: form } })
      );

      assert.equal(island.textContent.trim(), "Request failed. Please try again.");
      assert.equal(island.querySelector(".status-ok"), null, "the stale success is gone");
      assert.ok(island.querySelector(".status-error"));
      assert.equal(form.querySelector("[data-save-button]").disabled, false, "the form can be saved again");
    } finally {
      dom.window.close();
    }
  });
}

const SYMPTOMS_BEFORE = `<details id="settings-symptoms" data-settings-section data-status-island-host open>
  <summary>Symptoms</summary>
  <div id="settings-symptom-row-1" data-focus-scroll-anchor>
    <input id="settings-symptom-name-1" type="text" name="name" value="Zeta"><button id="save-1" type="submit">Save</button>
  </div>
</details>`;

const SYMPTOMS_AFTER = `<details id="settings-symptoms" data-settings-section data-status-island-host open>
  <summary>Symptoms</summary>
  <div id="settings-symptom-row-2" data-focus-scroll-anchor>
    <input id="settings-symptom-name-2" type="text" name="name" value="Alpha" data-focus-after-swap>
  </div>
  <div id="settings-symptom-row-1" data-focus-scroll-anchor>
    <input id="settings-symptom-name-1" type="text" name="name" value="Zeta"><button id="save-1" type="submit">Save</button>
  </div>
</details>`;

function swapCard(window, afterHTML) {
  const doc = window.document;
  const old = doc.getElementById("settings-symptoms");
  doc.getElementById("save-1").focus();
  old.dispatchEvent(new window.CustomEvent("htmx:beforeSwap", { bubbles: true, detail: { target: old, shouldSwap: true } }));
  old.outerHTML = afterHTML;
  const fresh = doc.getElementById("settings-symptoms");
  fresh.dispatchEvent(new window.CustomEvent("htmx:afterSwap", { bubbles: true, detail: { target: old } }));
  return fresh;
}

test("a self-replacing card puts focus on the marked control and scrolls its row clear of the sticky header", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: `<!doctype html><html><body>${SYMPTOMS_BEFORE}</body></html>` });
  try {
    const scrolled = [];
    dom.window.HTMLElement.prototype.scrollIntoView = function (options) {
      scrolled.push({ id: this.id, margin: this.style.scrollMarginTop, options });
    };
    swapCard(dom.window, SYMPTOMS_AFTER);

    assert.equal(dom.window.document.activeElement.id, "settings-symptom-name-2", "focus follows the renamed row, not BODY");
    assert.equal(scrolled.length, 1);
    assert.equal(scrolled[0].id, "settings-symptom-row-2", "the row is scrolled, not the field alone");
    assert.match(scrolled[0].margin, /--sticky-header-clearance/);
  } finally {
    dom.window.close();
  }
});

test("a swap that loses focus and names no marker lands on the card's summary", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: `<!doctype html><html><body>${SYMPTOMS_BEFORE}</body></html>` });
  try {
    dom.window.HTMLElement.prototype.scrollIntoView = function () {};
    swapCard(dom.window, SYMPTOMS_AFTER.replace(" data-focus-after-swap", ""));

    assert.equal(dom.window.document.activeElement.tagName, "SUMMARY");
  } finally {
    dom.window.close();
  }
});
