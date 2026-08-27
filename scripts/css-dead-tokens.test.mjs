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
// The first assertion is the report over the real tree. Everything after it
// exists because that green is only worth something if the report can tell a
// dead token from a live one, and every one of those judgements has a way to be
// wrong in the direction that DELETES something live: a comment scanner that
// eats a class off a line with a URL on it, a functional `@utility name-*` whose
// stem no real use matches, a reachability pass that stops one link short.

import test from "node:test";
import assert from "node:assert/strict";
import path from "node:path";
import { mkdtempSync, mkdirSync, writeFileSync, symlinkSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";

import {
  findDeadCssTokens,
  formatReport,
  isTestSource,
  languageOf,
  readCssGraph,
  sameMembers,
  stripSourceComments,
  tokenize,
  walkSourceTree
} from "./css-dead-tokens.mjs";

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

test("every scanned source parses; none falls back to raw text", () => {
  // The fallback counts every token in a file as live, so a file that silently
  // took it would weaken the first assertion by exactly its own contents. A
  // baseline here would make this a list rather than a barrier.
  const { unparsed } = readCssGraph(repoRoot);
  assert.deepEqual(unparsed, []);
});

// ---------------------------------------------------------------------------
// Finding 1: prose is not a consumer, on either side
// ---------------------------------------------------------------------------

const COMMENT_CSS = `
@utility widget-live {
  color: green;
}
@utility widget-retired {
  color: red;
}
:root {
  --widget-live-ink: #0f0;
  --widget-retired-ink: #f00;
}
`;

test("a Go comment naming a token does not keep it alive", () => {
  const goSource = [
    "package httpx",
    "",
    "// widget-retired was dropped in 1.4.0; it read --widget-retired-ink.",
    "/* The same claim in a block comment, naming widget-retired again. */",
    'const markup = "<div class=\\"widget-live\\" style=\\"color:var(--widget-live-ink)\\"></div>"'
  ].join("\n");
  const report = findDeadCssTokens({
    css: COMMENT_CSS,
    sources: [{ path: "internal/httpx/markup.go", text: goSource }],
    testSources: []
  });
  assert.deepEqual(report.deadUtilities, ["widget-retired"]);
  assert.deepEqual(report.deadProperties, ["--widget-retired-ink"]);
});

test("a `//` inside a Go string is string content, not a comment", () => {
  // The whole reason the scanner walks characters instead of matching a regex.
  const goSource = 'const link = "https://example.com/widget-retired" + " class=widget-live"';
  const { text, fellBack } = stripSourceComments(goSource, "go");
  assert.equal(fellBack, false);
  assert.ok(text.includes("widget-live"), "the class after the URL must survive");
  assert.ok(text.includes("widget-retired"), "text inside the string literal must survive");
});

test("an HTML comment is stripped and an href on a live line is not", () => {
  // internal/templates/privacy.html:51 carries href="https://…" and
  // class="inline-link" on ONE line. A `//`-to-end-of-line rule applied to markup
  // would report `inline-link` dead and a reader would delete a class the privacy
  // page renders.
  const markup = [
    "<!-- widget-retired was removed here -->",
    '<a href="https://github.com/ovumcy/ovumcy-web" rel="noopener" class="widget-live">link</a>',
    "{{/* widget-retired again, in a template comment */}}"
  ].join("\n");
  const { text, fellBack } = stripSourceComments(markup, "html");
  assert.equal(fellBack, false);
  assert.ok(text.includes("widget-live"), "the class beside the URL must survive");
  assert.ok(!text.includes("widget-retired"), "both comment forms must be stripped");
});

test("a JS regex literal is not mistaken for a comment", () => {
  // web/src/js/timezone-bootstrap.js:9 is `/^[A-Za-z0-9_+/-]+$/` — a slash inside
  // a character class. A scanner without regex handling can read a literal's
  // contents as a comment opener and swallow the rest of the file, which would
  // silently drop every class named below it.
  const jsSource = [
    'var ok = /^[A-Za-z0-9_+/-]+$/.test(value);',
    'var odd = /a\\/\\/b/.test(value);',
    'var block = /[/*]/.test(value);',
    'element.className = "widget-live";'
  ].join("\n");
  const { text, fellBack } = stripSourceComments(jsSource, "js");
  assert.equal(fellBack, false);
  assert.ok(text.includes("widget-live"), "code after the regex literals must survive");

  const report = findDeadCssTokens({ css: COMMENT_CSS, sources: [{ path: "web/src/js/x.js", text: jsSource }], testSources: [] });
  assert.ok(report.deadUtilities.includes("widget-retired"));
  assert.ok(!report.deadUtilities.includes("widget-live"));
});

test("division is not read as a regex literal", () => {
  const jsSource = 'var ratio = width / height; var other = a / b; var cls = "widget-live";';
  const { text, fellBack } = stripSourceComments(jsSource, "js");
  assert.equal(fellBack, false);
  assert.ok(text.includes("widget-live"));
});

test("an unterminated comment falls back to raw text rather than eating the file", () => {
  const { text, fellBack } = stripSourceComments("/* never closed\nwidget-live", "js");
  assert.equal(fellBack, true);
  assert.ok(text.includes("widget-live"), "the fallback keeps everything live, which is the safe direction");
});

test("languageOf maps the extensions the @source set actually holds", () => {
  assert.equal(languageOf("internal/httpx/markup.go"), "go");
  assert.equal(languageOf("web/static/js/chart-lite.js"), "js");
  assert.equal(languageOf("web/src/js/__tests__/x.test.mjs"), "js");
  assert.equal(languageOf("internal/templates/base.html"), "html");
  assert.equal(languageOf("web/static/img/logo.svg"), "unknown");
});

// ---------------------------------------------------------------------------
// Finding 2: functional utilities
// ---------------------------------------------------------------------------

const FUNCTIONAL_CSS = `
@utility tab-* {
  tab-size: --value(integer);
}
@utility pane-* {
  width: --value(integer);
}
`;

test("a functional @utility is live from any suffixed use, not from its bare stem", () => {
  const report = findDeadCssTokens({
    css: FUNCTIONAL_CSS,
    sources: [{ path: "internal/templates/x.html", text: '<div class="tab-4"></div>' }],
    testSources: []
  });
  // `tab-*` is consumed as `tab-4`; matching the stem `tab-` with identifier
  // boundaries needs a non-identifier character after the hyphen, which no real
  // use has, so an unhandled `*` reports every functional utility dead — in the
  // one direction the header calls dangerous. `pane-*` is the control.
  assert.deepEqual(report.deadUtilities, ["pane-*"]);
});

test("the `*` survives into the report so the reader can find the declaration", () => {
  const report = findDeadCssTokens({ css: FUNCTIONAL_CSS, sources: [], testSources: [] });
  assert.deepEqual(report.deadUtilities, ["tab-*", "pane-*"]);
  assert.ok(formatReport(report).startsWith("@utility tab-* —"));
});

// ---------------------------------------------------------------------------
// Finding 3: reachability is closed, not single-pass
// ---------------------------------------------------------------------------

const CHAIN_CSS = `
:root {
  --leaf: #111;
  --middle: var(--leaf);
  --root-token: var(--middle);
}
@utility widget-live {
  color: var(--root-token);
}
@utility widget-retired {
  background: rgb(var(--only-the-dead-utility-reads-me));
}
:root {
  --only-the-dead-utility-reads-me: 1 2 3;
}
`;

test("a chain of properties read only by each other collapses in one run", () => {
  // Single-pass, `--middle` reads live because dead `--root-token` names it, and
  // `--leaf` reads live because dead `--middle` names it: three runs to find
  // three tokens, with a green report between each.
  const report = findDeadCssTokens({
    css: CHAIN_CSS,
    sources: [{ path: "internal/templates/x.html", text: '<div class="widget-live"></div>' }],
    testSources: []
  });
  assert.deepEqual(report.deadUtilities, ["widget-retired"]);
  assert.deepEqual(report.deadProperties.sort(), ["--only-the-dead-utility-reads-me"]);
});

test("a property read only from inside a dead utility is dead in the same run", () => {
  // This is the lag that produced the fourth finding of the first pass:
  // 0282f024 deleted `@utility calendar-tag-ovulation`, whose body held the only
  // `rgb(var(--cal-ovulation-solid))`, and left the property for a later wave.
  const report = findDeadCssTokens({ css: CHAIN_CSS, sources: [], testSources: [] });
  assert.ok(report.deadUtilities.includes("widget-retired"));
  assert.ok(report.deadProperties.includes("--only-the-dead-utility-reads-me"));
  // With `widget-live` dead too, the whole chain behind it goes in the same run.
  assert.deepEqual(report.deadProperties.sort(), [
    "--leaf",
    "--middle",
    "--only-the-dead-utility-reads-me",
    "--root-token"
  ]);
});

test("an @apply inside a dead utility does not keep its target alive", () => {
  const report = findDeadCssTokens({
    css: `
@utility widget-base { color: red; }
@utility widget-retired { @apply widget-base; }
`,
    sources: [],
    testSources: []
  });
  assert.deepEqual(report.deadUtilities.sort(), ["widget-base", "widget-retired"]);
});

test("an @apply inside a live utility does keep its target alive", () => {
  const report = findDeadCssTokens({
    css: `
@utility widget-base { color: red; }
@utility widget-live { @apply widget-base; }
`,
    sources: [{ path: "internal/templates/x.html", text: '<div class="widget-live"></div>' }],
    testSources: []
  });
  assert.deepEqual(report.deadUtilities, []);
});

// ---------------------------------------------------------------------------
// The chart-lite clause, and the neighbour trap
// ---------------------------------------------------------------------------

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
    sources: [{ path: "web/static/js/chart-lite.js", text: CHART_JS }],
    testSources: []
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
    sources: [{ path: "x.html", text: `class="card-quiet"` }],
    testSources: []
  });
  assert.deepEqual(report.deadProperties, ["--bg-soft"]);
  assert.deepEqual(report.deadUtilities, []);
});

