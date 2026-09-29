import { readFileSync, realpathSync, writeFileSync } from "node:fs";
import { fileURLToPath } from "node:url";

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
  "./web/src/js/app/50-chrome.js",
  "./web/src/js/app/51-day-inputs.js",
  "./web/src/js/app/52-dashboard.js",
  "./web/src/js/app/53-dashboard-autosave.js",
  "./web/src/js/app/54-settings-forms.js",
  "./web/src/js/app/54b-settings-sections.js",
  "./web/src/js/app/55-settings-interface.js",
  "./web/src/js/app/56-calendar.js",
  "./web/src/js/app/57-onboarding.js",
  "./web/src/js/app/58-recovery.js",
  "./web/src/js/app/59-init.js",
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

// The banner names the installed htmx; a node_modules that drifted from the
// lockfile would otherwise ship a different htmx body under a stale banner.
export function resolveHtmxVersion(installedPackage, lockfile) {
  const installed = installedPackage?.version;
  const locked = lockfile?.packages?.["node_modules/htmx.org"]?.version;
  if (!locked) {
    throw new Error("package-lock.json has no node_modules/htmx.org entry");
  }
  if (installed !== locked) {
    throw new Error(
      `installed htmx.org ${installed} does not match package-lock.json ${locked}; run \`npm ci\``
    );
  }
  return installed;
}

function readJSON(path) {
  return JSON.parse(readFileSync(path, "utf8"));
}

function build() {
  let htmxVersion;
  try {
    htmxVersion = resolveHtmxVersion(
      readJSON("./node_modules/htmx.org/package.json"),
      readJSON("./package-lock.json")
    );
  } catch (error) {
    console.error(`build-js: ${error.message}`);
    process.exit(1);
  }

  const appBundle = buildBundle(appBundleSources);
  writeLF("./web/static/js/app.js", appBundle);

  const settingsExportBundle = buildBundle(settingsExportBundleSources);
  writeLF("./web/static/js/settings-export.js", settingsExportBundle);

  const settingsImportBundle = buildBundle(settingsImportBundleSources);
  writeLF("./web/static/js/settings-import.js", settingsImportBundle);

  const htmxLicenseBanner =
    "/*!\n" +
    ` * htmx.org ${htmxVersion}\n` +
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
}

// argv[1] is unresolved while import.meta.url is realpath'd by the ESM loader;
// comparing them raw turns a run through a junction into a silent no-op.
if (process.argv[1] && realpathSync(process.argv[1]) === fileURLToPath(import.meta.url)) {
  build();
}
