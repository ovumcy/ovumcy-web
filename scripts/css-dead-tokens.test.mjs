// The component layer had no dead-token report at all, and neither half of the
// build could grow one. Tailwind emits a class rule only for a name it finds in
// the @source set, so a `@utility` nothing renders simply never reaches the
// bundle and leaves no trace to grep; the stale-bundle CI step compares the
// committed bundle against a fresh build of the SAME sources, so both sides
// agree and stay green. Custom properties fail the other way: they are not class
// names, so every declared one ships whether or not a rule reads it.
//
// THIS FILE LIVES IN scripts/ BECAUSE OF ITS OWN FIXTURES. `@source "../js"`
// covers web/src/js **including __tests__**, so a fixture there is a Tailwind
// content source: with an earlier draft of this file under
// web/src/js/__tests__/, the fixture string "status-transient" minted
// `.status-transient{animation:none}` — 33 bytes, measured — straight back into
// the bundle the same change was deleting it from. `scripts/` is in no @source
// directive, which is also the honest home: this is a repository-level check,
// not a browser module. `npm run test:unit` globs it explicitly.
//
// The first assertion is the report over the real tree. The fixtures below exist
// because that green is only worth something if the report can tell a dead token
// from a live one — and one shape in particular. chart-lite.js reads
// --chart-grid and --chart-line through `cssVar(name, fallback)`, i.e.
// getComputedStyle().getPropertyValue(), so a var() scan of the stylesheet sees
// nothing and a naive checker calls both dead. A checker that reports a live
// token is worse than no checker: acting on its output deletes something the
// chart paints with.

import test from "node:test";
import assert from "node:assert/strict";
import path from "node:path";

import { findDeadCssTokens, isTestSource, readCssGraph, formatReport } from "./css-dead-tokens.mjs";

const repoRoot = path.resolve(import.meta.dirname, "..");

test("no @utility and no custom property in input.css is without a consumer", () => {
  const report = findDeadCssTokens(readCssGraph(repoRoot));
  assert.equal(
    formatReport(report),
    "",
    "web/src/css/input.css declares a token nothing reads; delete the declaration, " +
      "or add the consumer that justifies it"
  );
});

test("the real @source set is read, not an empty one", () => {
  // An empty source set would make every token look dead, so the assertion above
  // would be unfalsifiable in the other direction.
  const { sources } = readCssGraph(repoRoot);
  assert.ok(
    sources.length > 20,
    `expected the @source set to cover the templates and the client JS, got ${sources.length} files`
  );
  const named = sources.map((source) => source.path);
  assert.ok(named.includes("web/static/js/chart-lite.js"), "chart-lite.js must be in the scanned set");
  assert.ok(named.some((file) => file.startsWith("internal/templates/")), "the server templates must be in the scanned set");
  assert.ok(named.includes("internal/httpx/markup.go"), "the Go markup helpers must be in the scanned set");
});

const CHART_CSS = `
:root {
  --chart-grid: rgba(172, 136, 96, 0.26);
  --chart-line: #a8622c;
  --chart-abandoned: #123456;
}
`;

// The shape at web/static/js/chart-lite.js:73-74 and :719-720, verbatim.
const CHART_JS = `
  function cssVar(name, fallback) {
    var raw = getComputedStyle(document.documentElement).getPropertyValue(name);
    return raw ? raw.trim() : fallback;
  }
  var palette = {
    grid: cssVar("--chart-grid", "rgba(172, 136, 96, 0.26)"),
    line: cssVar("--chart-line", "#a8622c")
  };
`;

test("a property read only through cssVar()/getPropertyValue is live, and its neighbour is not", () => {
  const report = findDeadCssTokens({
    css: CHART_CSS,
    sources: [{ path: "web/static/js/chart-lite.js", text: CHART_JS }]
  });
  // Both halves matter. --chart-grid and --chart-line are invisible to a var()
  // scan and must not be reported; --chart-abandoned differs from them in
  // nothing but the missing JS read, so reporting it is what proves the check
  // looked at all three rather than skipping the block.
  assert.deepEqual(report.deadProperties, ["--chart-abandoned"]);
});

