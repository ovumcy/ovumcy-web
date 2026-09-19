// Not picked up by `npm run test:unit`'s glob (web/src/js/__tests__/*.test.mjs
// is non-recursive, and this file lives one directory below it). Run
// standalone via `node --test <this file>` as a child process by
// harness-unhandled-errors.test.mjs, to prove that an event-handler
// exception still fails the test even when the test never calls
// dom.window.close() — the afterEach hook in ../_helpers.mjs is what must
// catch it here, not an explicit close() call.
import test from "node:test";
import { loadDOMWithScript } from "../_helpers.mjs";

test("fixture: handler throws and this test never calls close()", async () => {
  await loadDOMWithScript(
    'document.addEventListener("click", () => { throw new Error("fixture: never-closed handler boom"); });' +
      "document.dispatchEvent(new Event('click'));",
    { html: "<!doctype html><html><body></body></html>" }
  );
  // Deliberately no dom.window.close() call here.
});