test("a property declared in two theme blocks is not read by its own second declaration", () => {
  // Erasing one declaration and leaving the other would make every dual-theme
  // token look live — which is every colour token in the file.
  const report = findDeadCssTokens({
    css: NEIGHBOUR_CSS.replace("var(--bg-soft-strong)", "transparent"),
    sources: [],
    testSources: []
  });
  assert.deepEqual(report.deadProperties.sort(), ["--bg-soft", "--bg-soft-strong"]);
});

test("tokenize cuts on identifier boundaries, which is where the neighbour trap lives", () => {
  const tokens = tokenize('background: rgb(var(--bg-soft-strong) / 0.2); .card-quiet {}');
  assert.equal(tokens.has("--bg-soft-strong"), true);
  assert.equal(tokens.has("--bg-soft"), false);
  assert.equal(tokens.has("card-quiet"), true);
});

// ---------------------------------------------------------------------------
// Finding 4: a test-only token gets its own category and its own sentence
// ---------------------------------------------------------------------------

test("a token named only by a test is reported separately, naming the test", () => {
  const report = findDeadCssTokens({
    css: "@utility widget-retired { animation: none; }\n:root { --widget-retired-ink: #f00; }",
    sources: [],
    testSources: [{ path: "internal/api/widget_regression_test.go", text: '"widget-retired" and "--widget-retired-ink"' }]
  });
  assert.deepEqual(report.deadUtilities, []);
  assert.deepEqual(report.deadProperties, []);
  assert.deepEqual(report.testOnlyUtilities, [
    { name: "widget-retired", tests: ["internal/api/widget_regression_test.go"] }
  ]);
  assert.deepEqual(report.testOnlyProperties, [
    { name: "--widget-retired-ink", tests: ["internal/api/widget_regression_test.go"] }
  ]);
  // The wording is the point of the split: "named by no source" is false for
  // these, and a reader who deletes on that sentence reddens a test with no
  // warning the two were about to collide.
  const text = formatReport(report);
  assert.ok(text.includes("named ONLY by internal/api/widget_regression_test.go"));
  assert.ok(text.includes("delete the declaration and the test's reference together"));
  assert.ok(!text.includes("named by no source"));
  // It still fails the check: the token is dead in production either way.
  assert.notEqual(text, "");
});

