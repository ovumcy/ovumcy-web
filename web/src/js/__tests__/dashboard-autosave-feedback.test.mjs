// A day save answers with the server's own sentence about the day, and after a
// positive pregnancy test that sentence carries medical-safety guidance: the
// predictions are paused, and bleeding, pain or dizziness mean seeking care
// promptly. The calendar editor shows it because htmx swaps the fragment into
// its status region. The dashboard journal saves through fetch instead, so the
// sentence reaches the page only if the autosave reads the response and puts
// it there — as text, never as markup from the response.

import test from "node:test";
import assert from "node:assert/strict";
import { readAppBundle, loadDOMWithScript } from "./_helpers.mjs";

const APP_BUNDLE = readAppBundle();

const TODAY = "2026-08-12";
const PAUSED_SENTENCE =
  "Saved. Cycle predictions are paused after a positive pregnancy test. If you experience bleeding, pain, or dizziness, seek medical care promptly.";

function dashboardPage() {
  return `<!doctype html><html><head><meta name="csrf-token" content="unit-test-token"></head><body>
  <div data-dashboard-editor>
    <form
      hx-put="/api/v1/days/${TODAY}"
      hx-target="#save-status"
      hx-swap="innerHTML"
      data-save-feedback
      data-dashboard-save-form
      data-dashboard-date="${TODAY}"
      data-today-entry-exists="false"
      data-autosave-saving="Saving..."
      data-autosave-saved="Saved"
      data-day-save-failed-text="Couldn't save. Your entry is still here."
      data-day-save-retry-label="Try again">
      <input type="hidden" name="csrf_token" value="unit-test-token">
      <input type="radio" name="pregnancy_test" value="negative">
      <input type="radio" name="pregnancy_test" value="positive">
      <div id="save-status" class="save-status" aria-live="polite"></div>
      <div class="dashboard-autosave-indicator" data-dashboard-autosave-indicator data-autosave-state="idle" aria-live="polite"></div>
    </form>
  </div>
</body></html>`;
}

// The fragment the server sends for an htmx day save (httpx's dismissible
// success markup): the sentence arrives HTML-escaped inside .toast-message.
function statusOKFragment(escapedMessage) {
  return (
    '<div class="status-ok"><div class="toast-body"><span class="toast-message-wrap">' +
    '<span class="toast-icon" aria-hidden="true">✓</span>' +
    `<span class="toast-message">${escapedMessage}</span></span>` +
    '<button type="button" class="toast-close" data-dismiss-status aria-label="Close">×</button>' +
    "</div></div>"
  );
}

function escapeHTML(text) {
  return text
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&#34;")
    .replace(/'/g, "&#39;");
}

async function loadDashboard(responseBody) {
  const calls = [];
  const dom = await loadDOMWithScript(APP_BUNDLE, {
    html: dashboardPage(),
    beforeRun: (window) => {
      window.fetch = (url, init) => {
        calls.push({ url: String(url), init: init || {} });
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: { get: () => null },
          text: () => Promise.resolve(responseBody),
        });
      };
    },
  });
  return { dom, calls };
}

// Mark the positive result and let the runner send it. `pagehide` reaches the
// same runner the 2 s debounce does, without sitting the debounce out.
async function savePositiveTest(window) {
  const positive = window.document.querySelector("input[name='pregnancy_test'][value='positive']");
  positive.checked = true;
  positive.dispatchEvent(new window.Event("change", { bubbles: true }));
  window.dispatchEvent(new window.Event("pagehide"));
  await new Promise((resolve) => setTimeout(resolve, 0));
}

function saveStatus(window) {
  return window.document.querySelector("#save-status");
}

test("a positive-test save shows the server's safety sentence in the dashboard status region", async () => {
  const { dom, calls } = await loadDashboard(statusOKFragment(escapeHTML(PAUSED_SENTENCE)));
  try {
    await savePositiveTest(dom.window);

    assert.equal(calls.length, 1, "the positive result is saved");
    assert.ok(String(calls[0].init.body).includes("pregnancy_test=positive"));
    assert.equal(
      dom.window.document.querySelector("[data-dashboard-autosave-indicator]").getAttribute("data-autosave-state"),
      "saved"
    );

    const message = saveStatus(dom.window).querySelector(".status-ok .toast-message");
    assert.ok(message, "the save's feedback must land in the dashboard's status region");
    assert.equal(
      message.textContent,
      PAUSED_SENTENCE,
      "the red-flag guidance the server composed is shown word for word"
    );
    assert.ok(
      saveStatus(dom.window).querySelector(".status-ok [data-dismiss-status]"),
      "the shown status is the same dismissible one the calendar editor renders"
    );
  } finally {
    dom.window.close();
  }
});

