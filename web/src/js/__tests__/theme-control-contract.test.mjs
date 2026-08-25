// The theme control moved once already: a header toggle bound to
// `[data-theme-option]` was replaced by the settings-interface radiogroup bound
// to `[data-settings-interface-theme-option]`. The template half of the move
// landed; the JS half did not, and the orphaned binder kept running on every
// page for as long as nobody grepped for its selector.
//
// This guard is scoped to that one pair of hooks on purpose. It pins both
// directions of the move: the retired selector must not reappear in any client
// source, and the surviving selector must still be the one the settings panel
// renders and the one the bundle queries. A dead binder is silent by nature —
// it costs a querySelectorAll per init and reports nothing — so the only thing
// that can notice it is a check that names the selector.

import test from "node:test";
import assert from "node:assert/strict";
import { readdirSync, readFileSync, statSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";

import { readAppBundle, loadDOMWithScript } from "./_helpers.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, "..", "..", "..", "..");
const clientSourceRoot = path.join(repoRoot, "web", "src", "js");

const RETIRED_HOOK = "data-theme-option";
const LIVE_HOOK = "data-settings-interface-theme-option";

function clientSourceFiles(directory) {
  const found = [];
  for (const entry of readdirSync(directory)) {
    const full = path.join(directory, entry);
    if (statSync(full).isDirectory()) {
      // The tests themselves name the retired hook; they are the guard, not a
      // consumer of it.
      if (entry === "__tests__") {
        continue;
      }
      found.push(...clientSourceFiles(full));
      continue;
    }
    if (entry.endsWith(".js")) {
      found.push(full);
    }
  }
  return found;
}

test("no client source binds the retired [data-theme-option] hook", () => {
  const offenders = [];
  for (const file of clientSourceFiles(clientSourceRoot)) {
    const source = readFileSync(file, "utf8");
    source.split("\n").forEach((line, index) => {
      // `data-settings-interface-theme-option` contains the retired string as a
      // suffix; only the standalone attribute name is the defect.
      if (/(^|[^-])data-theme-option/.test(line)) {
        offenders.push(`${path.relative(repoRoot, file)}:${index + 1}: ${line.trim()}`);
      }
    });
  }

  assert.deepEqual(
    offenders,
    [],
    `the retired theme hook is bound by client sources no template renders:\n${offenders.join("\n")}`,
  );
});

test("the app bundle installs nothing on a legacy [data-theme-option] control", async () => {
  const dom = await loadDOMWithScript(readAppBundle(), {
    html: `<!doctype html><html><head></head><body>
      <button type="button" data-theme-option="dark">Dark</button>
    </body></html>`,
  });

  const button = dom.window.document.querySelector("[data-theme-option]");
  assert.ok(button, "fixture must render the legacy control");
  assert.equal(
    button.dataset.themeToggleBound,
    undefined,
    "the bundle still binds the retired theme hook on init",
  );
  assert.equal(
    button.getAttribute("aria-pressed"),
    null,
    "the bundle still relabels the retired theme hook on init",
  );
});

test("the shipped theme control is the settings-interface hook", () => {
  const controller = readFileSync(
    path.join(clientSourceRoot, "app", "55-settings-interface.js"),
    "utf8",
  );
  assert.ok(
    controller.includes(`[${LIVE_HOOK}]`),
    "the settings-interface controller must query the live theme hook",
  );

  const panel = readFileSync(
    path.join(repoRoot, "internal", "templates", "components", "settings_interface.html"),
    "utf8",
  );
  assert.ok(
    panel.includes(LIVE_HOOK),
    "the settings-interface panel must render the live theme hook",
  );
  assert.ok(
    !panel.includes(`"${RETIRED_HOOK}`),
    "the settings-interface panel must not resurrect the retired theme hook",
  );
});