test("a production consumer outranks a test one", () => {
  const report = findDeadCssTokens({
    css: "@utility widget-retired { animation: none; }",
    sources: [{ path: "internal/httpx/markup.go", text: `"widget-retired"` }],
    testSources: [{ path: "internal/httpx/markup_test.go", text: `"widget-retired"` }]
  });
  assert.deepEqual(report.deadUtilities, []);
  assert.deepEqual(report.testOnlyUtilities, []);
});

test("test sources are separated from production ones by path, and the real tree has both", () => {
  assert.equal(isTestSource(path.join("internal", "httpx", "markup_test.go")), true);
  assert.equal(isTestSource(path.join("internal", "httpx", "markup.go")), false);
  assert.equal(isTestSource(path.join("web", "src", "js", "__tests__", "_helpers.mjs")), true);
  assert.equal(isTestSource(path.join("web", "src", "js", "app", "toast.js")), false);

  const { sources, testSources } = readCssGraph(repoRoot);
  assert.equal(sources.filter((source) => isTestSource(source.path)).length, 0);
  assert.ok(testSources.length > 0, "the @source set does cover test files; they must reach the test category");
  assert.ok(testSources.every((source) => isTestSource(source.path)));
});

test("a token named only in a stylesheet comment is not a consumer", () => {
  const report = findDeadCssTokens({
    css: `/* --bg-soft and widget-retired are described here */\n:root { --bg-soft: #fff4e8; }\n@utility widget-retired { animation: none; }`,
    sources: [],
    testSources: []
  });
  assert.deepEqual(report.deadProperties, ["--bg-soft"]);
  assert.deepEqual(report.deadUtilities, ["widget-retired"]);
});

