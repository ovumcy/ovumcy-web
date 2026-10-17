import test from "node:test";
import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { cpSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = fileURLToPath(new URL("../../../../", import.meta.url));

// A copy of the checkout, holding only what the JS build reads, so the build
// under test writes into the copy and never into the tree running the suite.
function copyCheckoutForBuild(root) {
  for (const path of [
    "scripts/build-js.mjs",
    "package-lock.json",
    "web/src/js/app",
    "web/src/js/settings-export",
    "web/src/js/settings-import",
    "web/src/js/theme-bootstrap.js",
    "web/src/js/timezone-bootstrap.js"
  ]) {
    cpSync(join(repoRoot, path), join(root, path), { recursive: true });
  }
  for (const path of ["package.json", "dist/htmx.min.js"]) {
    cpSync(join(repoRoot, "node_modules/htmx.org", path), join(root, "node_modules/htmx.org", path));
  }
  mkdirSync(join(root, "web/static/js"), { recursive: true });
}

test("build-js builds its own checkout when started from another directory", (t) => {
  const scratch = mkdtempSync(join(tmpdir(), "build-js-own-checkout-"));
  t.after(() => rmSync(scratch, { recursive: true, force: true }));
  const checkout = join(scratch, "checkout");
  const elsewhere = join(scratch, "elsewhere");
  copyCheckoutForBuild(checkout);
  mkdirSync(elsewhere);

  const result = spawnSync(process.execPath, [join(checkout, "scripts/build-js.mjs")], {
    cwd: elsewhere,
    encoding: "utf8"
  });

  assert.equal(result.status, 0, result.stderr);
  // Read against the copied sources, not the suite's committed bundle, so a
  // source edited ahead of its rebuild does not turn this test red.
  const bundle = readFileSync(join(checkout, "web/static/js/app.js"), "utf8");
  const firstSource = readFileSync(join(checkout, "web/src/js/app/00-core.js"), "utf8").trimEnd();
  assert.ok(
    bundle.replace(/\r\n?/g, "\n").startsWith(firstSource.replace(/\r\n?/g, "\n")),
    "the bundle is built from, and written into, the script's own checkout"
  );
  assert.equal(existsSync(join(elsewhere, "web")), false, "nothing is read from or written into the starting directory");
});
