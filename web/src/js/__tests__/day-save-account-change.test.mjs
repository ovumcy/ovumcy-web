// A day form names the account its page was rendered for, and the server
// refuses a read or a write from it once another account holds the session.
// Two things follow for the owner of the open tab:
//
// - a save-conflict choice (keep mine, take theirs) re-reads the stored day; when
//   that read is refused for another account the tab says so, rather than
//   "couldn't save, try again", which no retry could make true;
// - once the region says the account changed, an answer to a request made
//   before it (the save's own feedback, a failure) does not write over it,
//   whichever lands last. The next save of the form ends that hold.

import test from "node:test";
import assert from "node:assert/strict";
import { readFileSync, readdirSync } from "node:fs";
import { fileURLToPath } from "node:url";
import path from "node:path";
import { readAppBundle, loadDOMWithScript } from "./_helpers.mjs";

const REPO_ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..", "..", "..", "..");
const TEMPLATES_DIR = path.join(REPO_ROOT, "internal", "templates");

const APP_BUNDLE = readAppBundle();
const TODAY = "2026-08-12";
const ACCOUNT_BINDING = "unit-test-account-binding";
const OPEN_ACCOUNT_CHANGED_KEY = "daylog.open_account_changed";
const OPEN_REFUSED_TEXT = "This day can't be opened: you're now signed in to a different account. Reload the page.";
const SERVER_OPEN_REFUSED_TEXT = "Ce jour ne peut pas être ouvert : un autre compte est connecté.";
const FAILED_TEXT = "Couldn't save. Your entry is still here.";
const CONFLICT_KEY = "daylog.save_conflict";

const CONFLICT_FRAGMENT = `<div class="status-error" data-flash-key="${CONFLICT_KEY}" data-flash-status="error">Changed on another device.</div>`;
// What the server answers a read carrying HX-Request and the binding of another
// account: the status fragment of the day form page refusal.
const OPEN_REFUSED_FRAGMENT = `<div class="status-error" data-flash-key="${OPEN_ACCOUNT_CHANGED_KEY}" data-flash-status="error">${SERVER_OPEN_REFUSED_TEXT}</div>`;
// The same refusal without HX-Request: the JSON envelope.
const OPEN_REFUSED_ENVELOPE = JSON.stringify({
  error: "day form page account changed",
  error_detail: { key: "day form page account changed", category: "conflict", target: "global" },
});

function journalForm({ notes = "rendered" } = {}) {
  return `<form
      hx-put="/api/v1/days/${TODAY}"
      data-save-feedback
      data-dashboard-save-form
      data-dashboard-date="${TODAY}"
      data-today-entry-exists="true"
      data-autosave-saving="Saving..."
      data-autosave-saved="Saved"
      data-day-save-failed-text="${FAILED_TEXT}"
      data-day-save-retry-label="Try again"
      data-day-open-refused-text="${OPEN_REFUSED_TEXT}"
      data-day-conflict-refresh-url="/dashboard"
      data-day-conflict-keep-label="Keep mine"
      data-day-conflict-take-label="Take theirs">
      <input type="hidden" name="csrf_token" value="unit-test-token">
      <input type="hidden" name="day_form_account" value="${ACCOUNT_BINDING}">
      <input type="radio" name="pregnancy_test" value="positive">
      <input type="radio" name="pregnancy_test" value="none" checked hidden>
      <textarea id="today-notes" name="notes" data-dashboard-notes>${notes}</textarea>
      <div id="save-status" class="save-status" aria-live="polite"></div>
      <div data-dashboard-autosave-indicator data-autosave-state="idle"></div>
    </form>`;
}

function dashboardPage() {
  return `<!doctype html><html><head><meta name="csrf-token" content="unit-test-token"></head><body>
  <div data-dashboard-status-header><p data-dashboard-reminder-banner>Header</p></div>
  <div data-dashboard-editor>${journalForm()}</div></body></html>`;
}

