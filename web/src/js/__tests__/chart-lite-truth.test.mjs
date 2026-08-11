// chart-lite draws the two stats charts. These tests pin the contract that
// makes them honest about the data behind them:
//
//   * a JSON `null` in the BBT series is an unlogged day — a gap, never a
//     reading. `Number(null)` is 0 and passes a finite check, which used to
//     draw unlogged days as real 0 °C points and floor the axis at zero.
//   * the BBT axis is scaled to the readings (the whole subject of the chart
//     is a ~0.3 °C biphasic shift, invisible on a 0–37 °C axis), with a floor
//     window so a flat cycle is not magnified into noise instead.
//   * the cycle-length series is a dot plot, not bars: bar length encodes
//     magnitude and therefore demands a zero-based axis, on which 28 d and
//     29 d are indistinguishable. Position encoding makes the non-zero
//     domain that separates them legitimate.
//
// jsdom has no canvas, so the tests install a recording 2D context and read
// the drawing calls back: grid lines give the plot box, filled arcs give the
// markers, stroked paths give the value line, and right-aligned text left of
// the plot gives the axis ticks.

import test from "node:test";
import assert from "node:assert/strict";
import { readChartLite, loadDOMWithScript } from "./_helpers.mjs";

const CHART_LITE = readChartLite();

// The recorder's measureText is deterministic: every glyph is this wide.
const GLYPH_WIDTH = 6;

function createRecordingContext() {
  const calls = [];
  const stack = [];
  const state = {
    strokeStyle: "",
    fillStyle: "",
    lineWidth: 1,
    lineCap: "butt",
    lineJoin: "miter",
    font: "",
    textAlign: "start",
    textBaseline: "alphabetic",
    dash: [],
  };

  const context = {};
  for (const property of Object.keys(state)) {
    if (property === "dash") {
      continue;
    }
    Object.defineProperty(context, property, {
      get: () => state[property],
      set: (value) => {
        state[property] = value;
      },
    });
  }

  function record(op, args) {
    calls.push({
      op,
      args,
      strokeStyle: state.strokeStyle,
      fillStyle: state.fillStyle,
      lineWidth: state.lineWidth,
      lineCap: state.lineCap,
      lineJoin: state.lineJoin,
      textAlign: state.textAlign,
      dash: state.dash.slice(),
    });
  }

  const plainOps = [
    "scale",
    "clearRect",
    "beginPath",
    "closePath",
    "moveTo",
    "lineTo",
    "stroke",
    "fill",
    "arc",
    "fillRect",
    "strokeRect",
    "fillText",
  ];
  for (const op of plainOps) {
    context[op] = (...args) => record(op, args);
  }

  context.setLineDash = (dash) => {
    // Array.from re-creates the pattern in this realm: an array built inside
    // the jsdom window is not deepEqual to a plain one here.
    state.dash = Array.isArray(dash) ? Array.from(dash) : [];
    record("setLineDash", [state.dash.slice()]);
  };
  context.save = () => {
    stack.push({ ...state, dash: state.dash.slice() });
    record("save", []);
  };
  context.restore = () => {
    const previous = stack.pop();
    if (previous) {
      Object.assign(state, previous);
    }
    record("restore", []);
  };
  context.measureText = (text) => ({ width: String(text).length * GLYPH_WIDTH });

  context.calls = calls;
  return context;
}

async function drawChart(payload, attributes = {}) {
  const declared = Object.entries({ "data-chart": JSON.stringify(payload), ...attributes })
    .map(([name, value]) => `${name}='${value}'`)
    .join(" ");
  const html = `<!doctype html><html><head></head><body><div id="chart" class="chart-shell" ${declared}></div></body></html>`;

  let context = null;
  const dom = await loadDOMWithScript(CHART_LITE, {
    html,
    beforeRun(win) {
      context = createRecordingContext();
      win.HTMLCanvasElement.prototype.getContext = () => context;
    },
  });
  dom.window.close();

  assert.ok(context, "the recording context was installed");
  assert.ok(context.calls.length > 0, "the chart drew something onto the canvas");

  // jsdom fires its own DOMContentLoaded besides the one the helper dispatches,
  // so the chart may be drawn more than once into the same recorder. Every
  // render opens with clearRect: keep the calls of the last one.
  const start = context.calls.map((call) => call.op).lastIndexOf("clearRect");
  return context.calls.slice(Math.max(0, start));
}

// paths groups the recorded calls into beginPath..stroke/fill units.
function paths(calls) {
  const groups = [];
  let current = null;
  for (const call of calls) {
    if (call.op === "beginPath") {
      current = { ops: [], terminators: [] };
      groups.push(current);
      continue;
    }
    if (!current) {
      continue;
    }
    if (call.op === "stroke" || call.op === "fill") {
      current.terminators.push(call);
      continue;
    }
    if (call.op === "moveTo" || call.op === "lineTo" || call.op === "arc") {
      current.ops.push(call);
    }
  }
  return groups;
}

