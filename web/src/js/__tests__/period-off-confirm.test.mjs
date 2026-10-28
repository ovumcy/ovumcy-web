// WEB-267: un-ticking a period day whose un-tick reaches past it — the cycle
// start moves, auto-filled days are cleared — asks first. The server writes
// the question onto the toggle (data-period-off-confirm) only when there is
// such an effect. A declined question leaves the box ticked with no input or
// change event behind, so the day forms mark nothing for saving; an accepted
// one un-ticks the box with both. A toggle without the question un-ticks at
// once, and ticking never asks.

import test from "node:test";
import assert from "node:assert/strict";
import { readAppBundle, loadDOMWithScript } from "./_helpers.mjs";

const APP_BUNDLE = readAppBundle();
const QUESTION = "Un-marking this day moves the start of your cycle. Your statistics will be recalculated.";
const ACCEPT = "Yes, un-mark";

function page({ question = QUESTION, checked = true } = {}) {
  const asks = question ? ` data-period-off-confirm="${question}" data-period-off-confirm-accept="${ACCEPT}"` : "";
  return `<!doctype html><html><head><meta name="csrf-token" content="unit-test-token"></head><body
  data-confirm-cancel="Cancel" data-confirm-delete="Delete">
  <div data-dashboard-editor>
    <button type="button" data-quick-action="period">Period</button>
    <form hx-put="/api/v1/days/2026-08-12" data-dashboard-save-form data-dashboard-date="2026-08-12">
      <input type="hidden" name="csrf_token" value="unit-test-token">
      <label class="period-toggle" data-binary-toggle>
        <input type="checkbox" name="is_period" value="true" data-period-toggle data-binary-toggle-input${checked ? " checked" : ""}${asks}>
      </label>
      <div class="dashboard-autosave-indicator" data-dashboard-autosave-indicator data-autosave-state="idle"></div>
    </form>
  </div>
  <div id="confirm-modal" class="hidden" role="dialog" aria-modal="true" aria-hidden="true">
    <p id="confirm-modal-message"></p>
    <button id="confirm-modal-cancel" type="button"></button>
    <button id="confirm-modal-accept" type="button"></button>
  </div>
</body></html>`;
}

async function load(options) {
  const dom = await loadDOMWithScript(APP_BUNDLE, {
    html: page(options),
    beforeRun: (window) => {
      window.fetch = () => new Promise(() => {});
    },
  });
  const { document } = dom.window;
  const events = [];
  const form = document.querySelector("[data-dashboard-save-form]");
  form.addEventListener("input", () => events.push("input"));
  form.addEventListener("change", () => events.push("change"));
  return {
    dom,
    window: dom.window,
    toggle: document.querySelector("[data-period-toggle]"),
    modal: document.getElementById("confirm-modal"),
    message: document.getElementById("confirm-modal-message"),
    events,
  };
}

const settle = () => new Promise((resolve) => setTimeout(resolve, 0));

test("un-ticking a toggle that carries the question asks before the box changes", async () => {
  const { window, toggle, modal, message, events } = await load();
  try {
    toggle.click();
    assert.equal(toggle.checked, true, "the box stays ticked while the question is open");
    assert.deepEqual(events, [], "no input or change event reaches the form before the answer");
    assert.equal(modal.classList.contains("hidden"), false, "the confirm modal opens");
    assert.equal(message.textContent, QUESTION);
    assert.equal(window.document.getElementById("confirm-modal-accept").textContent, ACCEPT);
  } finally {
    window.close();
  }
});

test("declining the question keeps the period and marks nothing for saving", async () => {
  const { window, toggle, events } = await load();
  try {
    toggle.click();
    window.document.getElementById("confirm-modal-cancel").click();
    await settle();
    assert.equal(toggle.checked, true);
    assert.deepEqual(events, []);
  } finally {
    window.close();
  }
});

test("accepting the question un-ticks the box with its input and change events", async () => {
  const { window, toggle, events } = await load();
  try {
    toggle.click();
    window.document.getElementById("confirm-modal-accept").click();
    await settle();
    assert.equal(toggle.checked, false);
    assert.deepEqual(events, ["input", "change"]);
  } finally {
    window.close();
  }
});

test("the dashboard's period quick action asks the same question before un-ticking", async () => {
  const { window, toggle, modal, events } = await load();
  try {
    window.document.querySelector("[data-quick-action='period']").click();
    assert.equal(toggle.checked, true);
    assert.equal(modal.classList.contains("hidden"), false);
    assert.deepEqual(events, []);
  } finally {
    window.close();
  }
});

test("a toggle without the question un-ticks at once, and ticking never asks", async () => {
  const plain = await load({ question: "" });
  try {
    plain.toggle.click();
    assert.equal(plain.toggle.checked, false);
    assert.equal(plain.modal.classList.contains("hidden"), true);
    assert.deepEqual(plain.events, ["input", "change"]);
  } finally {
    plain.window.close();
  }

  const unticked = await load({ checked: false });
  try {
    unticked.toggle.click();
    assert.equal(unticked.toggle.checked, true);
    assert.equal(unticked.modal.classList.contains("hidden"), true);
  } finally {
    unticked.window.close();
  }
});
