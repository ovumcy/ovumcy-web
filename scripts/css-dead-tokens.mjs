// Dead-token report for the component layer: an `@utility` no source names, and
// a custom property nothing reads.
//
// WHY THIS EXISTS AS ITS OWN INSTRUMENT. The stale-bundle CI step compares the
// committed bundle against a fresh build of the same sources, so both agree on a
// rule that was deleted and rebuilt away, and both agree on one that was never
// emitted at all. `status-transient` sat in input.css for months declaring
// `animation: none` while `grep -c status-transient web/static/css/tailwind.css`
// returned 0 — nothing in the pipeline could report it. Custom properties are
// worse: they are NOT class names, so Tailwind emits every declared one into the
// bundle whether or not a single rule reads it. `--bg-soft` shipped in both
// theme blocks with no reader anywhere.
//
// It runs under `npm run test:unit` through scripts/css-dead-tokens.test.mjs —
// which lives here rather than in web/src/js/__tests__ because that directory is
// inside `@source "../js"`, so its fixtures are Tailwind content — and by hand as
// `node scripts/css-dead-tokens.mjs`.

import { readFileSync, readdirSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const INPUT_CSS = "web/src/css/input.css";

// A CSS identifier continues through letters, digits, `_` and `-`, so plain
// substring matching cannot tell `--bg-soft` from `--bg-soft-strong` — the two
// are declared on consecutive lines in both theme blocks, and only the second is
// read. Every lookup in this file goes through these boundaries.
function tokenPattern(name) {
  const escaped = name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return new RegExp(`(?<![A-Za-z0-9_-])${escaped}(?![A-Za-z0-9_-])`);
}

// Prose is not a consumer. A comment in input.css that names a token it no
// longer uses would otherwise keep that token alive forever.
function stripCssComments(css) {
  return css.replace(/\/\*[\s\S]*?\*\//g, " ");
}

function declarationPattern() {
  // A custom-property DECLARATION: line start or a preceding `{`/`;`, the name,
  // then `:`. Anything else that spells the name — `var(--x)`, `rgb(var(--x) / 0.2)`,
  // a nested fallback — is a read.
  return /(^|[{;])(\s*)(--[A-Za-z0-9_-]+)\s*:/gm;
}

/**
 * Pure core: given the component stylesheet and the texts of the files its
 * `@source` list covers, report the tokens with no consumer.
 *
 * @param {{css: string, sources: Array<{path: string, text: string}>}} input
 * @returns {{deadUtilities: string[], deadProperties: string[]}}
 */
export function findDeadCssTokens({ css, sources }) {
  const stylesheet = stripCssComments(css);
  const sourceText = sources.map((source) => source.text).join("\n");

  // A utility's consumers live in the SOURCES — markup, Go class literals, JS —
  // plus `@apply` inside the stylesheet, which genuinely composes it into
  // another rule. A bare selector reference such as `&:is(.card, .card-quiet)`
  // deliberately does NOT count: a rule inside a live component that names a
  // dead class survives the class's death and can never match, which is the
  // exact residue this report hunts. Counting it would make the check blind to
  // its own subject.
  const applyText = (stylesheet.match(/@apply[^;}]*/g) || []).join("\n");
  const utilities = [...stylesheet.matchAll(/^@utility\s+([A-Za-z0-9_-]+)/gm)].map((match) => match[1]);
  const deadUtilities = utilities.filter((name) => {
    const pattern = tokenPattern(name);
    return !pattern.test(sourceText) && !pattern.test(applyText);
  });

  // For properties, every declaration is erased from the stylesheet first; what
  // survives is the read side. `--bg-soft` is declared twice (light and dark),
  // so erasing one occurrence would leave the other looking like a read.
  const declared = [];
  const seen = new Set();
  for (const match of stylesheet.matchAll(declarationPattern())) {
    if (seen.has(match[3])) continue;
    seen.add(match[3]);
    declared.push(match[3]);
  }
  const readSide = stylesheet.replace(declarationPattern(), "$1$2");

  // THE CLAUSE THAT KEEPS `--chart-grid` OUT OF THIS REPORT. chart-lite.js reads
  // it as `cssVar("--chart-grid", ...)` — a getComputedStyle().getPropertyValue()
  // behind a helper — and `--chart-hover-x` is written back through
  // style.setProperty(). No `var()` scan of the stylesheet can see either one.
  // So a bare occurrence of the name anywhere in a source file counts as a read.
  // Raw text rather than string literals on purpose: this report's only dangerous
  // direction is a false positive, because acting on one deletes a live token.
  const deadProperties = declared.filter((name) => {
    const pattern = tokenPattern(name);
    return !pattern.test(readSide) && !pattern.test(sourceText);
  });

  return { deadUtilities, deadProperties };
}

// A class named only by a test is not a consumer: the utility is dead in
// production and the test is pinning its own fixture. Tailwind itself does scan
// these files, so a test-only class still reaches the bundle — which is the
// reason to judge them separately here rather than trust the build.
export function isTestSource(filePath) {
  const normalized = filePath.split(path.sep).join("/");
  return /(^|\/)__tests__\//.test(normalized) || /_test\.go$/.test(normalized) || /\.test\.[cm]?js$/.test(normalized);
}

function walk(target, into) {
  if (statSync(target).isDirectory()) {
    for (const entry of readdirSync(target).sort()) walk(path.join(target, entry), into);
    return;
  }
  into.push(target);
}

/**
 * Read the stylesheet and the files its own `@source` directives cover. The
 * `@source` list is the build's hermetic content set, so reading it from the
 * stylesheet keeps this report and the bundle looking at the same files: adding
 * a source directive extends both at once.
 *
 * @param {string} repoRoot
 */
export function readCssGraph(repoRoot) {
  const cssPath = path.join(repoRoot, INPUT_CSS);
  const css = readFileSync(cssPath, "utf8");
  const cssDir = path.dirname(cssPath);

  const targets = [...stripCssComments(css).matchAll(/@source\s+"([^"]+)"/g)].map((match) =>
    path.resolve(cssDir, match[1])
  );
  if (targets.length === 0) {
    throw new Error(`${INPUT_CSS} declares no @source; the report would call every token dead`);
  }

  const files = [];
  for (const target of targets) walk(target, files);

  const sources = files
    .filter((file) => !isTestSource(file))
    .map((file) => ({ path: path.relative(repoRoot, file).split(path.sep).join("/"), text: readFileSync(file, "utf8") }));

  return { css, sources };
}

export function formatReport({ deadUtilities, deadProperties }) {
  const lines = [];
  for (const name of deadUtilities) {
    lines.push(`@utility ${name} — declared in ${INPUT_CSS}, named by no @source file and no @apply`);
  }
  for (const name of deadProperties) {
    lines.push(`${name} — declared in ${INPUT_CSS}, read by no var(), no @source file, no JS string`);
  }
  return lines.join("\n");
}

const invokedDirectly = process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url);
if (invokedDirectly) {
  const repoRoot = path.resolve(import.meta.dirname, "..");
  const report = findDeadCssTokens(readCssGraph(repoRoot));
  const text = formatReport(report);
  if (text) {
    console.error(`dead CSS tokens (${report.deadUtilities.length} utilities, ${report.deadProperties.length} properties):`);
    console.error(text);
    process.exit(1);
  }
  console.log("no dead CSS tokens");
}
