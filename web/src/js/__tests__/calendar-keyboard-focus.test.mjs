// The month grid is one Tab stop with arrow-key movement, opening a day moves
// focus to the day panel's heading, and Escape from the panel returns to the
// selected day (WEB-228). Before this, every day was its own Tab stop, opening
// a day left focus on the cell (or, after Edit, on the body) and the panel
// below the month on a phone looked like a no-op.

import test from "node:test";
import assert from "node:assert/strict";
import { readAppBundle, loadDOMWithScript } from "./_helpers.mjs";

const APP_BUNDLE = readAppBundle();

function dayButton(day, { disabled = false, today = false } = {}) {
  const date = `2026-10-${String(day).padStart(2, "0")}`;
  return `<button type="button" data-day="${date}"${disabled ? " disabled" : ""}${today ? ' aria-current="date"' : ""}>${day}</button>`;
}

// Two weeks: the 1st and the 9th are outside the history bounds, the 10th is
// today.
function calendarPage(selectedDate) {
  const cells = [];
  for (let day = 1; day <= 14; day++) {
    cells.push(dayButton(day, { disabled: day === 1 || day === 9, today: day === 10 }));
  }
  return `<!doctype html><html><head></head><body>
  <div data-calendar-view data-selected-date="${selectedDate}">
    <section id="calendar-grid-panel">${cells.join("")}</section>
    <aside id="day-editor"><div data-day-editor-autoload></div></aside>
  </div></body></html>`;
}

const PANEL = `<div role="region" aria-labelledby="day-editor-heading">
  <h2 id="day-editor-heading" tabindex="-1" data-day-editor-heading>Sat, Oct 10</h2>
  <textarea name="notes"></textarea>
</div>`;

async function load(selectedDate = "", { scrolls } = {}) {
  return loadDOMWithScript(APP_BUNDLE, {
    html: calendarPage(selectedDate),
    beforeRun(window) {
      window.matchMedia = () => ({ matches: false, addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {} });
      window.Element.prototype.scrollIntoView = function (arg) {
        if (scrolls) scrolls.push({ element: this, arg });
      };
    },
  });
}

function cell(document, day) {
  return document.querySelector(`button[data-day="2026-10-${String(day).padStart(2, "0")}"]`);
}

function key(window, element, name) {
  element.dispatchEvent(new window.KeyboardEvent("keydown", { key: name, bubbles: true, cancelable: true }));
}

function swapPanel(window, source) {
  const editor = window.document.getElementById("day-editor");
  editor.innerHTML = PANEL;
  editor.dispatchEvent(
    new window.CustomEvent("htmx:afterSwap", {
      detail: { target: editor, requestConfig: { elt: source } },
      bubbles: true,
    }),
  );
  return editor;
}

test("the month is one Tab stop: today without a selection, the selected day with one", async () => {
  for (const [selected, tabStop] of [["", 10], ["2026-10-04", 4]]) {
    const dom = await load(selected);
    try {
      const document = dom.window.document;
      const stops = [...document.querySelectorAll("button[data-day]")].filter((button) => button.getAttribute("tabindex") === "0");
      assert.deepEqual(stops, [cell(document, tabStop)], `selected=${selected || "none"}`);
      for (const button of document.querySelectorAll("button[data-day]")) {
        if (button.disabled) {
          assert.equal(button.hasAttribute("aria-pressed"), false, "a disabled day claims no pressed state");
          continue;
        }
        assert.equal(button.getAttribute("aria-pressed"), button.getAttribute("data-day") === selected ? "true" : "false");
      }
    } finally {
      dom.window.close();
    }
  }
});

test("arrow keys move between open days by day and by week, skipping closed ones", async () => {
  const dom = await load("2026-10-02");
  try {
    const { window } = dom;
    const document = window.document;
    cell(document, 2).focus();

    key(window, cell(document, 2), "ArrowRight");
    assert.equal(document.activeElement, cell(document, 3));
    key(window, cell(document, 3), "ArrowLeft");
    assert.equal(document.activeElement, cell(document, 2));
    key(window, cell(document, 2), "ArrowLeft");
    assert.equal(document.activeElement, cell(document, 2), "the closed 1st is skipped and nothing lies before it");
    key(window, cell(document, 2), "ArrowDown");
    assert.equal(document.activeElement, cell(document, 2), "the closed 9th is skipped and nothing lies a week past it");
    key(window, cell(document, 3), "ArrowDown");
    assert.equal(document.activeElement, cell(document, 10));
    key(window, cell(document, 10), "ArrowUp");
    assert.equal(document.activeElement, cell(document, 3));

    const stops = [...document.querySelectorAll("button[data-day]")].filter((button) => button.getAttribute("tabindex") === "0");
    assert.deepEqual(stops, [cell(document, 3)], "the Tab stop follows focus");
  } finally {
    dom.window.close();
  }
});

test("opening a day from the grid moves focus to the panel heading and brings an off-screen panel into view", async () => {
  const scrolls = [];
  const dom = await load("", { scrolls });
  try {
    const { window } = dom;
    window.HTMLHeadingElement.prototype.getBoundingClientRect = () => ({ top: 2000, bottom: 2030 });
    const day = cell(window.document, 4);
    day.focus();
    const editor = swapPanel(window, day);
    assert.equal(window.document.activeElement, editor.querySelector("[data-day-editor-heading]"));
    assert.deepEqual(scrolls.filter((entry) => entry.element === editor).map((entry) => entry.arg.block), ["start"]);
  } finally {
    dom.window.close();
  }
});

test("a panel already on screen takes focus without a scroll", async () => {
  const scrolls = [];
  const dom = await load("", { scrolls });
  try {
    const { window } = dom;
    window.HTMLHeadingElement.prototype.getBoundingClientRect = () => ({ top: 100, bottom: 130 });
    const editor = swapPanel(window, cell(window.document, 4));
    assert.equal(window.document.activeElement, editor.querySelector("[data-day-editor-heading]"));
    assert.equal(scrolls.length, 0);
  } finally {
    dom.window.close();
  }
});

test("the panel a ?day= URL loads on its own leaves focus alone", async () => {
  const dom = await load("2026-10-04");
  try {
    const { window } = dom;
    const autoload = window.document.querySelector("[data-day-editor-autoload]");
    swapPanel(window, autoload);
    assert.equal(window.document.activeElement, window.document.body);
  } finally {
    dom.window.close();
  }
});

test("Escape inside the panel returns focus to the selected day", async () => {
  const dom = await load("2026-10-04");
  try {
    const { window } = dom;
    const editor = swapPanel(window, cell(window.document, 4));
    const notes = editor.querySelector("textarea");
    notes.focus();
    key(window, notes, "Escape");
    assert.equal(window.document.activeElement, cell(window.document, 4));
  } finally {
    dom.window.close();
  }
});
