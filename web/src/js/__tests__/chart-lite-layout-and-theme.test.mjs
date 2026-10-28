// chart-lite lays its axis text out against measured widths and repaints when
// the system colour scheme flips:
//
//   * the last x label is always drawn, and the label before it is dropped when
//     the two would touch — an unmeasured "17" + "18" read as "1718";
//   * the y gutter grows to the widest tick, so "100.0°F" is never cut;
//   * a `prefers-color-scheme` change repaints the canvas (it keeps the ink of
//     the scheme it was drawn in otherwise);
//   * the "cannot draw" fallback comes from the template, not from English.
//
// The recording context measures every glyph at a fixed width, so overlap is
// plain arithmetic on the recorded fillText calls.

import test from "node:test";
import assert from "node:assert/strict";
import { readChartLite, loadDOMWithScript } from "./_helpers.mjs";

const CHART_LITE = readChartLite();
const GLYPH_WIDTH = 6;
const CANVAS_WIDTH = 320;

function createRecordingContext() {
  const calls = [];
  const state = { fillStyle: "", strokeStyle: "", lineWidth: 1, font: "", textAlign: "start", textBaseline: "alphabetic" };
  const context = {};
  for (const property of Object.keys(state)) {
    Object.defineProperty(context, property, {
      get: () => state[property],
      set: (value) => {
        state[property] = value;
      },
    });
  }
  for (const op of ["scale", "clearRect", "beginPath", "closePath", "moveTo", "lineTo", "stroke", "fill", "arc", "fillRect", "strokeRect", "save", "restore", "setLineDash"]) {
    context[op] = (...args) => calls.push({ op, args });
  }
  context.fillText = (...args) => calls.push({ op: "fillText", args, textAlign: state.textAlign });
  context.measureText = (text) => ({ width: String(text).length * GLYPH_WIDTH });
  context.calls = calls;
  return context;
}

async function render(payload, { attributes = {}, noCanvas = false, colorScheme = false } = {}) {
  const declared = Object.entries({ "data-chart": JSON.stringify(payload), ...attributes })
    .map(([name, value]) => `${name}='${value}'`)
    .join(" ");
  const html = `<!doctype html><html><head></head><body><div id="chart" class="chart-shell" ${declared}></div></body></html>`;
  const context = createRecordingContext();
  const listeners = [];
  const dom = await loadDOMWithScript(CHART_LITE, {
    html,
    beforeRun(win) {
      win.HTMLCanvasElement.prototype.getContext = () => (noCanvas ? null : context);
      Object.defineProperty(win.HTMLElement.prototype, "clientWidth", { get: () => CANVAS_WIDTH });
      if (colorScheme) {
        win.matchMedia = () => ({
          matches: false,
          addEventListener: (type, listener) => listeners.push({ type, listener }),
          removeEventListener() {},
        });
      }
    },
  });
  return { dom, context, listeners };
}

function lastRender(calls) {
  const start = calls.map((call) => call.op).lastIndexOf("clearRect");
  return calls.slice(Math.max(0, start));
}

function textSpans(calls, align) {
  return calls
    .filter((call) => call.op === "fillText" && call.textAlign === align)
    .map((call) => {
      const text = String(call.args[0]);
      const width = text.length * GLYPH_WIDTH;
      const x = call.args[1];
      const left = align === "center" ? x - width / 2 : align === "right" ? x - width : x;
      return { text, left, right: left + width };
    });
}

function labels(count) {
  return Array.from({ length: count }, (_, index) => String(index + 1));
}

test("no two x labels touch, and the final label is still drawn", async () => {
  for (const count of [12, 14, 16, 18, 20]) {
    const values = Array.from({ length: count }, (_, i) => 36.4 + (i % 3) * 0.1);
    const { dom, context } = await render({ kind: "line", labels: labels(count), values }, { attributes: { "data-value-suffix": "°C" } });
    dom.window.close();
    const spans = textSpans(lastRender(context.calls), "center").sort((a, b) => a.left - b.left);
    assert.equal(spans[spans.length - 1].text, String(count), `n=${count}: the last day is labelled`);
    for (let i = 1; i < spans.length; i++) {
      assert.ok(spans[i].left - spans[i - 1].right >= 2, `n=${count}: "${spans[i - 1].text}" and "${spans[i].text}" do not touch`);
    }
  }
});

