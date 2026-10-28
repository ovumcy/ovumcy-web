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
const KEY = "dashboard.period_off_confirm.moves_start";

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

// `answer`, when given, is the X-Ovumcy-Period-Off-Confirm value every save is
// answered with (null: the header is absent); without it saves never answer.
async function load(options = {}) {
  const dom = await loadDOMWithScript(APP_BUNDLE, {
    html: page(options),
    beforeRun: (window) => {
      if (!("answer" in options)) {
        window.fetch = () => new Promise(() => {});
        return;
      }
      window.fetch = () => Promise.resolve({
        ok: true,
        status: 200,
        headers: { get: (name) => (name === "X-Ovumcy-Period-Off-Confirm" ? options.answer : name === "X-Ovumcy-Period-Off-Confirm-Key" ? KEY : null) },
        text: () => Promise.resolve(""),
      });
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

test("the quick action un-ticks a toggle without the question as the owner's click would", async () => {
  const { window, toggle, modal, events } = await load({ question: "" });
  try {
    window.document.querySelector("[data-quick-action='period']").click();
    assert.equal(toggle.checked, false);
    assert.equal(modal.classList.contains("hidden"), true);
    assert.deepEqual(events, ["input", "change"]);
  } finally {
    window.close();
  }
});

// The form stays open after its save, so the save's answer replaces the
// question the toggle was rendered with: a saved tick can make the next
// un-tick reach past the day, a saved un-tick leaves nothing to ask.
const flushAndSettle = async (window) => {
  await settle();
  window.dispatchEvent(new window.Event("pagehide"));
  for (let round = 0; round < 5; round++) {
    await settle();
  }
};

test("a saved tick hands the toggle the question its next un-tick asks", async () => {
  const { window, toggle, modal } = await load({ question: "", checked: false, answer: encodeURIComponent(QUESTION) });
  try {
    toggle.click();
    await flushAndSettle(window);
    assert.equal(toggle.getAttribute("data-period-off-confirm"), QUESTION);
    assert.equal(toggle.getAttribute("data-period-off-confirm-key"), KEY);

    toggle.click();
    assert.equal(toggle.checked, true, "the un-tick after the save is held for the question");
    assert.equal(modal.classList.contains("hidden"), false);
  } finally {
    window.close();
  }
});

test("the calendar editor's save answer hands its toggle the question too", async () => {
  const { window } = await load();
  try {
    const { document } = window;
    document.body.insertAdjacentHTML("beforeend", `<form data-save-feedback data-day-editor-form>
      <input type="checkbox" name="is_period" value="true" data-period-toggle checked>
    </form>`);
    const editor = document.querySelector("[data-day-editor-form]");
    const editorToggle = editor.querySelector("[data-period-toggle]");
    const answer = (successful, header) => editor.dispatchEvent(new window.CustomEvent("htmx:afterRequest", {
      bubbles: true,
      detail: {
        elt: editor,
        successful,
        xhr: { responseText: "", getResponseHeader: (name) => (name === "X-Ovumcy-Period-Off-Confirm" ? header : name === "X-Ovumcy-Period-Off-Confirm-Key" ? KEY : null) },
      },
    }));

    answer(false, encodeURIComponent(QUESTION));
    assert.equal(editorToggle.hasAttribute("data-period-off-confirm"), false, "a refused save changes nothing");

    answer(true, encodeURIComponent(QUESTION));
    assert.equal(editorToggle.getAttribute("data-period-off-confirm"), QUESTION);
    assert.equal(editorToggle.getAttribute("data-period-off-confirm-key"), KEY);

    answer(true, "");
    assert.equal(editorToggle.hasAttribute("data-period-off-confirm"), false);
    assert.equal(editorToggle.hasAttribute("data-period-off-confirm-key"), false);
  } finally {
    window.close();
  }
});

test("a saved un-tick takes the question away, and an answer without the header keeps it", async () => {
  const cleared = await load({ answer: "" });
  try {
    cleared.toggle.click();
    cleared.window.document.getElementById("confirm-modal-accept").click();
    await flushAndSettle(cleared.window);
    assert.equal(cleared.toggle.hasAttribute("data-period-off-confirm"), false);
    assert.equal(cleared.toggle.hasAttribute("data-period-off-confirm-key"), false);
  } finally {
    cleared.window.close();
  }

  const kept = await load({ answer: null });
  try {
    kept.toggle.click();
    kept.window.document.getElementById("confirm-modal-accept").click();
    await flushAndSettle(kept.window);
    assert.equal(kept.toggle.getAttribute("data-period-off-confirm"), QUESTION);
  } finally {
    kept.window.close();
  }
});
