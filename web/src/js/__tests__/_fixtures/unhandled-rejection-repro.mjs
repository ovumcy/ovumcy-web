// Not a node:test file (no *.test.mjs suffix, so `npm run test:unit`'s glob
// never picks it up). Run standalone via a plain `node` child process by
// harness-unhandled-errors.test.mjs to prove that a promise rejected inside
// jsdom-evaluated script — and never caught — crashes the process the way an
// unhandled rejection always does in Node, with no help needed from this
// suite's harness. See the comment on that channel in ../_helpers.mjs.
import { loadDOMWithScript } from "../_helpers.mjs";

const dom = await loadDOMWithScript(
  'Promise.reject(new Error("fixture: rejection boom"));',
  { html: "<!doctype html><html><body></body></html>" }
);

// Give the rejection a turn to be flagged unhandled before we would
// otherwise exit cleanly.
await new Promise((resolve) => setTimeout(resolve, 50));
dom.window.close();
