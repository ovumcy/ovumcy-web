// The egress card replaces ITSELF through htmx, so the element the status
// machinery is handed is no longer the status island. It is the card on a
// success swap, and on an error it is whichever element htmx resolved for the
// request — which can be inside the card, because htmx never swaps a 4xx.
//
// Both have to reach the same island, and neither may reach a DIFFERENT card's
// island: the settings page carries several, and a lookup that searched the
// document rather than the element's own ancestors would render a webhook error
// into the symptoms card. These tests pin all three properties against the
// shipped bundle, because the failure they guard against is silent — the request
// completes, nothing appears, and the page looks like it simply did nothing.

import test from "node:test";
import assert from "node:assert/strict";
import { readAppBundle, loadDOMWithScript } from "./_helpers.mjs";

const APP_BUNDLE = readAppBundle();

const PAGE = `<!doctype html><html><head></head><body>
  <section data-settings-sections>
    <details id="settings-symptoms" data-settings-section data-status-island-host open>
      <div id="settings-symptoms-status" class="save-status" aria-live="polite"></div>
    </details>

    <details id="settings-egress" data-settings-section data-status-island-host open>
      <form
        hx-post="/api/v1/users/current/webhook"
        hx-target="#settings-egress"
        hx-swap="outerHTML"
        data-settings-webhook-form
        data-save-feedback>
        <button type="submit" data-save-button>Save webhook</button>
      </form>
      <div id="settings-egress-status" class="save-status" aria-live="polite"></div>
    </details>
  </section>
</body></html>`;

function fire(window, name, detail) {
  window.document.body.dispatchEvent(new window.CustomEvent(name, { detail, bubbles: true }));
}

function islandText(window, id) {
  return window.document.getElementById(id).textContent.trim();
}

test("an error answered for the whole card lands in that card's island", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: PAGE });
  try {
    fire(dom.window, "htmx:responseError", {
      target: dom.window.document.getElementById("settings-egress"),
      xhr: {
        responseText:
          '<div class="status-error" data-flash-key="settings.error.invalid_webhook_url">Enter a valid URL.</div>',
      },
    });

    assert.match(islandText(dom.window, "settings-egress-status"), /Enter a valid URL/);
    assert.equal(
      islandText(dom.window, "settings-symptoms-status"),
      "",
      "another card's island must stay untouched"
    );
  } finally {
    dom.window.close();
  }
});

test("an error answered for an element inside the card still lands in the card's island", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: PAGE });
  try {
    fire(dom.window, "htmx:responseError", {
      target: dom.window.document.querySelector("[data-settings-webhook-form]"),
      xhr: {
        responseText:
          '<div class="status-error" data-flash-key="settings.error.invalid_webhook_url">Enter a valid URL.</div>',
      },
    });

    assert.match(
      islandText(dom.window, "settings-egress-status"),
      /Enter a valid URL/,
      "the island is resolved from the nearest declaring ancestor, not only from the target itself"
    );
    assert.equal(islandText(dom.window, "settings-symptoms-status"), "");
  } finally {
    dom.window.close();
  }
});

// The card declares that it HOSTS an island; the island declares whether a
// success there also raises a toast. One attribute carrying both meanings is
// what these two pin against: the card wore the toast opt-in only to be
// findable, and the toast it named could never fire, because the attribute is
// read off the resolved island and the island did not carry it.
test("the host attribute resolves the island and the island's own opt-in raises the toast", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: PAGE });
  try {
    const { document } = dom.window;
    const island = document.getElementById("settings-egress-status");
    island.setAttribute("data-success-toast", "true");
    island.innerHTML = '<div class="status-ok">Webhook saved.</div>';

    fire(dom.window, "htmx:afterSwap", { target: document.getElementById("settings-egress") });

    const messages = [...document.querySelectorAll(".toast-stack .toast-message")].map(
      (node) => node.textContent
    );
    assert.ok(
      messages.includes("Webhook saved."),
      "the island inside the host card raised its own toast"
    );
  } finally {
    dom.window.close();
  }
});

test("a repeat message is suppressed even when the swap replaced the island node", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: PAGE });
  try {
    const { document } = dom.window;
    const card = document.getElementById("settings-egress");
    const islandMarkup =
      '<div id="settings-egress-status" class="save-status" data-success-toast="true" aria-live="polite">' +
      '<div class="status-ok" data-flash-key="settings.egress.webhook_removed">Webhook saved.</div></div>';

    const swap = () => {
      document.getElementById("settings-egress-status").remove();
      card.insertAdjacentHTML("beforeend", islandMarkup);
      fire(dom.window, "htmx:afterSwap", { target: card });
      return document.querySelectorAll(".toast-stack .toast-message").length;
    };

    // What a self-replacing card actually does: the island that raised the first
    // toast is gone, and an identical one stands in its place. A dedup key held
    // on the node resets here, which is precisely when the repeat arrives — so
    // the assertion is that the SECOND swap adds nothing, whatever the first
    // raised.
    const afterFirst = swap();
    const afterRepeat = swap();

    assert.ok(afterFirst > 0, "the first save is announced");
    assert.equal(
      afterRepeat,
      afterFirst,
      "the same message must not be announced again because the island was replaced"
    );
  } finally {
    dom.window.close();
  }
});

test("an element in no toast surface renders no status anywhere", async () => {
  const dom = await loadDOMWithScript(APP_BUNDLE, { html: PAGE });
  try {
    fire(dom.window, "htmx:responseError", {
      target: dom.window.document.querySelector("[data-settings-sections]"),
      xhr: {
        responseText: '<div class="status-error">Enter a valid URL.</div>',
      },
    });

    assert.equal(
      islandText(dom.window, "settings-egress-status"),
      "",
      "a target outside every card must not pick an island by document order"
    );
    assert.equal(islandText(dom.window, "settings-symptoms-status"), "");
  } finally {
    dom.window.close();
  }
});
