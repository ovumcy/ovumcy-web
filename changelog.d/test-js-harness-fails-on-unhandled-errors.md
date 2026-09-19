none

Test-only: the JS unit-test harness (`web/src/js/__tests__/_helpers.mjs`) used to ignore any
error jsdom surfaced from inside its window — an exception thrown by an event handler or a timer
callback — so a test could pass while the code under test threw. `loadDOMWithScript` now records
every such error and makes `dom.window.close()` (already called by every test) throw and name it,
except for an explicit, exact-message allowlist of known jsdom emulator limits (currently just
`HTMLFormElement.requestSubmit()`, which jsdom has never implemented). No production behavior
changes.