function calendarPage() {
  return `<!doctype html><html><head><meta name="csrf-token" content="unit-test-token"></head><body>
  <div id="day-editor">
    <form
      hx-put="/api/v1/days/${TODAY}"
      hx-target="#calendar-save-status"
      data-save-feedback
      data-day-editor-form
      data-day-editor-date="${TODAY}"
      data-day-save-failed-text="${FAILED_TEXT}"
      data-day-save-retry-label="Try again"
      data-day-conflict-refresh-url="/calendar/day/${TODAY}?mode=edit"
      data-day-conflict-keep-label="Keep mine"
      data-day-conflict-take-label="Take theirs">
      <input type="hidden" name="csrf_token" value="unit-test-token">
      <input type="hidden" name="day_form_account" value="${ACCOUNT_BINDING}">
      <textarea id="calendar-notes" name="notes">mine</textarea>
      <button type="submit" data-save-button>Save</button>
      <div id="calendar-save-status" class="save-status" aria-live="polite"></div>
    </form>
    <form
      hx-post="/api/v1/days/${TODAY}/cycle-start?source=calendar"
      hx-target="#calendar-save-status"
      data-cycle-start-confirm-form
      data-day-cycle-start-form>
      <button type="submit">Start</button>
    </form>
  </div></body></html>`;
}

function response(status, text) {
  return { ok: status < 300, status, headers: { get: () => null }, text: () => Promise.resolve(text) };
}

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

// Every request is held until the test answers it, in the order it chooses.
function heldFetch(window, calls) {
  window.fetch = (url, init = {}) => {
    const held = deferred();
    calls.push({ url: String(url), init, method: init.method || "GET", answer: held.resolve, fail: held.reject });
    return held.promise;
  };
}

// PATCHes answer from `patchAnswers` in order; every GET answers `getAnswer`.
function scriptedFetch(patchAnswers, getAnswer) {
  return (window, calls) => {
    window.fetch = (url, init = {}) => {
      calls.push({ url: String(url), init, method: init.method || "GET" });
      if ((init.method || "GET") === "GET") {
        return Promise.resolve(getAnswer);
      }
      return Promise.resolve(patchAnswers.shift() || response(200, ""));
    };
  };
}

async function load(html, install, url) {
  const calls = [];
  const dom = await loadDOMWithScript(APP_BUNDLE, { html, url, beforeRun: (window) => install(window, calls) });
  return { dom, calls };
}

