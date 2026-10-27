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
function calendarPage(selectedDate, autoload = "") {
  const cells = [];
  for (let day = 1; day <= 14; day++) {
    cells.push(dayButton(day, { disabled: day === 1 || day === 9, today: day === 10 }));
  }
  return `<!doctype html><html><head></head><body>
  <header class="paper-header"></header>
  <div data-calendar-month-nav>
    <a href="/calendar?month=2026-09" data-calendar-nav-month="2026-09">Prev</a>
    <a href="/calendar?month=2026-11" data-calendar-nav-month="2026-11">Next</a>
    <a href="/calendar" data-calendar-today data-calendar-nav-month="2026-10" data-calendar-nav-today="2026-10-10">Today</a>
  </div>
  <div data-calendar-view data-selected-date="${selectedDate}">
    <section id="calendar-grid-panel">${cells.join("")}</section>
    <aside id="day-editor"><div data-day-editor-autoload="${autoload}"></div></aside>
  </div></body></html>`;
}

const PANEL = `<div role="region" aria-labelledby="day-editor-heading">
  <h2 id="day-editor-heading" tabindex="-1" data-day-editor-heading>Sat, Oct 10</h2>
  <textarea name="notes"></textarea>
</div>`;

async function load(selectedDate = "", { scrolls = [], reduce = false, autoload = "" } = {}) {
  return loadDOMWithScript(APP_BUNDLE, {
    html: calendarPage(selectedDate, autoload),
    beforeRun(window) {
      window.matchMedia = (query) => ({
        matches: query === "(prefers-reduced-motion: reduce)" ? reduce : false,
        addEventListener() {},
        removeEventListener() {},
        addListener() {},
        removeListener() {},
      });
      window.scrollTo = (arg) => scrolls.push(arg);
    },
  });
}

function rect(top, bottom) {
  return () => ({ top, bottom, left: 0, right: 0, width: 0, height: bottom - top });
}

// Lays the page out: an 80px sticky header, the month ending at gridBottom,
// the panel starting at panelTop, and its heading 20px into it.
function layout(window, { gridBottom, panelTop }) {
  const document = window.document;
  document.querySelector(".paper-header").getBoundingClientRect = rect(0, 80);
  document.getElementById("calendar-grid-panel").getBoundingClientRect = rect(100, gridBottom);
  document.getElementById("day-editor").getBoundingClientRect = rect(panelTop, panelTop + 600);
  window.HTMLHeadingElement.prototype.getBoundingClientRect = rect(panelTop + 20, panelTop + 50);
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

// The reported phone case: at 375×812 the panel starts at 630, under the
// month, so its heading is technically on screen while everything it opened
// is below the fold.
for (const reduce of [false, true]) {
  test(`a panel stacked below the month is brought up under the header, even with its heading on screen (reduce=${reduce})`, async () => {
    const scrolls = [];
    const dom = await load("", { scrolls, reduce });
    try {
      const { window } = dom;
      layout(window, { gridBottom: 610, panelTop: 630 });
      const day = cell(window.document, 4);
      day.focus();
      const editor = swapPanel(window, day);
      assert.equal(window.document.activeElement, editor.querySelector("[data-day-editor-heading]"));
      // Spread: the options object is built in the page's realm.
      assert.deepEqual(scrolls.map((entry) => ({ ...entry })), [{ top: 630 - 80 - 12, behavior: reduce ? "auto" : "smooth" }]);
    } finally {
      dom.window.close();
    }
  });
}

test("a panel beside the month takes focus without a scroll while its heading is in view", async () => {
  const scrolls = [];
  const dom = await load("", { scrolls });
  try {
    const { window } = dom;
    layout(window, { gridBottom: 900, panelTop: 100 });
    const editor = swapPanel(window, cell(window.document, 4));
    assert.equal(window.document.activeElement, editor.querySelector("[data-day-editor-heading]"));
    assert.deepEqual(scrolls, []);
  } finally {
    dom.window.close();
  }
});

test("a panel beside the month whose heading is under the sticky header is brought down", async () => {
  const scrolls = [];
  const dom = await load("", { scrolls });
  try {
    const { window } = dom;
    layout(window, { gridBottom: 600, panelTop: -200 });
    swapPanel(window, cell(window.document, 4));
    assert.equal(scrolls.length, 1);
    assert.equal(scrolls[0].top, -200 - 80 - 12);
  } finally {
    dom.window.close();
  }
});

test("the arrowed-to day keeps the Tab stop across a swap; a click hands it back to the selection", async () => {
  const dom = await load("2026-10-02");
  try {
    const { window } = dom;
    const document = window.document;
    cell(document, 2).focus();
    key(window, cell(document, 2), "ArrowRight");
    key(window, cell(document, 3), "ArrowRight");
    document.body.dispatchEvent(new window.CustomEvent("htmx:afterSwap", { detail: { target: document.body }, bubbles: true }));
    const stop = () => [...document.querySelectorAll("button[data-day]")].filter((button) => button.getAttribute("tabindex") === "0");
    assert.deepEqual(stop(), [cell(document, 4)]);

    cell(document, 6).click();
    assert.deepEqual(stop(), [cell(document, 6)]);
  } finally {
    dom.window.close();
  }
});

test("the panel a ?day= URL loads on its own leaves focus and scroll alone", async () => {
  const scrolls = [];
  const dom = await load("2026-10-04", { scrolls });
  try {
    const { window } = dom;
    layout(window, { gridBottom: 610, panelTop: 630 });
    const autoload = window.document.querySelector("[data-day-editor-autoload]");
    swapPanel(window, autoload);
    assert.equal(window.document.activeElement, window.document.body);
    assert.deepEqual(scrolls, []);
  } finally {
    dom.window.close();
  }
});

// A link that asked to edit a day (?day=…&edit=1) lands on a phone with the
// editor below the month: it is brought up, but focus stays where the page
// load put it, since nothing on this page had it to hand over (WEB-228).
test("a ?day= panel loaded in edit mode is brought into view without taking focus", async () => {
  const scrolls = [];
  const dom = await load("2026-10-04", { scrolls, autoload: "edit" });
  try {
    const { window } = dom;
    layout(window, { gridBottom: 610, panelTop: 630 });
    const autoload = window.document.querySelector("[data-day-editor-autoload]");
    swapPanel(window, autoload);
    assert.equal(window.document.activeElement, window.document.body);
    assert.deepEqual(scrolls.map((entry) => ({ ...entry })), [{ top: 630 - 80 - 12, behavior: "smooth" }]);
  } finally {
    dom.window.close();
  }
});

// Month navigation keeps the chosen day open (WEB-265); a day chosen without
// a page load is carried too. Today with today (or nothing else) selected is a
// bare /calendar.
test("choosing a day carries it through Prev, Next and Today", async () => {
  const dom = await load("");
  try {
    const { window } = dom;
    const document = window.document;
    const hrefs = () => [...document.querySelectorAll("[data-calendar-month-nav] a")].map((link) => link.getAttribute("href"));

    cell(document, 4).click();
    assert.deepEqual(hrefs(), [
      "/calendar?month=2026-09&day=2026-10-04",
      "/calendar?month=2026-11&day=2026-10-04",
      "/calendar?month=2026-10&day=2026-10-04",
    ]);

    cell(document, 10).click();
    assert.deepEqual(hrefs(), [
      "/calendar?month=2026-09&day=2026-10-10",
      "/calendar?month=2026-11&day=2026-10-10",
      "/calendar",
    ]);
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
