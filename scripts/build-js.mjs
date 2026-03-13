import { copyFileSync, readFileSync, writeFileSync } from "node:fs";

const appBundleSources = [
  "./web/src/js/app/00-core.js",
  "./web/src/js/app/10-language-auth-transitions.js",
  "./web/src/js/app/20-password-toggles.js",
  "./web/src/js/app/21-login-form-ui.js",
  "./web/src/js/app/22-confirm-modal.js",
  "./web/src/js/app/23-cycle-start-confirm.js",
  "./web/src/js/app/24-pwa-install.js",
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

function buildBundle(sources) {
  return sources
    .map((source) => readFileSync(source, "utf8").trimEnd())
    .join("\n\n") + "\n";
}

const appBundle = buildBundle(appBundleSources);
writeFileSync("./web/src/js/app.js", appBundle, "utf8");
writeFileSync("./web/static/js/app.js", appBundle, "utf8");

const settingsExportBundle = buildBundle(settingsExportBundleSources);
writeFileSync("./web/src/js/settings-export.js", settingsExportBundle, "utf8");
writeFileSync("./web/static/js/settings-export.js", settingsExportBundle, "utf8");

const buildTargets = [
  ["./node_modules/htmx.org/dist/htmx.min.js", "./web/static/js/htmx.min.js"],
  ["./web/src/js/theme-bootstrap.js", "./web/static/js/theme-bootstrap.js"],
  ["./web/src/js/timezone-bootstrap.js", "./web/static/js/timezone-bootstrap.js"]
];

for (const [source, destination] of buildTargets) {
  copyFileSync(source, destination);
}