function settle() {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

async function settleAll() {
  for (let round = 0; round < 4; round++) {
    await settle();
  }
}

async function typeAndFlush(window, value) {
  const notes = window.document.querySelector("#today-notes");
  notes.value = value;
  notes.dispatchEvent(new window.Event("input", { bubbles: true }));
  window.dispatchEvent(new window.Event("pagehide"));
  await settle();
}

function choose(window, choice) {
  window.document
    .querySelector(`[data-day-conflict-choice='${choice}']`)
    .dispatchEvent(new window.MouseEvent("click", { bubbles: true, cancelable: true }));
}

function fireOnCalendarForm(window, name, detail) {
  const form = window.document.querySelector("[data-day-editor-form]");
  form.dispatchEvent(new window.CustomEvent(name, { detail, bubbles: true }));
}

function assertAccountChanged(region, text) {
  const notice = region.querySelector("[data-day-save-failed]");
  assert.ok(notice, "the region carries a notice");
  assert.equal(
    notice.getAttribute("data-day-save-failed"),
    "account-changed",
    "the notice is the account change, not a save that did not land"
  );
  const message = notice.querySelector("[data-notice-key]");
  assert.equal(message.getAttribute("data-notice-key"), OPEN_ACCOUNT_CHANGED_KEY, "the notice says the account changed");
  assert.equal(message.textContent, text);
  assert.equal(notice.querySelector("[data-day-save-retry]"), null, "no retry: none could land");
  assert.equal(notice.querySelector("[data-day-conflict-choice]"), null, "the choice is withdrawn with the day it needed");
  assert.equal(region.querySelectorAll(".status-ok").length, 0, "no success beside it");
}

for (const choice of ["mine", "theirs"]) {
  test(`a dashboard conflict choice (${choice}) refused for another account shows the account-changed notice`, async () => {
    const { dom, calls } = await load(
      dashboardPage(),
      scriptedFetch([response(409, CONFLICT_FRAGMENT)], response(409, OPEN_REFUSED_FRAGMENT)),
      "https://ovumcy.test/dashboard"
    );
    try {
      const document = dom.window.document;
      await typeAndFlush(dom.window, "mine");
      assert.ok(document.querySelector("#save-status [data-day-save-conflict]"), "the save met a conflict first");

      choose(dom.window, choice);
      await settleAll();

      const read = calls.find((call) => call.method === "GET");
      assert.equal(read.url, "/dashboard");
      assert.equal(read.init.headers["X-Ovumcy-Day-Form-Account"], ACCOUNT_BINDING, "the read names the page's account");
      assert.equal(read.init.headers["HX-Request"], "true", "so the refusal comes back as the status fragment");
      assertAccountChanged(document.querySelector("#save-status"), OPEN_REFUSED_TEXT);
      assert.equal(calls.filter((call) => call.method === "PATCH").length, 1, "nothing more is sent");
      assert.equal(document.querySelector("#today-notes").value, "mine", "the entry stays in the form");
    } finally {
      dom.window.close();
    }
  });

  test(`a calendar conflict choice (${choice}) refused for another account shows the server's account-changed notice`, async () => {
    const { dom, calls } = await load(calendarPage(), scriptedFetch([], response(409, OPEN_REFUSED_FRAGMENT)));
    try {
      const document = dom.window.document;
      fireOnCalendarForm(dom.window, "htmx:responseError", {
        target: document.getElementById("calendar-save-status"),
        xhr: { status: 409, responseText: CONFLICT_FRAGMENT },
      });
      assert.ok(document.querySelector("#calendar-save-status [data-day-save-conflict]"), "the save met a conflict first");

      choose(dom.window, choice);
      await settleAll();

      const read = calls.find((call) => call.method === "GET");
      assert.equal(read.url, `/calendar/day/${TODAY}?mode=edit`);
      assert.equal(read.init.headers["X-Ovumcy-Day-Form-Account"], ACCOUNT_BINDING);
      assertAccountChanged(document.querySelector("#calendar-save-status"), SERVER_OPEN_REFUSED_TEXT);
      assert.equal(document.querySelector("#calendar-notes").value, "mine", "the entry stays in the form");
    } finally {
      dom.window.close();
    }
  });
}

test("a conflict choice refused with the JSON envelope is read as the account change too", async () => {
  const { dom } = await load(
    dashboardPage(),
    scriptedFetch([response(409, CONFLICT_FRAGMENT)], response(409, OPEN_REFUSED_ENVELOPE)),
    "https://ovumcy.test/dashboard"
  );
  try {
    await typeAndFlush(dom.window, "mine");
    choose(dom.window, "mine");
    await settleAll();
    assertAccountChanged(dom.window.document.querySelector("#save-status"), OPEN_REFUSED_TEXT);
  } finally {
    dom.window.close();
  }
});

// The controls: a read that fails for any other reason still says the save did
// not land and offers the retry.
for (const [label, answer] of [
  ["a server error", response(500, "")],
  ["a 409 naming another refusal", response(409, CONFLICT_FRAGMENT)],
]) {
  test(`a conflict choice whose read fails with ${label} still offers the retry`, async () => {
    const { dom } = await load(
      dashboardPage(),
      scriptedFetch([response(409, CONFLICT_FRAGMENT)], answer),
      "https://ovumcy.test/dashboard"
    );
    try {
      await typeAndFlush(dom.window, "mine");
      choose(dom.window, "mine");
      await settleAll();
      const notice = dom.window.document.querySelector("#save-status [data-day-save-failed]");
      assert.equal(notice.getAttribute("data-day-save-failed"), "unreachable");
      assert.ok(notice.querySelector("[data-day-save-retry]"), "the retry is offered");
    } finally {
      dom.window.close();
    }
  });
}

// A saved pregnancy result fetches the dashboard again (the header refresh)
// while the save's own feedback is still being read: the two answers race for
// the region.
async function savePregnancyResult(window, calls) {
  const radio = window.document.querySelector("input[name='pregnancy_test'][value='positive']");
  radio.checked = true;
  radio.dispatchEvent(new window.Event("change", { bubbles: true }));
  window.dispatchEvent(new window.Event("pagehide"));
  await settle();
  const save = calls.find((call) => call.method === "PATCH");
  const feedback = deferred();
  save.answer({ ok: true, status: 200, headers: { get: () => null }, text: () => feedback.promise });
  await settleAll();
  const refresh = calls.find((call) => call.method === "GET");
  assert.ok(refresh, "the save started the header refresh");
  return { feedback, refresh };
}

const SAVE_FEEDBACK = '<div class="status-ok">Saved. Predictions are paused.</div>';

for (const order of ["refusal first", "feedback first"]) {
  test(`the account-changed notice outlasts the save's own feedback (${order})`, async () => {
    const { dom, calls } = await load(dashboardPage(), heldFetch, "https://ovumcy.test/dashboard");
    try {
      const { feedback, refresh } = await savePregnancyResult(dom.window, calls);
      const refuse = () => refresh.answer(response(409, OPEN_REFUSED_ENVELOPE));
      const answerFeedback = () => feedback.resolve(SAVE_FEEDBACK);
      if (order === "refusal first") {
        refuse();
        await settleAll();
        answerFeedback();
      } else {
        answerFeedback();
        await settleAll();
        refuse();
      }
      await settleAll();
      assertAccountChanged(dom.window.document.querySelector("#save-status"), OPEN_REFUSED_TEXT);
    } finally {
      dom.window.close();
    }
  });
}

test("a failure of a save already on the wire does not write over the account-changed notice", async () => {
  const { dom, calls } = await load(dashboardPage(), heldFetch, "https://ovumcy.test/dashboard");
  try {
    const { feedback, refresh } = await savePregnancyResult(dom.window, calls);
    feedback.resolve("");
    await settleAll();
    await typeAndFlush(dom.window, "typed while the refresh was out");
    const second = calls.filter((call) => call.method === "PATCH")[1];
    assert.ok(second, "a second save went out before the refresh came back");

    refresh.answer(response(409, OPEN_REFUSED_ENVELOPE));
    await settleAll();
    second.fail(new TypeError("network down"));
    await settleAll();

    assertAccountChanged(dom.window.document.querySelector("#save-status"), OPEN_REFUSED_TEXT);
    assert.equal(dom.window.document.querySelector("#today-notes").value, "typed while the refresh was out");
  } finally {
    dom.window.close();
  }
});

// The hold lasts until the next save of the form starts. The calendar's saves
// go through htmx, so the start is its beforeRequest.
test("on the calendar the account-changed notice holds until the next Save starts", async () => {
  const { dom } = await load(calendarPage(), scriptedFetch([], response(409, OPEN_REFUSED_FRAGMENT)));
  try {
    const document = dom.window.document;
    const region = document.getElementById("calendar-save-status");
    fireOnCalendarForm(dom.window, "htmx:responseError", {
      target: region,
      xhr: { status: 409, responseText: CONFLICT_FRAGMENT },
    });
    choose(dom.window, "mine");
    await settleAll();
    assertAccountChanged(region, SERVER_OPEN_REFUSED_TEXT);

    fireOnCalendarForm(dom.window, "htmx:sendError", { xhr: {} });
    assertAccountChanged(region, SERVER_OPEN_REFUSED_TEXT);

    fireOnCalendarForm(dom.window, "htmx:beforeRequest", { target: region });
    assert.equal(region.querySelector("[data-day-save-failed]"), null, "the new Save takes the region");
    fireOnCalendarForm(dom.window, "htmx:sendError", { xhr: {} });
    const notice = region.querySelector("[data-day-save-failed]");
    assert.equal(notice.getAttribute("data-day-save-failed"), "unreachable", "its own outcome is shown");
    assert.ok(notice.querySelector("[data-day-save-retry]"));
  } finally {
    dom.window.close();
  }
});

// htmx writes a successful answer into its target itself. This mirrors it: the
// swap happens only when htmx:beforeSwap leaves shouldSwap set, then afterSwap.
const SAVED_FRAGMENT = '<div class="status-ok">Day saved.</div>';

function htmxSwap(window, source, region, xhr) {
  const detail = { target: region, xhr, shouldSwap: xhr.status < 400 };
  source.dispatchEvent(new window.CustomEvent("htmx:beforeSwap", { detail, bubbles: true }));
  if (detail.shouldSwap) {
    region.innerHTML = xhr.responseText;
    source.dispatchEvent(new window.CustomEvent("htmx:afterSwap", { detail: { target: region, xhr }, bubbles: true }));
  }
  return detail.shouldSwap;
}

async function calendarNoticeAfterAnEarlierSave(window) {
  const region = window.document.getElementById("calendar-save-status");
  fireOnCalendarForm(window, "htmx:responseError", { target: region, xhr: { status: 409, responseText: CONFLICT_FRAGMENT } });
  // The owner presses Save again while the conflict choice is on screen: that
  // save is on the wire, and its answer is about the session before the switch.
  fireOnCalendarForm(window, "htmx:beforeRequest", { target: region });
  choose(window, "mine");
  await settleAll();
  assertAccountChanged(region, SERVER_OPEN_REFUSED_TEXT);
  return region;
}

test("on the calendar a Save that landed before the account changed is not swapped over the notice", async () => {
  const { dom } = await load(calendarPage(), scriptedFetch([], response(409, OPEN_REFUSED_FRAGMENT)));
  try {
    const form = dom.window.document.querySelector("[data-day-editor-form]");
    const region = await calendarNoticeAfterAnEarlierSave(dom.window);

    const swapped = htmxSwap(dom.window, form, region, { status: 200, responseText: SAVED_FRAGMENT });
    assert.equal(swapped, false, "htmx is told not to swap the earlier save's success");
    assertAccountChanged(region, SERVER_OPEN_REFUSED_TEXT);

    // The control: the next Save's own answer is swapped in.
    fireOnCalendarForm(dom.window, "htmx:beforeRequest", { target: region });
    assert.equal(htmxSwap(dom.window, form, region, { status: 200, responseText: SAVED_FRAGMENT }), true);
    assert.ok(region.querySelector(".status-ok").textContent.includes("Day saved."), "the new Save's success is shown");
  } finally {
    dom.window.close();
  }
});

test("on the calendar the cycle-start form's earlier answers do not replace the notice either", async () => {
  const { dom } = await load(calendarPage(), scriptedFetch([], response(409, OPEN_REFUSED_FRAGMENT)));
  try {
    const cycleStart = dom.window.document.querySelector("[data-day-cycle-start-form]");
    const region = await calendarNoticeAfterAnEarlierSave(dom.window);

    cycleStart.dispatchEvent(
      new dom.window.CustomEvent("htmx:responseError", { detail: { target: region, xhr: { status: 500, responseText: "" } }, bubbles: true })
    );
    assertAccountChanged(region, SERVER_OPEN_REFUSED_TEXT);
    assert.equal(htmxSwap(dom.window, cycleStart, region, { status: 200, responseText: SAVED_FRAGMENT }), false);
    assertAccountChanged(region, SERVER_OPEN_REFUSED_TEXT);

    // The control: a cycle-start sent after the notice answers in the region.
    cycleStart.dispatchEvent(new dom.window.CustomEvent("htmx:beforeRequest", { detail: { target: region }, bubbles: true }));
    assert.equal(htmxSwap(dom.window, cycleStart, region, { status: 200, responseText: SAVED_FRAGMENT }), true);
    assert.ok(region.querySelector(".status-ok"), "its own answer is shown");
  } finally {
    dom.window.close();
  }
});

// The success the notice replaced still has its clear pending; when it runs, it
// finds the success gone and must not empty the region of the notice.
test("the clear of the success the notice replaced leaves the notice", async () => {
  const { dom, calls } = await load(dashboardPage(), heldFetch, "https://ovumcy.test/dashboard");
  try {
    const parked = [];
    const realSetTimeout = dom.window.setTimeout.bind(dom.window);
    dom.window.setTimeout = (fn, delay, ...rest) => {
      if (delay === 5200 || delay === 220) {
        parked.push(fn);
        return 0;
      }
      return realSetTimeout(fn, delay, ...rest);
    };
    const { feedback, refresh } = await savePregnancyResult(dom.window, calls);
    feedback.resolve(SAVE_FEEDBACK);
    await settleAll();
    assert.ok(parked.length > 0, "the success scheduled its clear");
    refresh.answer(response(409, OPEN_REFUSED_ENVELOPE));
    await settleAll();
    while (parked.length) {
      parked.shift()();
    }
    assertAccountChanged(dom.window.document.querySelector("#save-status"), OPEN_REFUSED_TEXT);
  } finally {
    dom.window.close();
  }
});

// The refusal's JSON envelope carries a key and no text, so the notice's copy
// has to be on the form. Both day forms render it; the calendar's is read from
// the shipped template, so a form that stops carrying it fails here.
const OPEN_REFUSED_ATTRIBUTE = 'data-day-open-refused-text="{{t .Messages "daylog.open_account_changed"}}"';

function templateFiles(dir) {
  return readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      return templateFiles(full);
    }
    return entry.name.endsWith(".html") ? [full] : [];
  });
}

