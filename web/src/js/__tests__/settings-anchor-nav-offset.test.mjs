import test from "node:test";
import assert from "node:assert/strict";

import { loadDOMWithScript, readAppBundle } from "./_helpers.mjs";

const APP_BUNDLE = readAppBundle();

// A settings deep link (/settings#settings-cycle, the section index, the
// privacy page's ledger link) scrolls the target card to the top of the
// viewport, where the sticky header and — from 640px up — the sticky section
// index sit. Each card's scroll-margin-top clears both, and the index's share
// of it is --settings-section-nav-height. The index wraps to as many rows as
// the locale and the width need, so the stylesheet's two-row figure is only a
// default: the script measures the index and publishes the real height on the
// stack. These cases pin that the published value follows the measured one,
// at load and again when the index re-wraps.
function settingsPage({ withNav = true } = {}) {
  return `<!doctype html>
<html lang="en"><head><meta name="csrf-token" content="test-token"></head>
<body>
  <header class="paper-header"><a href="/">Ovumcy</a></header>
  <section data-settings-sections>
    ${withNav ? `<nav class="settings-section-nav" data-settings-section-nav aria-label="Settings">
      <a href="#settings-cycle" class="settings-section-nav-link">Cycle</a>
      <a href="#settings-account" class="settings-section-nav-link">Account</a>
    </nav>` : ""}
    <details id="settings-cycle" class="card settings-section" data-settings-section open>
      <summary class="settings-section-summary"><h2>Cycle</h2></summary>
    </details>
    <details id="settings-account" class="card settings-section" data-settings-section open>
      <summary class="settings-section-summary"><h2>Account</h2></summary>
    </details>
  </section>
</body></html>`;
}

// jsdom performs no layout, so the index's height is whatever the test says it
// is; every other element reports 0, as it does in jsdom anyway.
function stubIndexHeight(window, initial, headerInitial = 0) {
  window.__testIndexHeight = initial;
  window.__testHeaderHeight = headerInitial;
  Object.defineProperty(window.HTMLElement.prototype, "offsetHeight", {
    configurable: true,
    get() {
      if (this.hasAttribute("data-settings-section-nav")) {
        return window.__testIndexHeight;
      }
      return this.classList.contains("paper-header") ? window.__testHeaderHeight : 0;
    },
  });
}

function publishedClearance(window) {
  return window.document
    .querySelector("[data-settings-sections]")
    .style.getPropertyValue("--sticky-header-clearance");
}

function stubResizeObserver(window) {
  const observers = [];
  window.ResizeObserver = class {
    constructor(callback) {
      this.callback = callback;
      this.targets = [];
      observers.push(this);
    }
    observe(target) {
      this.targets.push(target);
    }
    disconnect() {}
  };
  return observers;
}

function publishedHeight(window) {
  return window.document
    .querySelector("[data-settings-sections]")
    .style.getPropertyValue("--settings-section-nav-height");
}

test("the measured section-index height is published on the stack at load", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, {
    html: settingsPage(),
    url: "https://ovumcy.test/settings#settings-cycle",
    beforeRun(window) {
      stubIndexHeight(window, 143);
      stubResizeObserver(window);
    },
  });

  assert.equal(publishedHeight(dom.window), "143px");
  dom.window.close();
});

test("a re-wrapped section index republishes its new height", async () => {
  let observers = [];
  const dom = await loadDOMWithScript(APP_BUNDLE, {
    html: settingsPage(),
    url: "https://ovumcy.test/settings",
    beforeRun(window) {
      stubIndexHeight(window, 96);
      observers = stubResizeObserver(window);
    },
  });

  const index = dom.window.document.querySelector("[data-settings-section-nav]");
  const watching = observers.filter((observer) => observer.targets.includes(index));
  assert.equal(watching.length, 1, "expected exactly one observer on the section index");

  // Russian on a tablet: the index wraps to a third row.
  dom.window.__testIndexHeight = 139;
  watching[0].callback([]);
  assert.equal(publishedHeight(dom.window), "139px");
  dom.window.close();
});

