import js from "@eslint/js";
import globals from "globals";
import tseslint from "typescript-eslint";

export default [
  {
    ignores: [
      "node_modules/",
      "web/src/js/app/*.js",
      "web/src/js/settings-export/*.js",
      "web/static/js/htmx.min.js",
      "scripts/take-screenshots.mjs"
    ]
  },
  {
    // The app/ and settings-export/ sources are non-standalone IIFE fragments
    // (the wrapper open/close and shared closure are split across files), so
    // they are ignored above and linted through their concatenated bundles
    // in web/static/js instead. build-js.mjs keeps those bundles in sync and
    // the CI stale-bundle guard enforces it.
    files: [
      "web/src/js/**/*.js",
      "web/static/js/chart-lite.js",
      "web/static/js/app.js",
      "web/static/js/settings-export.js"
    ],
    ...js.configs.recommended,
    languageOptions: {
      ecmaVersion: "latest",
      sourceType: "script",
      globals: {
        ...globals.browser,
        htmx: "readonly"
      }
    }
  },
  {
    files: ["scripts/*.mjs"],
    ...js.configs.recommended,
    languageOptions: {
      ecmaVersion: "latest",
      sourceType: "module",
      globals: globals.node
    }
  },
  {
    files: ["web/src/js/__tests__/**/*.mjs"],
    ...js.configs.recommended,
    languageOptions: {
      ecmaVersion: "latest",
      sourceType: "module",
      globals: {
        ...globals.node,
        setImmediate: "readonly"
      }
    }
  },
  // The browser e2e suite. Until this block existed nothing checked e2e/ at
  // all — `lint:js` never scoped there and TypeScript was not a dependency —
  // so a broken signature or a dangling import surfaced only as a failing spec
  // minutes into a browser run. `npm run lint:types` (tsc --noEmit) covers the
  // types; this covers what a type-checker does not judge.
  //
  // Type-AWARE, on purpose. The rule that pays for the whole block is
  // `no-floating-promises`: a Playwright call whose `await` went missing still
  // passes, silently, because the assertion it should have made never runs
  // before the test ends. That is a false GREEN, the failure mode this
  // repository cares about most, and no untyped linter can see it.
  ...tseslint.configs.recommendedTypeChecked.map((config) => ({
    ...config,
    files: ["e2e/**/*.ts", "playwright.config.ts"]
  })),
  {
    files: ["e2e/**/*.ts", "playwright.config.ts"],
    languageOptions: {
      parserOptions: {
        projectService: true,
        tsconfigRootDir: import.meta.dirname
      }
    },
    rules: {
      // The suite's own house style, already followed ~20 times: assert a
      // response is not null, then read it through `!`. Forbidding the
      // assertion would mean rewriting every one of those into a type guard
      // for no gain — the `expect(...).not.toBeNull()` on the line above is
      // what makes the failure legible.
      "@typescript-eslint/no-non-null-assertion": "off"
    }
  }
];