test("a save whose response carries no feedback adds nothing to the status region", async () => {
  for (const body of ["", '{"date":"2026-08-12"}', '<div class="status-ok"><span class="toast-message">  </span></div>']) {
    const { dom, calls } = await loadDashboard(body);
    try {
      await savePositiveTest(dom.window);

      assert.equal(calls.length, 1);
      assert.equal(
        saveStatus(dom.window).childNodes.length,
        0,
        `a response without a sentence must not add anything (body ${JSON.stringify(body)})`
      );
    } finally {
      dom.window.close();
    }
  }
});

test("markup in the feedback renders as text, never as elements", async () => {
  const escaped = 'Saved. <img src="x" data-injected="escaped">';
  const raw = 'Saved. <img src="x" data-injected="raw"><b data-injected="raw">bold</b>';
  const cases = [
    // What the server sends: the sentence escaped once, so it reads as characters.
    { body: statusOKFragment(escapeHTML(escaped)), text: escaped },
    // A regression that let unescaped markup into the fragment: only its text
    // may be adopted.
    { body: statusOKFragment(raw), text: "Saved. bold" },
  ];
  for (const { body, text } of cases) {
    const { dom } = await loadDashboard(body);
    try {
      await savePositiveTest(dom.window);

      const region = saveStatus(dom.window);
      assert.equal(region.querySelector(".status-ok .toast-message").textContent, text);
      assert.equal(region.querySelector("[data-injected]"), null, "no element from the response reaches the page");
      assert.equal(region.querySelector("img, b"), null);
    } finally {
      dom.window.close();
    }
  }
});

// --- The status's kind -------------------------------------------------------
//
// The server declares what kind of sentence it sent in data-status-kind, and the
// client decides by that marker alone, never by the words: a neutral line only
// says the day was saved, which the journal's own indicator already says; the
// pregnancy-pause line carries red-flag guidance and stays until dismissed, on
// the dashboard and in the calendar editor alike; a routine line clears itself.

const TOAST_VISIBLE_MS = 5200;
const TOAST_EXIT_MS = 220;
const SELF_CARE_SENTENCE = "Saved. Take care of yourself today.";

function kindFragment(escapedMessage, kind) {
  return statusOKFragment(escapedMessage).replace(
    '<div class="status-ok">',
    kind ? `<div class="status-ok" data-status-kind="${kind}">` : '<div class="status-ok">'
  );
}

// Every window timer is recorded and none runs on its own; elapseStatusClear
// runs only the status clear and its exit step, as the 5.2 s visible window and
// the exit animation would. The autosave's own debounce stays parked.
function installRecordedTimers(window) {
  const timers = [];
  window.setTimeout = (fn, delay) => {
    timers.push({ fn, delay: Number(delay) || 0, done: false });
    return timers.length;
  };
  window.clearTimeout = (id) => {
    if (timers[id - 1]) {
      timers[id - 1].done = true;
    }
  };
  window.__statusTimers = timers;
}

function elapseStatusClear(window) {
  let ran = 0;
  for (let round = 0; round < 5; round += 1) {
    const due = window.__statusTimers.filter(
      (timer) => !timer.done && (timer.delay === TOAST_VISIBLE_MS || timer.delay === TOAST_EXIT_MS)
    );
    if (due.length === 0) {
      break;
    }
    for (const timer of due) {
      timer.done = true;
      timer.fn();
      ran += 1;
    }
  }
  return ran;
}

async function loadDashboardWithTimers(responseBody) {
  const dom = await loadDOMWithScript(APP_BUNDLE, {
    html: dashboardPage(),
    beforeRun: (window) => {
      installRecordedTimers(window);
      window.fetch = () =>
        Promise.resolve({
          ok: true,
          status: 200,
          headers: { get: () => null },
          text: () => Promise.resolve(responseBody),
        });
    },
  });
  return dom;
}

test("a neutral save adds nothing to the dashboard status region", async () => {
  const dom = await loadDashboardWithTimers(kindFragment("Saved.", "neutral"));
  try {
    await savePositiveTest(dom.window);

    assert.equal(
      dom.window.document.querySelector("[data-dashboard-autosave-indicator]").getAttribute("data-autosave-state"),
      "saved",
      "the journal's own indicator reports the save"
    );
    assert.equal(
      saveStatus(dom.window).childNodes.length,
      0,
      "the neutral line repeats the indicator, so the dashboard does not show it"
    );
  } finally {
    dom.window.close();
  }
});