function gridBox(calls) {
  const grid = paths(calls)[0];
  assert.ok(grid, "the grid is the first path drawn");
  assert.ok(
    grid.ops.every((op) => op.op === "moveTo" || op.op === "lineTo"),
    "the grid is made of straight lines",
  );
  const stroke = grid.terminators.find((call) => call.op === "stroke");
  assert.ok(stroke, "the grid is stroked");
  assert.deepEqual(stroke.dash, [], "grid lines are solid, never dashed");
  assert.equal(stroke.lineWidth, 1, "grid lines are hairlines");

  const rows = [];
  for (let index = 0; index + 1 < grid.ops.length; index += 2) {
    const from = grid.ops[index];
    const to = grid.ops[index + 1];
    assert.equal(from.args[1], to.args[1], "every grid line is horizontal");
    rows.push({ y: from.args[1], left: from.args[0], right: to.args[0] });
  }
  assert.ok(rows.length >= 2, "the grid has at least two lines");

  return {
    left: rows[0].left,
    right: rows[0].right,
    top: Math.min(...rows.map((row) => row.y)),
    bottom: Math.max(...rows.map((row) => row.y)),
    rows,
  };
}

// markers are the filled discs drawn for each value.
function markers(calls) {
  return paths(calls)
    .filter(
      (group) =>
        group.ops.length === 1 &&
        group.ops[0].op === "arc" &&
        group.terminators.some((call) => call.op === "fill"),
    )
    .map((group) => ({
      x: group.ops[0].args[0],
      y: group.ops[0].args[1],
      radius: group.ops[0].args[2],
      fill: group.terminators[0].fillStyle,
    }));
}

// valueLine is the connected series path: a stroked, undashed polyline that is
// not the grid (the grid is the first path drawn).
function valueLine(calls) {
  const groups = paths(calls).slice(1);
  const line = groups.find(
    (group) =>
      group.ops.length > 0 &&
      group.ops.every((op) => op.op === "moveTo" || op.op === "lineTo") &&
      group.terminators.some((call) => call.op === "stroke" && call.dash.length === 0),
  );
  assert.ok(line, "the series is drawn as a connected path");
  return {
    ops: line.ops,
    stroke: line.terminators.find((call) => call.op === "stroke"),
    segments: line.ops.filter((op) => op.op === "moveTo").length,
    points: line.ops.map((op) => ({ x: op.args[0], y: op.args[1] })),
  };
}

// axisTicks are the value labels drawn to the left of the plot area.
function axisTicks(calls, box) {
  return calls
    .filter((call) => call.op === "fillText" && call.textAlign === "right" && call.args[1] < box.left)
    .map((call) => ({ text: call.args[0], value: Number.parseFloat(call.args[0]), y: call.args[2] }));
}

function labelInsidePlot(calls, box) {
  const label = calls.find(
    (call) => call.op === "fillText" && call.textAlign === "right" && call.args[1] > box.left,
  );
  assert.ok(label, "the reference line carries a label");
  const width = String(label.args[0]).length * GLYPH_WIDTH;
  return { text: label.args[0], right: label.args[1], left: label.args[1] - width, y: label.args[2] };
}

const BBT_ATTRIBUTES = {
  "data-value-suffix": "°C",
  "data-value-decimals": "1",
  "data-baseline-label": "Coverline",
};

test("an unlogged day is a gap: a null draws no marker and breaks the line", async () => {
  // Five unlogged days at the head of the cycle, then a gap in the middle:
  // exactly the shape buildCurrentCycleBBTSeries emits for a cycle whose
  // logging started on day 6.
  const values = [null, null, null, null, null, 36.5, 36.55, null, 36.8, 36.85];
  const calls = await drawChart(
    { kind: "line", labels: values.map((_, index) => String(index + 1)), values },
    BBT_ATTRIBUTES,
  );

  const drawn = markers(calls);
  assert.equal(drawn.length, 4, "only the four logged readings get a marker");

  const series = valueLine(calls);
  assert.equal(series.points.length, 4, "the path visits only the logged readings");
  assert.equal(series.segments, 2, "the null splits the path into two segments, not one line through a fabricated zero");

  const box = gridBox(calls);
  const ticks = axisTicks(calls, box);
  assert.ok(ticks.length > 0, "the axis is labelled");
  for (const tick of ticks) {
    assert.ok(tick.value > 30, `the axis is scaled to the readings, not to a fabricated zero (saw ${tick.text})`);
  }
});