test("a wide label mid-axis pushes the step up instead of colliding with its neighbours", async () => {
  // Twelve days stepped by two leave 45 px between labels; the long date at
  // index 6 needs 52 px to clear "5" and "7", which the old last-pair-only
  // check never measured.
  const wide = labels(12);
  wide[6] = "Wed 30 Oct 2026";
  const values = Array.from({ length: 12 }, (_, i) => 36.4 + (i % 3) * 0.1);
  const { dom, context } = await render({ kind: "line", labels: wide, values }, { attributes: { "data-value-suffix": "°C" } });
  dom.window.close();
  const spans = textSpans(lastRender(context.calls), "center").sort((a, b) => a.left - b.left);
  assert.equal(spans[spans.length - 1].text, "12", "the last day is labelled");
  for (let i = 1; i < spans.length; i++) {
    assert.ok(
      spans[i].left - spans[i - 1].right >= 4,
      `"${spans[i - 1].text}" and "${spans[i].text}" keep the label gap`,
    );
  }
});

test("the y gutter is wide enough for the widest tick", async () => {
  const { dom, context } = await render(
    { kind: "line", labels: labels(5), values: [99.8, 100.1, 100.4, 100.2, 100.6] },
    { attributes: { "data-value-suffix": "°F", "data-value-unit": "fahrenheit", "data-value-decimals": "1" } },
  );
  dom.window.close();
  const spans = textSpans(lastRender(context.calls), "right");
  assert.ok(spans.some((span) => span.text.startsWith("100")), "a three-digit tick is drawn");
  for (const span of spans) {
    assert.ok(span.left >= 0, `"${span.text}" starts inside the canvas (left=${span.left})`);
  }
});

test("a colour-scheme change repaints the chart", async () => {
  const { dom, context, listeners } = await render(
    { kind: "line", labels: labels(5), values: [36.4, 36.5, 36.6, 36.5, 36.4] },
    { attributes: { "data-value-suffix": "°C" }, colorScheme: true },
  );
  assert.ok(listeners.some((entry) => entry.type === "change"), "the chart listens for prefers-color-scheme changes");
  const before = context.calls.filter((call) => call.op === "clearRect").length;
  for (const entry of listeners) {
    entry.listener({ matches: true });
  }
  await new Promise((resolve) => setTimeout(resolve, 250));
  dom.window.close();
  assert.ok(context.calls.filter((call) => call.op === "clearRect").length > before, "the canvas was redrawn");
});

test("an in-app theme change repaints the chart", async () => {
  const { dom, context } = await render(
    { kind: "line", labels: labels(5), values: [36.4, 36.5, 36.6, 36.5, 36.4] },
    { attributes: { "data-value-suffix": "°C" } },
  );
  const before = context.calls.filter((call) => call.op === "clearRect").length;
  dom.window.document.documentElement.setAttribute("data-theme", "dark");
  await new Promise((resolve) => setTimeout(resolve, 400));
  dom.window.close();
  assert.ok(context.calls.filter((call) => call.op === "clearRect").length > before, "the canvas was redrawn");
});

test("rewriting data-theme with the same value does not repaint", async () => {
  const { dom, context } = await render(
    { kind: "line", labels: labels(5), values: [36.4, 36.5, 36.6, 36.5, 36.4] },
    { attributes: { "data-value-suffix": "°C" } },
  );
  const root = dom.window.document.documentElement;
  root.setAttribute("data-theme", "light");
  await new Promise((resolve) => setTimeout(resolve, 400));
  const settled = context.calls.filter((call) => call.op === "clearRect").length;
  root.setAttribute("data-theme", "light");
  await new Promise((resolve) => setTimeout(resolve, 400));
  dom.window.close();
  assert.equal(context.calls.filter((call) => call.op === "clearRect").length, settled);
});

test("the cannot-draw fallback is the template's text", async () => {
  const { dom } = await render(
    { kind: "line", labels: labels(5), values: [36.4, 36.5, 36.6, 36.5, 36.4] },
    { attributes: { "data-error-text": "Diagramm nicht darstellbar." }, noCanvas: true },
  );
  const text = dom.window.document.getElementById("chart").textContent;
  dom.window.close();
  assert.equal(text, "Diagramm nicht darstellbar.");
});