test("the pregnancy-pause sentence stays on the dashboard after the clear timer elapses", async () => {
  const dom = await loadDashboardWithTimers(kindFragment(escapeHTML(PAUSED_SENTENCE), "persistent"));
  try {
    await savePositiveTest(dom.window);
    elapseStatusClear(dom.window);

    const message = saveStatus(dom.window).querySelector(".status-ok .toast-message");
    assert.ok(message, "the safety sentence must survive the auto-clear window");
    assert.equal(message.textContent, PAUSED_SENTENCE);

    saveStatus(dom.window).querySelector("[data-dismiss-status]").click();
    assert.equal(saveStatus(dom.window).querySelector(".status-ok"), null, "the owner can still dismiss it");
  } finally {
    dom.window.close();
  }
});

test("a self-care line on the dashboard clears itself", async () => {
  const dom = await loadDashboardWithTimers(kindFragment(escapeHTML(SELF_CARE_SENTENCE), ""));
  try {
    await savePositiveTest(dom.window);
    assert.equal(
      saveStatus(dom.window).querySelector(".status-ok .toast-message").textContent,
      SELF_CARE_SENTENCE,
      "a routine line is shown"
    );

    assert.ok(elapseStatusClear(dom.window) > 0, "the clear was scheduled");
    assert.equal(saveStatus(dom.window).querySelector(".status-ok"), null, "a routine line clears on its timer");
  } finally {
    dom.window.close();
  }
});

const CALENDAR_PAGE = `<!doctype html><html><head></head><body>
  <form hx-put="/api/v1/days/2026-08-11" hx-target="#calendar-save-status" data-save-feedback data-day-editor-form data-day-editor-date="2026-08-11">
    <button type="submit" data-save-button>Save</button>
    <div id="calendar-save-status" class="save-status" aria-live="polite"></div>
  </form>
</body></html>`;

// The calendar editor's save is an htmx swap of the same fragment into its
// status region, cleared by the same shared scheduler on afterSwap.
async function swapIntoCalendarEditor(fragment) {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: CALENDAR_PAGE, beforeRun: installRecordedTimers });
  const target = dom.window.document.querySelector("#calendar-save-status");
  target.innerHTML = fragment;
  target.dispatchEvent(new dom.window.CustomEvent("htmx:afterSwap", { bubbles: true, detail: { target } }));
  target.dispatchEvent(new dom.window.CustomEvent("htmx:afterSettle", { bubbles: true, detail: { target } }));
  return { dom, target };
}

test("the calendar editor keeps a persistent status after the clear timer elapses", async () => {
  const { dom, target } = await swapIntoCalendarEditor(kindFragment(escapeHTML(PAUSED_SENTENCE), "persistent"));
  try {
    elapseStatusClear(dom.window);
    const message = target.querySelector(".status-ok .toast-message");
    assert.ok(message, "the safety sentence must survive the auto-clear window in the calendar editor");
    assert.equal(message.textContent, PAUSED_SENTENCE);
  } finally {
    dom.window.close();
  }
});

test("the calendar editor still shows the neutral line and clears a routine status", async () => {
  for (const kind of ["neutral", ""]) {
    const { dom, target } = await swapIntoCalendarEditor(kindFragment("Saved.", kind));
    try {
      assert.equal(target.querySelector(".status-ok .toast-message").textContent, "Saved.");
      assert.ok(elapseStatusClear(dom.window) > 0, "the clear was scheduled");
      assert.equal(target.querySelector(".status-ok"), null, `a ${kind || "routine"} status clears on its timer`);
    } finally {
      dom.window.close();
    }
  }
});

// --- The header refresh after a pregnancy result -----------------------------
//
// A changed pregnancy result also fetches the dashboard again and swaps the
// pregnancy-dependent blocks of the status header in place. That refresh and
// the save's own sentence travel separately, and either may land first; the
// sentence lives in the journal's status region, outside the swapped blocks, so
// it must be on the page — and stay until dismissed — whichever answer arrives
// last.

const STALE_BANNER = "Period likely today";
const PAUSED_BANNER = "Next period estimate paused";

function statusHeader(banner) {
  return `<section data-dashboard-shell>
    <section data-dashboard-status-header>
      <p data-dashboard-status-line aria-live="polite">${banner}</p>
      <div data-dashboard-cycle-ribbon></div>
      <p data-dashboard-reminder-banner>${banner}</p>
      <p data-dashboard-prediction-disclaimer>Not medical advice</p>
    </section>
  </section>`;
}