// The trap R2-0162 names: --bg-soft and --bg-soft-strong were declared on
// consecutive lines in both theme blocks and only the second was read.
// Substring matching calls the first live.
const NEIGHBOUR_CSS = `
:root {
  --bg-soft: #fff4e8;
  --bg-soft-strong: rgba(255, 248, 240, 0.82);
}
:root[data-theme="dark"] {
  --bg-soft: #2b2439;
  --bg-soft-strong: rgba(39, 31, 52, 0.86);
}
@utility card-quiet {
  background: var(--bg-soft-strong);
}
`;

test("a property whose name prefixes a live one is still reported", () => {
  const report = findDeadCssTokens({
    css: NEIGHBOUR_CSS,
    sources: [{ path: "x.html", text: `class="card-quiet"` }]
  });
  assert.deepEqual(report.deadProperties, ["--bg-soft"]);
  assert.deepEqual(report.deadUtilities, []);
});

test("a property declared in two theme blocks is not read by its own second declaration", () => {
  // Erasing one declaration and leaving the other would make every dual-theme
  // token look live — which is every colour token in the file.
  const report = findDeadCssTokens({
    css: NEIGHBOUR_CSS.replace("var(--bg-soft-strong)", "transparent"),
    sources: []
  });
  assert.deepEqual(report.deadProperties, ["--bg-soft", "--bg-soft-strong"]);
});

const UTILITY_CSS = `
@utility widget-ok {
  color: green;
}
@utility widget-transient {
  animation: none;
}
@utility widget-stack {
  @apply widget-ok;
}
@utility widget-item {
  &:is(.widget-transient) {
    opacity: 0;
  }
}
`;

test("an @utility is live from markup or @apply, not from a selector that names it", () => {
  const report = findDeadCssTokens({
    css: UTILITY_CSS,
    sources: [{ path: "internal/httpx/markup.go", text: `"<div class=\\"widget-stack\\"><span class=\\"widget-item\\"></span></div>"` }]
  });
  // widget-ok is live through @apply. widget-transient is named only by the
  // `&:is(.widget-transient)` rule inside a live component — the shape that
  // survives a class's death and can never match — so it is not a consumer.
  assert.deepEqual(report.deadUtilities, ["widget-transient"]);
});

test("a class named only by a test file is not a consumer", () => {
  const fromProduction = findDeadCssTokens({
    css: "@utility widget-transient { animation: none; }",
    sources: [{ path: "internal/httpx/markup.go", text: `"widget-transient"` }]
  });
  assert.deepEqual(fromProduction.deadUtilities, [], "a production Go literal is a consumer");

  assert.equal(isTestSource(path.join("internal", "httpx", "markup_test.go")), true);
  assert.equal(isTestSource(path.join("internal", "httpx", "markup.go")), false);
  assert.equal(isTestSource(path.join("web", "src", "js", "__tests__", "_helpers.mjs")), true);
  assert.equal(isTestSource(path.join("web", "src", "js", "app", "toast.js")), false);

  // readCssGraph drops what isTestSource marks, so the same literal in a
  // _test.go file never reaches the source text the first assertion searches.
  const { sources } = readCssGraph(repoRoot);
  assert.equal(sources.filter((source) => isTestSource(source.path)).length, 0);
});

test("a token named only in a comment is not a consumer", () => {
  const report = findDeadCssTokens({
    css: `/* --bg-soft and widget-transient are described here */\n:root { --bg-soft: #fff4e8; }\n@utility widget-transient { animation: none; }`,
    sources: []
  });
  assert.deepEqual(report.deadProperties, ["--bg-soft"]);
  assert.deepEqual(report.deadUtilities, ["widget-transient"]);
});
