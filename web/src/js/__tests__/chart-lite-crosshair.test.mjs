// The BBT chart's hover readout. The drawing itself is pinned by
// chart-lite-truth.test.mjs; what matters here is the mapping from a pointer
// position to the day it names, and that the readout can always be dismissed.
//
// The one contract worth spelling out: the crosshair resolves the nearest
// index, not the nearest *reading*. Snapping to the nearest logged value would
// print a temperature under a day that has none — the series' null handling
// exists precisely so an unlogged day is never given a neighbour's number, and
// a readout that quietly did it would undo that at the pointer.
//
// jsdom has no canvas and no layout: getContext is stubbed, every
// getBoundingClientRect is a zero rect (so the chart's coordinate space and the
// pointer's CSS pixels coincide, scale 1), and getContainerSize falls back to
// its 640 x 280 default. That fixes the plot box at left = 46, width = 572.

import test from "node:test";
import assert from "node:assert/strict";
import { readChartLite, loadDOMWithScript } from "./_helpers.mjs";

const CHART_LITE = readChartLite();

const PLOT_LEFT = 46;
const PLOT_WIDTH = 572;

// A ten-day cycle with the reading on day 4 missing. valueTexts is what the
// server rendered: the readout prints those strings rather than re-rounding
// the floats, so it always agrees with the table twin under the chart. Day 4
// carries an empty string — there is nothing to render.
const VALUES = [36.5, 36.55, 36.6, null, 36.62, 36.7, 36.72, 36.75, 36.8, 36.85];
const VALUE_TEXTS = ["36.5", "36.5", "36.6", "", "36.6", "36.7", "36.7", "36.8", "36.8", "36.9"];
const LABELS = VALUES.map((_, index) => String(index + 1));
const DATES = VALUES.map((_, index) => `Mar ${index + 1}`);

function expectedX(index) {
  return PLOT_LEFT + (index * PLOT_WIDTH) / (LABELS.length - 1);
}

function stubContext() {
  const noop = () => {};
  return {
    scale: noop,
    clearRect: noop,
    beginPath: noop,
    moveTo: noop,
    lineTo: noop,
    stroke: noop,
    fill: noop,
    arc: noop,
    fillText: noop,
    save: noop,
    restore: noop,
    setLineDash: noop,
    measureText: (text) => ({ width: String(text).length * 6 }),
  };
}

async function hoverChart() {
  const payload = JSON.stringify({
    kind: "line",
    labels: LABELS,
    values: VALUES,
    dates: DATES,
    valueTexts: VALUE_TEXTS,
  });
  const html = `<!doctype html><html><head></head><body><div
      id="chart"
      class="chart-shell"
      data-chart='${payload}'
      data-value-suffix="°C"
      data-value-decimals="1"
      data-chart-hover
      data-hover-day-label="Cycle day"
      data-hover-empty-text="No reading"></div></body></html>`;

  const dom = await loadDOMWithScript(CHART_LITE, {
    html,
    beforeRun(win) {
      win.HTMLCanvasElement.prototype.getContext = () => stubContext();
    },
  });

  const { window } = dom;
  const container = window.document.getElementById("chart");

  const point = (type, clientX) => {
    container.dispatchEvent(new window.MouseEvent(type, { bubbles: true, clientX }));
  };
  const text = (selector) => container.querySelector(selector)?.textContent ?? null;

  return {
    dom,
    window,
    container,
    point,
    text,
    layer: () => container.querySelector(".chart-hover-layer"),
    index: () => container.getAttribute("data-chart-hover-index"),
    visible: () => container.querySelector(".chart-hover-layer")?.classList.contains("chart-hover-visible") ?? false,
  };
}