function deferred() {
  let resolve;
  const promise = new Promise((done) => {
    resolve = done;
  });
  return { promise, resolve };
}

async function settleRounds() {
  for (let round = 0; round < 4; round += 1) {
    await new Promise((resolve) => setTimeout(resolve, 0));
  }
}

async function loadDashboardWithHeaderRefresh() {
  const saveBody = deferred();
  const pageBody = deferred();
  const calls = [];
  const dom = await loadDOMWithScript(APP_BUNDLE, {
    html: dashboardPage().replace("<body>", `<body>${statusHeader(STALE_BANNER)}`),
    url: "https://ovumcy.test/dashboard",
    beforeRun: (window) => {
      installRecordedTimers(window);
      window.fetch = (url, init) => {
        const method = (init && init.method) || "GET";
        calls.push({ url: String(url), method });
        const body = method === "GET" ? pageBody : saveBody;
        return Promise.resolve({ ok: true, status: 200, headers: { get: () => null }, text: () => body.promise });
      };
    },
  });
  return { dom, calls, saveBody, pageBody };
}

for (const order of ["the refresh lands after the sentence", "the refresh lands before the sentence"]) {
  test(`the safety sentence survives the header refresh when ${order}`, async () => {
    const { dom, calls, saveBody, pageBody } = await loadDashboardWithHeaderRefresh();
    const document = dom.window.document;
    const sentence = () => saveStatus(dom.window).querySelector(".status-ok .toast-message");
    const freshPage = `<!doctype html><html><body>${statusHeader(PAUSED_BANNER)}</body></html>`;
    try {
      await savePositiveTest(dom.window);
      assert.deepEqual(
        calls.map((call) => call.method),
        ["PUT", "GET"],
        "the positive result is saved and the header is fetched again"
      );

      if (order === "the refresh lands after the sentence") {
        saveBody.resolve(kindFragment(escapeHTML(PAUSED_SENTENCE), "persistent"));
        await settleRounds();
        assert.ok(sentence(), "the sentence is shown before the refresh lands");
        pageBody.resolve(freshPage);
      } else {
        pageBody.resolve(freshPage);
        await settleRounds();
        assert.equal(
          document.querySelector("[data-dashboard-reminder-banner]").textContent,
          PAUSED_BANNER,
          "the header is refreshed before the sentence lands"
        );
        saveBody.resolve(kindFragment(escapeHTML(PAUSED_SENTENCE), "persistent"));
      }
      await settleRounds();

      assert.equal(
        document.querySelector("[data-dashboard-reminder-banner]").textContent,
        PAUSED_BANNER,
        "the refresh swapped the pregnancy-dependent blocks"
      );
      assert.ok(document.contains(saveStatus(dom.window)), "the journal's status region is still on the page");
      assert.ok(sentence(), "the safety sentence is still shown after the refresh");
      assert.equal(sentence().textContent, PAUSED_SENTENCE);

      elapseStatusClear(dom.window);
      assert.ok(sentence(), "the safety sentence outlasts the auto-clear window after the refresh");

      saveStatus(dom.window).querySelector("[data-dismiss-status]").click();
      assert.equal(saveStatus(dom.window).querySelector(".status-ok"), null, "the owner can still dismiss it");
    } finally {
      dom.window.close();
    }
  });
}

// --- A later save withdraws the earlier answer -------------------------------
//
// The status region reflects the LATEST successful save. A persistent sentence
// is cleared by no timer, so when the next save answers neutral — the owner
// corrected the test to negative, or pressed Undo — nothing but that save
// itself can take the "predictions are paused" sentence down. A routine or
// persistent answer replaces what is there, as before.

// Each fetch answers with the next body of the queue; the last one repeats.
async function loadDashboardWithAnswers(bodies, html) {
  const queue = bodies.slice();
  return loadDOMWithScript(APP_BUNDLE, {
    html: html || dashboardPage(),
    beforeRun: (window) => {
      installRecordedTimers(window);
      window.fetch = () => {
        const body = queue.length > 1 ? queue.shift() : queue[0];
        return Promise.resolve({
          ok: true,
          status: 200,
          headers: { get: () => null },
          text: () => Promise.resolve(body),
        });
      };
    },
  });
}

