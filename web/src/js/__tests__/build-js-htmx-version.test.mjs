import test from "node:test";
import assert from "node:assert/strict";

import { resolveHtmxVersion } from "../../../../scripts/build-js.mjs";

function lockWith(version) {
  return { packages: { "node_modules/htmx.org": { version } } };
}

test("resolveHtmxVersion returns the version when install matches the lockfile", () => {
  assert.equal(resolveHtmxVersion({ version: "2.0.11" }, lockWith("2.0.11")), "2.0.11");
});

test("resolveHtmxVersion throws naming both versions on a stale install", () => {
  assert.throws(
    () => resolveHtmxVersion({ version: "2.0.10" }, lockWith("2.0.11")),
    (error) =>
      error.message.includes("2.0.10") &&
      error.message.includes("2.0.11") &&
      error.message.includes("npm ci")
  );
});

test("resolveHtmxVersion throws when the lockfile has no htmx entry", () => {
  assert.throws(
    () => resolveHtmxVersion({ version: "2.0.11" }, { packages: {} }),
    /node_modules\/htmx\.org/
  );
});