test("the crosshair names the day nearest the pointer, with its date and reading", async () => {
  const chart = await hoverChart();

  chart.point("pointermove", expectedX(5));
  assert.equal(chart.index(), "5", "a pointer on day 6's column resolves to index 5");
  assert.equal(chart.text("[data-chart-tooltip-day]"), "Cycle day 6");
  assert.equal(chart.text("[data-chart-tooltip-date]"), "Mar 6");
  // The server's string plus the unit, not a second rounding of 36.7.
  assert.equal(chart.text("[data-chart-tooltip-value]"), "36.7°C");
  assert.equal(chart.visible(), true, "the readout is shown");

  // Halfway between two days still resolves — there is no dead zone between
  // columns, which is what makes the whole shell one hit surface.
  chart.point("pointermove", (expectedX(6) + expectedX(7)) / 2 - 1);
  assert.equal(chart.index(), "6");

  // Past either end the nearest index is the end itself.
  chart.point("pointermove", -400);
  assert.equal(chart.index(), "0");
  chart.point("pointermove", 4000);
  assert.equal(chart.index(), String(LABELS.length - 1));

  chart.dom.window.close();
});

test("a day with no reading resolves to itself and says so", async () => {
  const chart = await hoverChart();

  // Day 4 is the gap. Its column is nearest, so it is what the crosshair
  // reports — not day 3 or day 5, whose temperatures are not day 4's.
  chart.point("pointermove", expectedX(3));
  assert.equal(chart.index(), "3");
  assert.equal(chart.text("[data-chart-tooltip-day]"), "Cycle day 4");
  assert.equal(chart.text("[data-chart-tooltip-value]"), "No reading");
  assert.notEqual(chart.text("[data-chart-tooltip-value]"), "36.6°C");

  chart.dom.window.close();
});

test("a tap opens the readout and it can be dismissed by pointer or Escape", async () => {
  const chart = await hoverChart();

  // Touch has no hover: pointerdown is what opens the readout there.
  chart.point("pointerdown", expectedX(2));
  assert.equal(chart.visible(), true, "a tap on the chart opens the readout");
  assert.equal(chart.index(), "2");

  const elsewhere = chart.window.document.body;
  elsewhere.dispatchEvent(new chart.window.MouseEvent("pointerdown", { bubbles: true, clientX: 0 }));
  assert.equal(chart.visible(), false, "a tap outside the chart dismisses it");
  assert.equal(chart.index(), null);

  chart.point("pointermove", expectedX(4));
  assert.equal(chart.visible(), true);
  chart.container.dispatchEvent(new chart.window.MouseEvent("pointerleave", { bubbles: false }));
  assert.equal(chart.visible(), false, "the pointer leaving the chart dismisses it");

  chart.point("pointermove", expectedX(4));
  assert.equal(chart.visible(), true);
  chart.window.document.dispatchEvent(
    new chart.window.KeyboardEvent("keydown", { key: "Escape", bubbles: true }),
  );
  assert.equal(chart.visible(), false, "Escape dismisses it");

  chart.dom.window.close();
});

test("a chart without the hover opt-in grows no readout, and a re-render replaces it", async () => {
  const chart = await hoverChart();

  // jsdom delivers DOMContentLoaded twice, so the chart has already been drawn
  // more than once by the time the test runs. drawChart clears the container
  // before every pass, so the readout is replaced rather than stacked.
  assert.equal(
    chart.container.querySelectorAll("[data-chart-tooltip]").length,
    1,
    "re-rendering replaces the readout instead of stacking a second one",
  );

  const plain = chart.window.document.createElement("div");
  plain.className = "chart-shell";
  plain.setAttribute("data-chart", JSON.stringify({ kind: "bar", labels: ["Jan"], values: [28] }));
  chart.window.document.body.appendChild(plain);
  chart.window.document.body.dispatchEvent(
    new chart.window.CustomEvent("htmx:afterSwap", { bubbles: true, detail: { target: plain } }),
  );

  assert.equal(
    plain.querySelector(".chart-hover-layer"),
    null,
    "a chart that declares no text equivalent opts out of the readout",
  );
  plain.dispatchEvent(new chart.window.MouseEvent("pointermove", { bubbles: true, clientX: 200 }));
  assert.equal(plain.getAttribute("data-chart-hover-index"), null);

  chart.dom.window.close();
});