async function saveNegativeTest(window) {
  const negative = window.document.querySelector("input[name='pregnancy_test'][value='negative']");
  negative.checked = true;
  negative.dispatchEvent(new window.Event("change", { bubbles: true }));
  window.dispatchEvent(new window.Event("pagehide"));
  await new Promise((resolve) => setTimeout(resolve, 0));
}

test("a neutral save after a positive test withdraws the safety sentence", async () => {
  const dom = await loadDashboardWithAnswers([
    kindFragment(escapeHTML(PAUSED_SENTENCE), "persistent"),
    kindFragment("Saved.", "neutral"),
  ]);
  try {
    await savePositiveTest(dom.window);
    assert.ok(saveStatus(dom.window).querySelector(".status-ok"), "the safety sentence is shown after the positive test");

    await saveNegativeTest(dom.window);
    assert.equal(
      saveStatus(dom.window).querySelector(".status-ok"),
      null,
      "the corrected test answers neutral, so the earlier safety sentence must not outlive it"
    );
    assert.equal(saveStatus(dom.window).childNodes.length, 0, "and nothing new is shown in its place");
  } finally {
    dom.window.close();
  }
});

test("a neutral save withdraws a routine status and its pending clear", async () => {
  const dom = await loadDashboardWithAnswers([
    kindFragment(escapeHTML(SELF_CARE_SENTENCE), ""),
    kindFragment("Saved.", "neutral"),
  ]);
  try {
    await savePositiveTest(dom.window);
    assert.ok(saveStatus(dom.window).querySelector(".status-ok"), "the routine line is shown");
    const pending = () =>
      dom.window.__statusTimers.filter((timer) => !timer.done && timer.delay === TOAST_VISIBLE_MS).length;
    assert.equal(pending(), 1, "the routine line has a clear scheduled");

    await saveNegativeTest(dom.window);
    assert.equal(saveStatus(dom.window).childNodes.length, 0, "the earlier line is withdrawn");
    assert.equal(pending(), 0, "its clear timer is cancelled with it");
  } finally {
    dom.window.close();
  }
});

test("a neutral save withdraws a persistent status an htmx swap left in the region", async () => {
  const dom = await loadDashboardWithAnswers([kindFragment("Saved.", "neutral")]);
  try {
    // What an Enter-submit leaves behind: the swapped-in persistent fragment.
    saveStatus(dom.window).innerHTML = kindFragment(escapeHTML(PAUSED_SENTENCE), "persistent");
    assert.ok(saveStatus(dom.window).querySelector(".status-ok"));

    await savePositiveTest(dom.window);
    assert.equal(saveStatus(dom.window).querySelector(".status-ok"), null);
  } finally {
    dom.window.close();
  }
});

test("a later persistent answer replaces an earlier routine one", async () => {
  const dom = await loadDashboardWithAnswers([
    kindFragment(escapeHTML(SELF_CARE_SENTENCE), ""),
    kindFragment(escapeHTML(PAUSED_SENTENCE), "persistent"),
  ]);
  try {
    await saveNegativeTest(dom.window);
    await savePositiveTest(dom.window);
    const messages = saveStatus(dom.window).querySelectorAll(".status-ok .toast-message");
    assert.equal(messages.length, 1, "one status stands in the region");
    assert.equal(messages[0].textContent, PAUSED_SENTENCE);
  } finally {
    dom.window.close();
  }
});

test("Undo whose answer is neutral withdraws the safety sentence", async () => {
  const html = dashboardPage()
    .replace('data-today-entry-exists="false"', 'data-today-entry-exists="true" data-autosave-undo="Undo"')
    .replace('value="negative">', 'value="negative" checked>');
  const dom = await loadDashboardWithAnswers(
    [kindFragment(escapeHTML(PAUSED_SENTENCE), "persistent"), kindFragment("Saved.", "neutral")],
    html
  );
  try {
    await savePositiveTest(dom.window);
    assert.ok(saveStatus(dom.window).querySelector(".status-ok"), "the safety sentence is shown after the positive test");

    const undo = dom.window.document.querySelector("[data-dashboard-autosave-undo]");
    assert.ok(undo, "the save offers Undo");
    undo.click();
    await new Promise((resolve) => setTimeout(resolve, 0));
    await new Promise((resolve) => setTimeout(resolve, 0));

    assert.equal(
      dom.window.document.querySelector("input[name='pregnancy_test'][value='negative']").checked,
      true,
      "the undo restored the earlier answer"
    );
    assert.equal(
      saveStatus(dom.window).querySelector(".status-ok"),
      null,
      "the undone test answers neutral, so the safety sentence must not outlive it"
    );
  } finally {
    dom.window.close();
  }
});
