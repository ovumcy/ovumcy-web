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
//
// THE ONE DANGEROUS DIRECTION is a false positive: acting on a report deletes a
// token, so a live token named in the report costs a defect while a dead token
// missing from it costs only bytes. Every judgement call below leans that way,
// and the two places where it cannot — comment stripping, and the reachability
// closure — are the two that carry the most test weight.
//
// STATED LIMITS, so a clean run is not read as more than it is:
//   - Reachability is closed to a fixed point over three edges (a source names a
//     class, an `@apply` composes one, a live declaration reads a property), so a
//     dead chain collapses in ONE run. It is NOT closed over specificity: a rule
//     inside a live utility that ties with another and never paints still counts
//     as a read. That is the coin-toss problem, and only a browser can see it.
//   - A class assembled at runtime from fragments (`"status-" + kind`) is
//     invisible here exactly as it is to Tailwind, which would not emit the rule
//     either. The build and this report share that blind spot by construction.

import { lstatSync, readFileSync, readdirSync, realpathSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const INPUT_CSS = "web/src/css/input.css";

// ---------------------------------------------------------------------------
// Tokenising
// ---------------------------------------------------------------------------

// A CSS identifier runs through letters, digits, `_` and `-`, so a substring
// search cannot tell `--bg-soft` from `--bg-soft-strong` — the two were declared
// on consecutive lines in both theme blocks and only the second was read. Cutting
// every corpus into maximal identifier runs gives those boundaries exactly, and
// gives them once: membership in a Set replaces one full-corpus regex pass per
// token, which was 430 passes over ~1 MB and grew as tokens × corpus.
const IDENTIFIER_RUN = /[A-Za-z0-9_-]+/g;

export function tokenize(text) {
  const tokens = new Set();
  for (const match of text.matchAll(IDENTIFIER_RUN)) tokens.add(match[0]);
  return tokens;
}

// Tailwind's functional form, `@utility tab-*`, is consumed as `tab-4`/`tab-size`
// — never as the bare stem. Matching the stem with identifier boundaries requires
// a non-identifier character right after the hyphen, which no real use has, so
// every functional utility would be reported dead. Prefix matching is the whole
// fix; it costs one scan of the token set per functional utility, and there are
// currently none.
function hasConsumer(tokens, name, functional) {
  if (!functional) return tokens.has(name);
  for (const token of tokens) {
    if (token.length > name.length && token.startsWith(name)) return true;
  }
  return false;
}

function union(target, source) {
  for (const value of source) target.add(value);
  return target;
}

// ---------------------------------------------------------------------------
// Comment stripping, per language
// ---------------------------------------------------------------------------

// Prose is not a consumer, on EITHER side. The stylesheet half of this was
// obvious; the source half is where the residue is actually made, because
// "removed in 1.4.0, see the old `status-transient` rule" is the normal way that
// edit gets written, and it would pin the token live forever.
//
// Stripping is only safe if it never eats real code, so each language gets a
// scanner that walks strings and comments rather than a regex that cannot tell
// them apart. The motivating case is in this repository:
// internal/templates/privacy.html:51 carries `href="https://…"` and
// `class="inline-link"` on ONE line, so a `//`-to-end-of-line rule applied to
// HTML would delete a live class. HTML has no `//` comment, and the Go and JS
// scanners below see the `//` inside that URL as string content.
//
// Anything the scanner cannot resolve — an unterminated comment or string —
// falls back to the raw text for that file, which counts every token in it as
// live. That is the safe direction, and `readCssGraph` reports the fallback
// rather than swallowing it.

const REGEX_MAY_FOLLOW = new Set([..."(,=:[!&|?{};+-*%~^<>"]);
const REGEX_MAY_FOLLOW_WORD = /(?:^|[^A-Za-z0-9_$])(return|typeof|instanceof|in|of|new|delete|void|case|do|else|yield|await)$/;

function regexMayStartHere(tail) {
  if (tail === "") return true;
  if (REGEX_MAY_FOLLOW.has(tail[tail.length - 1])) return true;
  return REGEX_MAY_FOLLOW_WORD.test(tail);
}

// A regex literal cannot span a line, so a scan that runs off the end means this
// `/` was division after all — bail to that reading rather than swallowing the
// rest of the file.
function scanRegexLiteral(text, start) {
  let index = start + 1;
  let inClass = false;
  while (index < text.length) {
    const character = text[index];
    if (character === "\n") return -1;
    if (character === "\\") {
      index += 2;
      continue;
    }
    if (character === "[") inClass = true;
    else if (character === "]") inClass = false;
    else if (character === "/" && !inClass) {
      index += 1;
      while (index < text.length && /[a-z]/.test(text[index])) index += 1;
      return index;
    }
    index += 1;
  }
  return -1;
}

function scanQuoted(text, start, quote, raw) {
  let index = start + 1;
  while (index < text.length) {
    const character = text[index];
    if (!raw && character === "\\") {
      index += 2;
      continue;
    }
    if (character === quote) return index + 1;
    // A newline inside a single- or double-quoted literal is invalid in both Go
    // and JS; treating it as unterminated is what triggers the safe fallback.
    if (character === "\n" && quote !== "`") return -1;
    index += 1;
  }
  return -1;
}

// The last few significant characters before `index`, read back out of the raw
// text only when a `/` actually needs disambiguating. Keeping this state per
// character instead cost one string allocation per byte of corpus, which is the
// opposite of what the tokenising above is for.
const WHITESPACE = /\s/;
const COMMENT_OR_LITERAL_START = /["'`/]/g;

function significantTailBefore(text, index) {
  let end = index;
  while (end > 0 && WHITESPACE.test(text[end - 1])) end -= 1;
  return text.slice(Math.max(0, end - 16), end);
}

function stripBraceLanguageComments(text, language) {
  // Only comments leave the text; strings and regex literals are kept verbatim,
  // so the scan never has to touch a character it is going to keep. Jumping
  // between the characters that can OPEN one of those makes this one pass over
  // the interesting positions rather than over every byte.
  const kept = [];
  let segmentStart = 0;
  let match;
  COMMENT_OR_LITERAL_START.lastIndex = 0;
  while ((match = COMMENT_OR_LITERAL_START.exec(text)) !== null) {
    const index = match.index;
    if (match[0] === "/") {
      const next = text[index + 1];
      if (next === "/") {
        const lineEnd = text.indexOf("\n", index);
        kept.push(text.slice(segmentStart, index), " ");
        if (lineEnd === -1) return kept.join("");
        segmentStart = lineEnd;
        COMMENT_OR_LITERAL_START.lastIndex = lineEnd;
        continue;
      }
      if (next === "*") {
        const close = text.indexOf("*/", index + 2);
        if (close === -1) return null;
        kept.push(text.slice(segmentStart, index), " ");
        segmentStart = close + 2;
        COMMENT_OR_LITERAL_START.lastIndex = close + 2;
        continue;
      }
      if (language === "js" && regexMayStartHere(significantTailBefore(text, index))) {
        const end = scanRegexLiteral(text, index);
        if (end !== -1) COMMENT_OR_LITERAL_START.lastIndex = end;
      }
      continue; // a lone slash is division, and division is kept
    }
    const raw = language === "go" && match[0] === "`";
    const end = scanQuoted(text, index, match[0], raw);
    if (end === -1) return null;
    COMMENT_OR_LITERAL_START.lastIndex = end;
  }
  kept.push(text.slice(segmentStart));
  return kept.join("");
}

function stripMarkupComments(text) {
  // Both comment forms a server template carries. Neither can be confused with
  // an attribute value, which is why markup needs no character scanner.
  if (text.includes("<!--") && !text.includes("-->")) return null;
  if (text.includes("{{/*") && !text.includes("*/}}")) return null;
  return text.replace(/<!--[\s\S]*?-->/g, " ").replace(/\{\{\/\*[\s\S]*?\*\/\}\}/g, " ");
}

export function languageOf(filePath) {
  const extension = path.extname(filePath).toLowerCase();
  if (extension === ".go") return "go";
  if (extension === ".js" || extension === ".mjs" || extension === ".cjs") return "js";
  if (extension === ".html" || extension === ".htm") return "html";
  return "unknown";
}

/**
 * @returns {{text: string, fellBack: boolean}} the comment-free text, or the raw
 * text with `fellBack` set when the scanner could not resolve the file.
 */
export function stripSourceComments(text, language) {
  if (language === "html") {
    const stripped = stripMarkupComments(text);
    return stripped === null ? { text, fellBack: true } : { text: stripped, fellBack: false };
  }
  if (language === "go" || language === "js") {
    const stripped = stripBraceLanguageComments(text, language);
    return stripped === null ? { text, fellBack: true } : { text: stripped, fellBack: false };
  }
  return { text, fellBack: true };
}

// The stylesheet has only one comment form and no string that can contain it.
export function stripCssComments(css) {
  return css.replace(/\/\*[\s\S]*?\*\//g, " ");
}

// ---------------------------------------------------------------------------
// Stylesheet structure
// ---------------------------------------------------------------------------

function scanCssBlock(css, openBrace) {
  let depth = 0;
  let index = openBrace;
  while (index < css.length) {
    const character = css[index];
    if (character === '"' || character === "'") {
      const end = scanQuoted(css, index, character, false);
      if (end === -1) return -1;
      index = end;
      continue;
    }
    if (character === "{") depth += 1;
    else if (character === "}") {
      depth -= 1;
      if (depth === 0) return index + 1;
    }
    index += 1;
  }
  return -1;
}

// Split the stylesheet into its top-level `@utility` bodies and the text between
// them. The attribution is what lets a read inside a DEAD utility stop counting:
// `--cal-ovulation-solid` was read as `rgb(var(--cal-ovulation-solid))` inside
// `@utility calendar-tag-ovulation`, and when 0282f024 deleted that utility the
// property was left behind for a later wave to find. Closing that edge is the
// difference between finding the residue now and finding it next time.
function splitUtilityBlocks(css) {
  const header = /^@utility\s+([A-Za-z0-9_-]+)(\*?)\s*\{/gm;
  const blocks = [];
  const globals = [];
  let cursor = 0;
  let match;
  while ((match = header.exec(css)) !== null) {
    if (match.index < cursor) {
      header.lastIndex = cursor;
      continue;
    }
    const openBrace = match.index + match[0].length - 1;
    const close = scanCssBlock(css, openBrace);
    if (close === -1) {
      throw new Error(`${INPUT_CSS}: @utility ${match[1]}${match[2]} has no closing brace; the stylesheet cannot be attributed`);
    }
    globals.push(css.slice(cursor, match.index));
    blocks.push({ name: match[1], functional: match[2] === "*", body: css.slice(openBrace + 1, close - 1) });
    cursor = close;
    header.lastIndex = close;
  }
  globals.push(css.slice(cursor));
  return { blocks, globalCss: globals.join("\n") };
}

function scanDeclarationValue(css, start) {
  let depth = 0;
  let index = start;
  while (index < css.length) {
    const character = css[index];
    if (character === '"' || character === "'") {
      const end = scanQuoted(css, index, character, false);
      if (end === -1) return index;
      index = end;
      continue;
    }
    if (character === "(") depth += 1;
    else if (character === ")") depth -= 1;
    else if (depth === 0 && (character === ";" || character === "}")) return index;
    index += 1;
  }
  return index;
}

// Lift every custom-property declaration out of a chunk, returning what is left
// (the read side: selectors, ordinary declarations, `@apply`) and each
// declaration's name with its value text held separately. Holding the value
// separately is what makes the chain edge expressible: `--card-quiet-surface:
// var(--bg-soft-strong)` is a read of `--bg-soft-strong` only while
// `--card-quiet-surface` itself is alive.
function liftCustomProperties(chunk) {
  const header = /(^|[{;])(\s*)(--[A-Za-z0-9_-]+)\s*:/gm;
  const declarations = [];
  const kept = [];
  let cursor = 0;
  let match;
  while ((match = header.exec(chunk)) !== null) {
    if (match.index < cursor) {
      header.lastIndex = cursor;
      continue;
    }
    const nameStart = match.index + match[1].length + match[2].length;
    const valueStart = match.index + match[0].length;
    const valueEnd = scanDeclarationValue(chunk, valueStart);
    kept.push(chunk.slice(cursor, nameStart), " ");
    declarations.push({ name: match[3], value: chunk.slice(valueStart, valueEnd) });
    cursor = valueEnd;
    header.lastIndex = valueEnd;
  }
  kept.push(chunk.slice(cursor));
  return { readSide: kept.join(""), declarations };
}

function applyTokensOf(chunk) {
  return tokenize((chunk.match(/@apply[^;}]*/g) || []).join("\n"));
}

// ---------------------------------------------------------------------------
// The report
// ---------------------------------------------------------------------------

/**
 * Pure core: given the component stylesheet and the texts of the files its
 * `@source` list covers, report the tokens with no consumer.
 *
 * @param {{css: string, sources: Array<{path: string, text: string}>, testSources?: Array<{path: string, text: string}>}} input
 */
export function findDeadCssTokens({ css, sources, testSources = [] }) {
  const stylesheet = stripCssComments(css);
  const { blocks, globalCss } = splitUtilityBlocks(stylesheet);

  const sourceTokens = new Set();
  for (const source of sources) {
    const { text } = stripSourceComments(source.text, languageOf(source.path));
    union(sourceTokens, tokenize(text));
  }

  const testTokensByFile = testSources.map((source) => ({
    path: source.path,
    tokens: tokenize(stripSourceComments(source.text, languageOf(source.path)).text)
  }));

  const globals = liftCustomProperties(globalCss);
  const blockParts = blocks.map((block) => ({ ...block, ...liftCustomProperties(block.body) }));

  // A utility's consumers are the SOURCES — markup, Go class literals, JS — plus
  // an `@apply` that composes it into another rule. A bare selector reference
  // such as `&:is(.card, .card-quiet)` deliberately does not count: a rule inside
  // a live component that names a dead class survives the class's death and can
  // never match, which is the exact residue this report hunts.
  //
  // Iterated, because an `@apply` inside a utility that is itself dead is not a
  // consumer either. Each round can only shrink the live set, so it terminates.
  const globalApplies = applyTokensOf(globals.readSide);
  let liveUtilities = new Set(blockParts.map((block) => block.name));
  for (;;) {
    const applies = new Set(globalApplies);
    for (const block of blockParts) {
      if (liveUtilities.has(block.name)) union(applies, applyTokensOf(block.readSide));
    }
    const next = new Set(
      blockParts
        .filter((block) => hasConsumer(sourceTokens, block.name, block.functional) || hasConsumer(applies, block.name, block.functional))
        .map((block) => block.name)
    );
    if (next.size === liveUtilities.size) break;
    liveUtilities = next;
  }

  const declaredNames = [];
  const seenNames = new Set();
  for (const declaration of [...globals.declarations, ...blockParts.flatMap((block) => block.declarations)]) {
    if (seenNames.has(declaration.name)) continue;
    seenNames.add(declaration.name);
    declaredNames.push(declaration.name);
  }

  // THE CLAUSE THAT KEEPS `--chart-grid` OUT OF THIS REPORT. chart-lite.js reads
  // it as `cssVar("--chart-grid", …)` — getComputedStyle().getPropertyValue()
  // behind a helper — and `--chart-hover-x` is written back through
  // style.setProperty(). No `var()` scan of the stylesheet can see either one, so
  // a bare occurrence of the name in a source file counts as a read.
  const staticReads = new Set(sourceTokens);
  union(staticReads, tokenize(globals.readSide));
  let liveProperties = new Set(declaredNames);
  for (;;) {
    const reads = new Set(staticReads);
    for (const block of blockParts) {
      if (!liveUtilities.has(block.name)) continue;
      union(reads, tokenize(block.readSide));
    }
    for (const [owner, declarations] of [
      [null, globals.declarations],
      ...blockParts.map((block) => [block.name, block.declarations])
    ]) {
      if (owner !== null && !liveUtilities.has(owner)) continue;
      for (const declaration of declarations) {
        if (!liveProperties.has(declaration.name)) continue;
        union(reads, tokenize(declaration.value));
      }
    }
    const next = new Set(declaredNames.filter((name) => reads.has(name)));
    if (next.size === liveProperties.size) break;
    liveProperties = next;
  }

  const namedOnlyByTest = (name, functional) =>
    testTokensByFile.filter((file) => hasConsumer(file.tokens, name, functional)).map((file) => file.path);

  const report = { deadUtilities: [], deadProperties: [], testOnlyUtilities: [], testOnlyProperties: [] };
  for (const block of blockParts) {
    if (liveUtilities.has(block.name)) continue;
    const name = block.name + (block.functional ? "*" : "");
    const tests = namedOnlyByTest(block.name, block.functional);
    if (tests.length > 0) report.testOnlyUtilities.push({ name, tests });
    else report.deadUtilities.push(name);
  }
  for (const name of declaredNames) {
    if (liveProperties.has(name)) continue;
    const tests = namedOnlyByTest(name, false);
    if (tests.length > 0) report.testOnlyProperties.push({ name, tests });
    else report.deadProperties.push(name);
  }
  return report;
}

// ---------------------------------------------------------------------------
// Reading the tree
// ---------------------------------------------------------------------------

// A class named only by a test is not a consumer: the utility is dead in
// production and the test is pinning its own fixture. Tailwind itself does scan
// these files, so a test-only class still reaches the bundle — which is the
// reason to judge them separately here rather than trust the build. They are
// reported under their own message, because "named by no source" is false for
// one of them and a reader who deletes on that wording reddens a test.
export function isTestSource(filePath) {
  const normalized = filePath.split(path.sep).join("/");
  return /(^|\/)__tests__\//.test(normalized) || /_test\.go$/.test(normalized) || /\.test\.[cm]?js$/.test(normalized);
}

function sourceDirectiveError(directive, resolved, cause) {
  const glob = /[*?[\]]/.test(directive);
  return new Error(
    `${INPUT_CSS}: @source "${directive}" resolves to ${resolved}, which cannot be read (${cause})` +
      (glob
        ? `. Tailwind accepts glob forms in @source; this report resolves the directive as a literal path, so name the directory the glob covers instead.`
        : `. Name a directory or file that exists, or drop the directive.`)
  );
}

/**
 * Collect the files under one `@source` target. Symlinks are resolved and
 * de-duplicated by real path: this repository creates git worktrees under its own
 * root, so a link pointing at an ancestor is a live possibility and an unguarded
 * walk recurses until the stack gives out.
 */
export function walkSourceTree(target, directive, into = [], visitedDirectories = new Set()) {
  let stats;
  try {
    stats = lstatSync(target);
  } catch (error) {
    throw sourceDirectiveError(directive, target, error.code || error.message);
  }
  if (stats.isSymbolicLink()) {
    let resolved;
    try {
      resolved = realpathSync(target);
    } catch {
      return into; // a broken link holds no class names
    }
    if (visitedDirectories.has(resolved)) return into;
    return walkSourceTree(resolved, directive, into, visitedDirectories);
  }
  if (stats.isDirectory()) {
    const resolved = realpathSync(target);
    if (visitedDirectories.has(resolved)) return into;
    visitedDirectories.add(resolved);
    for (const entry of readdirSync(resolved).sort()) {
      walkSourceTree(path.join(resolved, entry), directive, into, visitedDirectories);
    }
    return into;
  }
  into.push(target);
  return into;
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

  const directives = [...stripCssComments(css).matchAll(/@source\s+"([^"]+)"/g)].map((match) => match[1]);
  if (directives.length === 0) {
    throw new Error(`${INPUT_CSS} declares no @source; the report would call every token dead`);
  }

  const files = [];
  for (const directive of directives) {
    walkSourceTree(path.resolve(cssDir, directive), directive, files, new Set());
  }

  const read = (file) => {
    const relative = path.relative(repoRoot, file).split(path.sep).join("/");
    const text = readFileSync(file, "utf8");
    return { path: relative, text, fellBack: stripSourceComments(text, languageOf(relative)).fellBack };
  };
  const all = files.map(read);

  return {
    css,
    sources: all.filter((file) => !isTestSource(file.path)),
    testSources: all.filter((file) => isTestSource(file.path)),
    unparsed: all.filter((file) => file.fellBack).map((file) => file.path)
  };
}

export function formatReport({ deadUtilities, deadProperties, testOnlyUtilities = [], testOnlyProperties = [] }) {
  const lines = [];
  for (const name of deadUtilities) {
    lines.push(`@utility ${name} — declared in ${INPUT_CSS}, named by no source outside a comment and composed by no @apply`);
  }
  for (const name of deadProperties) {
    lines.push(`${name} — declared in ${INPUT_CSS}, read by no live rule, no live declaration and no source`);
  }
  for (const { name, tests } of testOnlyUtilities) {
    lines.push(`@utility ${name} — declared in ${INPUT_CSS}, named ONLY by ${tests.join(", ")}; delete the declaration and the test's reference together`);
  }
  for (const { name, tests } of testOnlyProperties) {
    lines.push(`${name} — declared in ${INPUT_CSS}, named ONLY by ${tests.join(", ")}; delete the declaration and the test's reference together`);
  }
  return lines.join("\n");
}

const invokedDirectly = process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url);
if (invokedDirectly) {
  const repoRoot = path.resolve(import.meta.dirname, "..");
  const graph = readCssGraph(repoRoot);
  if (graph.unparsed.length > 0) {
    console.error(`scanned as raw text (comments could not be resolved): ${graph.unparsed.join(", ")}`);
  }
  const report = findDeadCssTokens(graph);
  const text = formatReport(report);
  if (text) {
    const count = report.deadUtilities.length + report.testOnlyUtilities.length;
    const properties = report.deadProperties.length + report.testOnlyProperties.length;
    console.error(`dead CSS tokens (${count} utilities, ${properties} properties):`);
    console.error(text);
    process.exit(1);
  }
  console.log("no dead CSS tokens");
}
