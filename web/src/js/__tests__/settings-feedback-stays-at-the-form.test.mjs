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
      scrolled.push({ id: this.id, inlineStyle: this.getAttribute("style"), options });
    };
    swapCard(dom.window, SYMPTOMS_AFTER);

    assert.equal(dom.window.document.activeElement.id, "settings-symptom-name-2", "focus follows the renamed row, not BODY");
    assert.equal(scrolled.length, 1);
    assert.equal(scrolled[0].id, "settings-symptom-row-2", "the row is scrolled, not the field alone");
    assert.equal(scrolled[0].inlineStyle, null, "the clearance is the settings-section class's, not a copy in an inline style");
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

// ---- review round: announcement, focus theft, swap pairing, toast key, coverage ----

const ROUND_BEFORE = `<input id="elsewhere" type="text">
${SYMPTOMS_BEFORE}`;

const ROUND_AFTER = SYMPTOMS_AFTER.replace(
  "data-focus-after-swap>",
  'data-focus-after-swap aria-describedby="settings-symptom-status-2">'
).replace(
  "</details>",
  '<div id="settings-symptom-status-2" role="status"><div class="status-ok">Symptom saved</div></div></details>'
);

function stubScroll(window) {
  const scrolled = [];
  window.HTMLElement.prototype.scrollIntoView = function () {
    scrolled.push(this.id);
  };
  return scrolled;
}

function beforeSwap(window, target, shouldSwap = true) {
  target.dispatchEvent(new window.CustomEvent("htmx:beforeSwap", { bubbles: true, detail: { target, shouldSwap } }));
}

function afterSwap(window, root, target) {
  root.dispatchEvent(new window.CustomEvent("htmx:afterSwap", { bubbles: true, detail: { target } }));
}

test("a swap that carries the marker announces the described message through one page-level live region", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: `<!doctype html><html><body>${ROUND_BEFORE}</body></html>` });
  try {
    const doc = dom.window.document;
    stubScroll(dom.window);
    swapCard(dom.window, ROUND_AFTER);
    const region = doc.querySelector("[data-swap-announcer]");
    assert.ok(region, "the announcer exists");
    assert.equal(region.getAttribute("aria-live"), "polite");
    assert.equal(region.textContent, "", "cleared first, so a repeat is announced again");
    await settle();
    assert.equal(region.textContent, "Symptom saved");

    swapCard(dom.window, ROUND_AFTER);
    await settle();
    assert.equal(doc.querySelectorAll("[data-swap-announcer]").length, 1, "one persistent region, not one per swap");
    assert.equal(doc.querySelector("[data-swap-announcer]").textContent, "Symptom saved");
    const stack = doc.querySelector(".toast-stack");
    assert.equal(stack ? stack.children.length : 0, 0, "no second feedback channel");
  } finally {
    dom.window.close();
  }
});

test("a swap does not steal focus the owner moved elsewhere, but still announces", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: `<!doctype html><html><body>${ROUND_BEFORE}</body></html>` });
  try {
    const doc = dom.window.document;
    const scrolled = stubScroll(dom.window);
    const old = doc.getElementById("settings-symptoms");
    // Pressed Save, then moved on while the request was in flight.
    doc.getElementById("elsewhere").focus();
    beforeSwap(dom.window, old);
    old.outerHTML = ROUND_AFTER;
    afterSwap(dom.window, doc.getElementById("settings-symptoms"), old);
    await settle();

    assert.equal(doc.activeElement.id, "elsewhere", "focus stays where the owner put it");
    assert.deepEqual(scrolled, [], "no scroll without a focus move");
    assert.equal(doc.querySelector("[data-swap-announcer]").textContent, "Symptom saved");
  } finally {
    dom.window.close();
  }
});

test("focus that was never inside the region and is lost afterwards is still repaired", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: `<!doctype html><html><body>${ROUND_BEFORE}</body></html>` });
  try {
    const doc = dom.window.document;
    stubScroll(dom.window);
    const old = doc.getElementById("settings-symptoms");
    beforeSwap(dom.window, old);
    old.outerHTML = ROUND_AFTER;
    afterSwap(dom.window, doc.getElementById("settings-symptoms"), old);
    assert.equal(doc.activeElement.id, "settings-symptom-name-2");
  } finally {
    dom.window.close();
  }
});