function shippedDayFormTags() {
  return templateFiles(TEMPLATES_DIR).flatMap((file) =>
    (readFileSync(file, "utf8").match(/<form\b[^>]*>/g) || [])
      .filter((tag) => /\sdata-(day-editor-form|dashboard-save-form)\s/.test(tag))
      .map((tag) => ({ file: path.basename(file), tag }))
  );
}

test("every day form in the shipped templates carries the account-changed copy", () => {
  const forms = shippedDayFormTags();
  assert.deepEqual(
    forms.map((form) => form.file).sort(),
    ["dashboard.html", "day_editor_partial.html"],
    "the calendar editor and the dashboard journal are found"
  );
  for (const form of forms) {
    assert.ok(form.tag.includes(OPEN_REFUSED_ATTRIBUTE), `${form.file}: the day form carries the account-changed copy`);
  }
});

function shippedCalendarFormTag() {
  const messages = JSON.parse(readFileSync(path.join(REPO_ROOT, "internal", "i18n", "locales", "en.json"), "utf8"));
  const escape = (text) => text.replace(/&/g, "&amp;").replace(/"/g, "&quot;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
  const form = shippedDayFormTags().find((entry) => entry.file === "day_editor_partial.html");
  return form.tag
    .replace(/\{\{t \.Messages "([^"]+)"\}\}/g, (_, key) => escape(messages[key]))
    .replace(/\{\{\.DateString\}\}/g, TODAY);
}

test("a calendar conflict choice refused with the JSON envelope shows the form's account-changed copy", async () => {
  const html = calendarPage().replace(/<form\b[^>]*data-day-editor-form[^>]*>/, () => shippedCalendarFormTag());
  const { dom } = await load(html, scriptedFetch([], response(409, OPEN_REFUSED_ENVELOPE)));
  try {
    const document = dom.window.document;
    const region = document.getElementById("calendar-save-status");
    fireOnCalendarForm(dom.window, "htmx:responseError", { target: region, xhr: { status: 409, responseText: CONFLICT_FRAGMENT } });
    choose(dom.window, "mine");
    await settleAll();
    assertAccountChanged(region, OPEN_REFUSED_TEXT);
    assert.equal(document.querySelector("#calendar-notes").value, "mine", "the entry stays in the form");
  } finally {
    dom.window.close();
  }
});
