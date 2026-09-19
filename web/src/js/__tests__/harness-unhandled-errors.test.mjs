// Self-test for the harness invariant added on top of loadDOMWithScript
// (WEB-17 TS-M13): any unhandled error or rejection raised in the jsdom
// window during a test must fail THAT test and name the error, except for
// explicitly allow-listed jsdom emulator limits.
//
// All four cases below were GREEN before this change:
//   - a thrown event-handler exception: jsdom's own reportException
//     machinery swallows it (window "error" + virtualConsole "jsdomError"),
//     and dom.window.close() returned normally.
//   - a thrown timer-callback exception: same swallowing path.
//   - any jsdom "Not implemented" message: nothing checked virtualConsole
//     "jsdomError" messages at all, allow-listed or not.
// The fourth case, an unhandled promise rejection, was already RED before
// this change (proven below by running it as a bare `node` child process,
// unrelated to loadDOMWithScript's own bookkeeping) — Node's own realm
// treats it as a real unhandled rejection because jsdom evaluates script in
// a genuine `vm` context on the same isolate. It is pinned here so a future
// change (e.g. `--unhandled-rejections=warn`) cannot silently reopen it.
import test from "node:test";
import assert from "node:assert/strict";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";
import { loadDOMWithScript, ALLOWED_JSDOM_ERROR_MESSAGES, isAllowedJsdomErrorMessage } from "./_helpers.mjs";

const PAGE = "<!doctype html><html><head></head><body></body></html>";

test("an event-handler exception fails the test via dom.window.close()", async () => {
  const dom = await loadDOMWithScript(
    'document.addEventListener("click", () => { throw new Error("handler boom"); });' +
      "document.dispatchEvent(new Event('click'));",
    { html: PAGE }
  );
  assert.throws(
    () => dom.window.close(),
    /handler boom/,
    "an exception thrown inside an event handler must fail the test and name it"
  );
});

test("a timer-callback exception fails the test via dom.window.close()", async () => {
  const dom = await loadDOMWithScript('setTimeout(() => { throw new Error("timer boom"); }, 0);', {
    html: PAGE,
  });
  // Let the timer fire before we assert.
  await new Promise((resolve) => dom.window.setTimeout(resolve, 20));
  assert.throws(
    () => dom.window.close(),
    /timer boom/,
    "an exception thrown inside a timer callback must fail the test and name it"
  );
});

test("an allow-listed jsdom emulator-limit message does not fail the test", async () => {
  const [allowedMessage] = ALLOWED_JSDOM_ERROR_MESSAGES.keys();
  assert.ok(allowedMessage, "the allowlist must not be empty for this assertion to mean anything");
  // Genuinely trigger it — production's retry path calls requestSubmit(),
  // which jsdom has never implemented.
  const dom = await loadDOMWithScript(
    "document.body.appendChild(document.createElement('form')).requestSubmit();",
    { html: PAGE }
  );
  assert.doesNotThrow(() => dom.window.close(), "an exact allow-listed jsdom message must not fail the test");
});

test("a real, different 'Not implemented' jsdom message still fails the test", async () => {
  // window.alert() is a distinct jsdom emulator limit from the allow-listed
  // requestSubmit() one, and is not itself allow-listed.
  const dom = await loadDOMWithScript("window.alert('hi');", { html: PAGE });
  assert.throws(
    () => dom.window.close(),
    /Not implemented: Window's alert\(\) method/,
    "a non-allow-listed jsdom message must fail the test and name it"
  );
});

test("the allowlist match is exact: one word off from the allow-listed message is not allowed", () => {
  const [allowedMessage] = ALLOWED_JSDOM_ERROR_MESSAGES.keys();
  const almostAllowedMessage = allowedMessage.replace("requestSubmit()", "requestSubmitX()");
  assert.notEqual(almostAllowedMessage, allowedMessage, "the near-miss message must actually differ");
  assert.equal(isAllowedJsdomErrorMessage(allowedMessage), true, "the exact allow-listed message is allowed");
  assert.equal(
    isAllowedJsdomErrorMessage(almostAllowedMessage),
    false,
    "a one-word-different message is not allowed — the match is exact, not a class/regex match"
  );
});

test("an unhandled promise rejection already fails the process (Node's own detection, no harness help)", () => {
  const fixture = fileURLToPath(new URL("./_fixtures/unhandled-rejection-repro.mjs", import.meta.url));
  const result = spawnSync(process.execPath, [fixture], { encoding: "utf8" });
  assert.notEqual(result.status, 0, "an unhandled rejection inside jsdom-evaluated script must crash the process");
  assert.match(result.stderr, /fixture: rejection boom/, "the crash must name the rejection's error message");
});

test("a handler exception fails the test even when it never calls dom.window.close()", () => {
  // Run as a child `node --test` process: the fixture test must itself go
  // red, so it cannot live inline here without permanently failing this
  // file. The afterEach hook in _helpers.mjs — not an explicit close() call
  // — is what must catch it.
  const fixture = fileURLToPath(
    new URL("./_fixtures/never-closed-handler-throw-repro.mjs", import.meta.url)
  );
  // Strip NODE_TEST_CONTEXT/NODE_TEST_WORKER_ID: when this file itself runs
  // under `node --test`, Node sets them so a nested `node --test` child
  // reports through the parent's IPC channel instead of its own exit code,
  // which would make this assertion pass regardless of the fixture's result.
  const childEnv = { ...process.env };
  delete childEnv.NODE_TEST_CONTEXT;
  delete childEnv.NODE_TEST_WORKER_ID;
  const result = spawnSync(process.execPath, ["--test", fixture], { encoding: "utf8", env: childEnv });
  assert.notEqual(
    result.status,
    0,
    "a handler exception must fail the fixture's test even though it never calls dom.window.close()"
  );
  assert.match(
    result.stdout + result.stderr,
    /never-closed handler boom/,
    "the automatic afterEach check must name the error"
  );
});