test("overlapping swaps each keep their own focus memory", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, {
    html: `<!doctype html><html><body>${SYMPTOMS_BEFORE}<div id="other-region"><p>x</p></div></body></html>`,
  });
  try {
    const doc = dom.window.document;
    dom.window.HTMLElement.prototype.scrollIntoView = function () {};
    const old = doc.getElementById("settings-symptoms");
    doc.getElementById("save-1").focus();
    beforeSwap(dom.window, old);
    beforeSwap(dom.window, doc.getElementById("other-region"));
    old.outerHTML = SYMPTOMS_AFTER.replace(" data-focus-after-swap", "");
    afterSwap(dom.window, doc.getElementById("settings-symptoms"), old);
    assert.equal(doc.activeElement.tagName, "SUMMARY", "the second beforeSwap did not overwrite the first target's state");
  } finally {
    dom.window.close();
  }
});

test("a beforeSwap that never swapped leaves no memory behind", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: `<!doctype html><html><body>${SYMPTOMS_BEFORE}</body></html>` });
  try {
    const doc = dom.window.document;
    dom.window.HTMLElement.prototype.scrollIntoView = function () {};
    const card = doc.getElementById("settings-symptoms");
    doc.getElementById("save-1").focus();
    beforeSwap(dom.window, card, false);
    card.dispatchEvent(new dom.window.CustomEvent("htmx:afterRequest", { bubbles: true, detail: { target: card } }));
    doc.activeElement.blur();
    afterSwap(dom.window, card, card);
    assert.equal(doc.activeElement.tagName, "BODY", "a later swap is not repaired on the strength of a swap that never happened");
  } finally {
    dom.window.close();
  }
});

for (const [label, trigger] of [
  ["a transport failure", "htmx:sendError"],
  ["the start of the next request", "htmx:beforeRequest"],
]) {
  test(`${label} forgets the repeat key an island with an id keeps on the page`, async () => {
    const dom = await loadDOMWithScript(APP_BUNDLE, { html: REMINDERS_PAGE });
    try {
      dom.window.__ovumcyLastToastByIsland = { "settings-reminders-status": "saved-key", "other-island": "keep" };
      const doc = dom.window.document;
      const island = doc.getElementById("settings-reminders-status");
      const form = doc.getElementById("reminders");
      form.dispatchEvent(new dom.window.CustomEvent(trigger, { bubbles: true, detail: { target: island, elt: form } }));
      assert.equal(dom.window.__ovumcyLastToastByIsland["settings-reminders-status"], undefined);
      assert.equal(dom.window.__ovumcyLastToastByIsland["other-island"], "keep", "only this island's key is forgotten");
    } finally {
      dom.window.close();
    }
  });
}

const NON_SETTINGS_PAGE = `<!doctype html><html lang="en"><head></head>
<body data-request-failed="Request failed. Please try again.">
  <form id="usage-goal" hx-patch="/api/v1/users/current/cycle" hx-target="#usage-goal-status" data-save-feedback>
    <button type="submit" data-save-button>Save</button>
    <div id="usage-goal-status" class="save-status" aria-live="polite"><div class="status-ok">Saved</div></div>
  </form>
  <form id="no-island" hx-post="/x" hx-target="#nowhere"><button type="submit">Go</button></form>
</body></html>`;

test("a transport failure outside the settings page says so in the form's own island", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: NON_SETTINGS_PAGE });
  try {
    const doc = dom.window.document;
    const island = doc.getElementById("usage-goal-status");
    doc.getElementById("usage-goal").dispatchEvent(
      new dom.window.CustomEvent("htmx:timeout", { bubbles: true, detail: { target: island, elt: doc.getElementById("usage-goal") } })
    );
    assert.equal(island.textContent.trim(), "Request failed. Please try again.");

    const lonely = doc.getElementById("no-island");
    lonely.dispatchEvent(new dom.window.CustomEvent("htmx:timeout", { bubbles: true, detail: { elt: lonely } }));
    assert.equal(doc.querySelectorAll(".status-error").length, 1, "a form with no resolvable island is left alone");
  } finally {
    dom.window.close();
  }
});
