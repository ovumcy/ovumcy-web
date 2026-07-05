import js from "@eslint/js";
import globals from "globals";

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
  }
];