test("a bare selector naming a class is not a consumer, but markup is", () => {
  const css = `
@utility widget-live { color: red; }
@utility widget-retired { color: blue; }
@utility widget-host {
  &:is(.widget-retired) { opacity: 0; }
}
`;
  const report = findDeadCssTokens({
    css,
    sources: [{ path: "internal/templates/x.html", text: '<div class="widget-live widget-host"></div>' }],
    testSources: []
  });
  // A rule inside a live component that names a dead class survives the class's
  // death and can never match — the exact residue this report hunts, so it
  // cannot be allowed to count as a consumer.
  assert.deepEqual(report.deadUtilities, ["widget-retired"]);
});

// ---------------------------------------------------------------------------
// Findings 5 and 7: reading the tree
// ---------------------------------------------------------------------------

test("a missing @source target is diagnosed by directive, not by ENOENT", () => {
  assert.throws(
    () => walkSourceTree(path.join(repoRoot, "web", "src", "css", "no-such-tree"), "../no-such-tree"),
    (error) => {
      assert.match(error.message, /@source "\.\.\/no-such-tree"/);
      assert.match(error.message, /ENOENT/);
      assert.match(error.message, /Name a directory or file that exists/);
      return true;
    }
  );
});

test("a glob @source target is diagnosed as a glob", () => {
  // Tailwind accepts `@source "../js/**/*.js"`; path.resolve turns it into a
  // literal path that does not exist, and the raw ENOENT names neither the
  // directive nor the reason.
  assert.throws(
    () => walkSourceTree(path.join(repoRoot, "web", "src", "js", "**", "*.js"), "../js/**/*.js"),
    (error) => {
      assert.match(error.message, /@source "\.\.\/js\/\*\*\/\*\.js"/);
      assert.match(error.message, /name the directory the glob covers/);
      return true;
    }
  );
});

