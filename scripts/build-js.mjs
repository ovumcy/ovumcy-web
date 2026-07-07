import { readFileSync, writeFileSync } from "node:fs";

const appBundleSources = [
  "./web/src/js/app/00-core.js",
  "./web/src/js/app/10-language-auth-transitions.js",
  "./web/src/js/app/20-password-toggles.js",
  "./web/src/js/app/21-login-form-ui.js",
  "./web/src/js/app/22-confirm-modal.js",
  "./web/src/js/app/22b-clear-data-confirm.js",
  "./web/src/js/app/23-cycle-start-confirm.js",
  "./web/src/js/app/24-pwa-install.js",
  "./web/src/js/app/25-timezone-sync.js",
  "./web/src/js/app/30-feedback-htmx.js",
  "./web/src/js/app/40-shared-utils.js",
  "./web/src/js/app/50-window-factories.js",
  "./web/src/js/app/90-bootstrap.js"
];

const settingsExportBundleSources = [
  "./web/src/js/settings-export/00-core.js",
  "./web/src/js/settings-export/10-context-range-summary.js",
  "./web/src/js/settings-export/20-calendar-controller.js",
  "./web/src/js/settings-export/30-export-and-bootstrap.js"
];

const settingsImportBundleSources = [
  "./web/src/js/settings-import/00-restore.js"
];

// Force LF on every emitted bundle: a source that drifted to CRLF in the working
// tree must not leak into a committed bundle and trip the "bundles must match a
// fresh build" CI guard (otherwise only reproducible on Windows checkouts).
function toLF(text) {
  return text.replace(/\r\n?/g, "\n");
}

function writeLF(destination, text) {
  writeFileSync(destination, toLF(text), "utf8");
}

function buildBundle(sources) {
  return sources
    .map((source) => readFileSync(source, "utf8").trimEnd())
    .join("\n\n") + "\n";
}

const appBundle = buildBundle(appBundleSources);
writeLF("./web/static/js/app.js", appBundle);

const settingsExportBundle = buildBundle(settingsExportBundleSources);
writeLF("./web/static/js/settings-export.js", settingsExportBundle);

const settingsImportBundle = buildBundle(settingsImportBundleSources);
writeLF("./web/static/js/settings-import.js", settingsImportBundle);

const htmxLicenseBanner =
  "/*!\n" +
  " * htmx.org 2.0.10\n" +
  " * 0BSD License, see THIRD_PARTY_LICENSES.md\n" +
  " */\n";

const htmxSource = readFileSync("./node_modules/htmx.org/dist/htmx.min.js", "utf8");
writeLF("./web/static/js/htmx.min.js", htmxLicenseBanner + htmxSource);

const buildTargets = [
  ["./web/src/js/theme-bootstrap.js", "./web/static/js/theme-bootstrap.js"],
  ["./web/src/js/timezone-bootstrap.js", "./web/static/js/timezone-bootstrap.js"]
];

for (const [source, destination] of buildTargets) {
  writeLF(destination, readFileSync(source, "utf8"));
}