test("without ResizeObserver a window resize republishes the height", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, {
    html: settingsPage(),
    url: "https://ovumcy.test/settings",
    beforeRun(window) {
      stubIndexHeight(window, 96);
      delete window.ResizeObserver;
    },
  });

  assert.equal(publishedHeight(dom.window), "96px");
  dom.window.__testIndexHeight = 52;
  dom.window.dispatchEvent(new dom.window.Event("resize"));
  assert.equal(publishedHeight(dom.window), "52px");
  dom.window.close();
});

test("an index that is not laid out keeps the stylesheet default", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, {
    html: settingsPage(),
    url: "https://ovumcy.test/settings",
    beforeRun(window) {
      stubIndexHeight(window, 0);
      stubResizeObserver(window);
    },
  });

  assert.equal(publishedHeight(dom.window), "");
  dom.window.close();
});

test("the measured sticky-header height plus the sliver is published at load", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, {
    html: settingsPage(),
    url: "https://ovumcy.test/settings#settings-cycle",
    beforeRun(window) {
      stubIndexHeight(window, 96, 93);
      stubResizeObserver(window);
    },
  });

  assert.equal(publishedClearance(dom.window), "97px");
  dom.window.close();
});

test("a re-wrapped sticky header republishes its clearance through the same observer", async () => {
  let observers = [];
  const dom = await loadDOMWithScript(APP_BUNDLE, {
    html: settingsPage(),
    url: "https://ovumcy.test/settings",
    beforeRun(window) {
      stubIndexHeight(window, 96, 93);
      observers = stubResizeObserver(window);
    },
  });

  const header = dom.window.document.querySelector(".paper-header");
  const index = dom.window.document.querySelector("[data-settings-section-nav]");
  const watching = observers.filter((observer) => observer.targets.includes(header));
  assert.equal(watching.length, 1, "expected exactly one observer on the sticky header");
  assert.ok(watching[0].targets.includes(index), "the header and the index share one observer");

  // 200% text zoom at 375px: the header wraps to a third row.
  dom.window.__testHeaderHeight = 188;
  watching[0].callback([]);
  assert.equal(publishedClearance(dom.window), "192px");
  dom.window.close();
});

test("without ResizeObserver a window resize republishes the header clearance", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, {
    html: settingsPage(),
    url: "https://ovumcy.test/settings",
    beforeRun(window) {
      stubIndexHeight(window, 96, 93);
      delete window.ResizeObserver;
    },
  });

  assert.equal(publishedClearance(dom.window), "97px");
  dom.window.__testHeaderHeight = 117;
  dom.window.dispatchEvent(new dom.window.Event("resize"));
  assert.equal(publishedClearance(dom.window), "121px");
  dom.window.close();
});

test("a header that is not laid out keeps the stylesheet default clearance", async () => {
  let observers = [];
  const dom = await loadDOMWithScript(APP_BUNDLE, {
    html: settingsPage(),
    url: "https://ovumcy.test/settings",
    beforeRun(window) {
      stubIndexHeight(window, 96, 0);
      observers = stubResizeObserver(window);
    },
  });

  assert.equal(publishedClearance(dom.window), "");
  // It lays out later: the observer publishes it, and a later 0 keeps it.
  dom.window.__testHeaderHeight = 93;
  observers[0].callback([]);
  assert.equal(publishedClearance(dom.window), "97px");
  dom.window.__testHeaderHeight = 0;
  observers[0].callback([]);
  assert.equal(publishedClearance(dom.window), "97px");
  dom.window.close();
});

test("a stack with no section index clears the header alone", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, {
    html: settingsPage({ withNav: false }),
    url: "https://ovumcy.test/settings",
    beforeRun(window) {
      stubResizeObserver(window);
    },
  });

  assert.equal(publishedHeight(dom.window), "0px");
  dom.window.close();
});
