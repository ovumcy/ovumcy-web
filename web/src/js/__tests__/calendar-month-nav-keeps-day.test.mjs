// Prev and Next keep the open day (WEB-265). The server renders them with the
// day the page was loaded on, and a day opened afterwards from the grid is a
// client-side swap, so the links take its date too; without that, stepping a
// month dropped the day the owner had just opened. Today keeps meaning today.

import test from "node:test";
import assert from "node:assert/strict";
import { readAppBundle, loadDOMWithScript } from "./_helpers.mjs";

const APP_BUNDLE = readAppBundle();

function calendarPage(selectedDate) {
  const day = selectedDate ? `&day=${selectedDate}` : "";
  return `<!doctype html><html><head></head><body>
  <div data-calendar-month-nav>
    <a href="/calendar?month=2026-09${day}" class="btn-secondary">Prev</a>
    <a href="/calendar?month=2026-11${day}" class="btn-secondary">Next</a>
    <a href="/calendar" class="btn-secondary btn-compact" data-calendar-today>Today</a>
  </div>
  <div data-calendar-view data-selected-date="${selectedDate}">
    <section id="calendar-grid-panel">
      <button type="button" data-day="2026-10-04">4</button>
      <button type="button" data-day="2026-10-12">12</button>
    </section>
    <aside id="day-editor"></aside>
  </div></body></html>`;
}

function hrefs(document) {
  return [...document.querySelectorAll("[data-calendar-month-nav] a")].map((link) => link.getAttribute("href"));
}

for (const selected of ["", "2026-10-04"]) {
  test(`a day opened from the grid is carried by Prev and Next, not by Today (loaded with day=${selected || "none"})`, async () => {
    const dom = await loadDOMWithScript(APP_BUNDLE, {
      html: calendarPage(selected),
      url: `https://ovumcy.test/calendar?month=2026-10${selected ? `&day=${selected}` : ""}`,
    });
    try {
      const { document } = dom.window;
      document.querySelector('button[data-day="2026-10-12"]').click();
      assert.deepEqual(hrefs(document), [
        "/calendar?month=2026-09&day=2026-10-12",
        "/calendar?month=2026-11&day=2026-10-12",
        "/calendar",
      ]);
    } finally {
      dom.window.close();
    }
  });
}