test("the BBT axis scales to the readings, with a tick every 0.2 °C", async () => {
  // Readings 36.4..36.9 -> domain [36.25, 37.05] (0.15 of padding on each
  // side, already wider than the 0.8 floor window) -> ticks at every multiple
  // of 0.2 inside it.
  const values = [36.4, 36.5, 36.45, 36.7, 36.9];
  const calls = await drawChart(
    { kind: "line", labels: values.map((_, index) => String(index + 1)), values },
    BBT_ATTRIBUTES,
  );

  const box = gridBox(calls);
  const ticks = axisTicks(calls, box);
  assert.deepEqual(
    ticks.map((tick) => tick.text),
    ["37.0°C", "36.8°C", "36.6°C", "36.4°C"],
    "the axis carries a tick every 0.2 °C across the padded data range",
  );

  // Every tick label sits on its own grid line, and a reading that equals a
  // tick is drawn at that tick's height: the value mapping and the axis
  // labels describe the same scale.
  const gridYs = box.rows.map((row) => row.y).sort((a, b) => a - b);
  const tickYs = ticks.map((tick) => tick.y).sort((a, b) => a - b);
  assert.equal(gridYs.length, tickYs.length, "one grid line per tick");
  for (let index = 0; index < gridYs.length; index += 1) {
    assert.ok(Math.abs(gridYs[index] - tickYs[index]) < 3, "tick labels sit on their grid lines");
  }

  const tick364 = ticks.find((tick) => tick.text === "36.4°C");
  const marker364 = markers(calls).find((marker) => Math.abs(marker.y - tick364.y) < 0.5);
  assert.ok(marker364, "the 36.4 °C reading is drawn at the 36.4 °C tick");
});

test("a flat BBT cycle keeps the 0.8 °C floor window instead of magnifying noise", async () => {
  // A 0.05 °C spread would pad to a 0.35 °C domain; the floor window widens
  // it around its centre (36.525) to [36.125, 36.925].
  const values = [36.5, 36.55, 36.5, 36.55];
  const calls = await drawChart(
    { kind: "line", labels: values.map((_, index) => String(index + 1)), values },
    BBT_ATTRIBUTES,
  );

  const box = gridBox(calls);
  const ticks = axisTicks(calls, box);
  assert.deepEqual(
    ticks.map((tick) => tick.text),
    ["36.8°C", "36.6°C", "36.4°C", "36.2°C"],
    "the axis spans the 0.8 °C floor window, so a flat cycle stays flat",
  );
});

test("the cycle-length series is a dot plot whose axis separates 28 d from 29 d", async () => {
  // Cycle lengths 28..30 -> domain [27, 31] (a day of padding each side,
  // already at the 4-day floor window) with a tick per day.
  const calls = await drawChart(
    {
      kind: "bar",
      labels: ["Jan", "Feb", "Mar", "Apr"],
      values: [28, 29, 28, 30],
      baseline: 29,
    },
    { "data-days-suffix": "d", "data-baseline-label": "Average" },
  );

  assert.ok(
    !calls.some((call) => call.op === "fillRect" || call.op === "strokeRect"),
    "cycle lengths are not drawn as bars: bar length encodes magnitude and would demand a zero-based axis",
  );

  const box = gridBox(calls);
  const ticks = axisTicks(calls, box);
  assert.deepEqual(
    ticks.map((tick) => tick.text),
    ["31d", "30d", "29d", "28d", "27d"],
    "the axis is a non-zero day domain around the data",
  );

  const drawn = markers(calls);
  assert.equal(drawn.length, 4, "every cycle gets a marker");
  for (const marker of drawn) {
    assert.ok(marker.radius >= 4, "markers are at least 8 px across");
  }

  const series = valueLine(calls);
  assert.equal(series.points.length, 4, "a hairline connects the markers");

  const [first, second] = drawn;
  assert.ok(
    Math.abs(first.y - second.y) > 24,
    `28 d and 29 d are visibly apart (saw ${Math.abs(first.y - second.y)} px)`,
  );
});

test("the average reference label stays inside the plot area", async () => {
  const calls = await drawChart(
    {
      kind: "bar",
      labels: ["Jan", "Feb", "Mar", "Apr"],
      values: [28, 29, 28, 30],
      baseline: 29,
    },
    { "data-days-suffix": "d", "data-baseline-label": "Average" },
  );

  const box = gridBox(calls);
  const label = labelInsidePlot(calls, box);
  assert.ok(
    label.right <= box.right - 12,
    `the label keeps padding from the right edge of the plot (label right ${label.right}, plot right ${box.right})`,
  );
  assert.ok(label.left >= box.left, "the label does not spill past the left edge of the plot");
  assert.ok(label.y > box.top && label.y < box.bottom, "the label sits inside the plot area");
});