test("a symlink cycle under a source tree terminates instead of exhausting the stack", () => {
  // This repository creates git worktrees under its own root, so a link pointing
  // at an ancestor is not hypothetical. `junction` is the type Windows accepts
  // without elevation and POSIX ignores.
  const root = mkdtempSync(path.join(tmpdir(), "css-dead-tokens-"));
  try {
    const nested = path.join(root, "nested");
    mkdirSync(nested);
    writeFileSync(path.join(nested, "markup.go"), `const c = "widget-live"`);
    symlinkSync(root, path.join(nested, "loop"), "junction");

    const files = walkSourceTree(root, "../fixture");
    assert.deepEqual(
      files.map((file) => path.relative(root, file).split(path.sep).join("/")),
      ["nested/markup.go"]
    );
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
});

// ---------------------------------------------------------------------------
// The convergence test, the required field, and the postfix-operator position
// ---------------------------------------------------------------------------

test("a fixed point converges on membership, not on how many members are left", () => {
  // No edge in the report today can produce a round that swaps one member for
  // another — every round is a subset of the one before it — so this cannot be
  // asserted through the report, and the helper is tested directly instead. That
  // is precisely the point: the subset property is a fact about today's three
  // edges, and the loops are where a fourth gets added. A size comparison exits
  // on the middle set below and computes the report from a half-converged state
  // with nothing red.
  assert.equal(sameMembers(new Set(["a", "b"]), new Set(["a", "b"])), true);
  assert.equal(sameMembers(new Set(["a", "b"]), new Set(["b", "a"])), true);
  assert.equal(sameMembers(new Set(["a", "b"]), new Set(["a", "c"])), false);
  assert.equal(sameMembers(new Set(["a"]), new Set(["a", "b"])), false);
  assert.equal(sameMembers(new Set(), new Set()), true);
});

test("testSources is required, because its absence is the wrong message rather than no message", () => {
  assert.throws(
    () => findDeadCssTokens({ css: "@utility widget-retired { color: red; }", sources: [] }),
    (error) => {
      assert.match(error.message, /testSources is required/);
      return true;
    }
  );
  // An explicit empty list is a claim and is accepted as one.
  const report = findDeadCssTokens({ css: "@utility widget-retired { color: red; }", sources: [], testSources: [] });
  assert.deepEqual(report.deadUtilities, ["widget-retired"]);
});

test("a slash after ++ or -- is division, so the comment behind it is still stripped", () => {
  // `+`, `-` and `*` all legitimately precede a regex, so the character set alone
  // permits one after `a++` and the scan runs off to the next unescaped slash —
  // which is the `//` opener, leaving the comment in place and its tokens live.
  for (const operator of ["++", "--"]) {
    const source = `var n = a${operator} / b; // widget-retired was removed in 1.4.0\n`;
    const { text, fellBack } = stripSourceComments(source, "js");
    assert.equal(fellBack, false);
    assert.ok(!text.includes("widget-retired"), `the comment after \`a${operator} / b\` must still be stripped`);
  }
});

test("a slash after a single + is still a regex position", () => {
  // The control for the exclusion above: closing the `++` case must not close
  // `a + /re/`, where the literal really does follow the operator. The apostrophe
  // inside the literal is the observable — read as a string opener it runs to the
  // end of the line and the file stops resolving.
  const { fellBack } = stripSourceComments("var x = 1 + /'/.test(s);\nvar cls = \"widget-live\";\n", "js");
  assert.equal(fellBack, false);
});
