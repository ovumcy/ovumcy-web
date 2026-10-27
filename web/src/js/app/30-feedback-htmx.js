  // The status island is not always the swap target. A settings section that
  // replaces itself (hx-swap="outerHTML" onto the card) targets the card, and the
  // island lives inside it — so every handler below resolves the container rather
  // than testing the target's own class. Before this, a card-level swap silently
  // skipped the toast, the auto-clear AND the error rendering: the request
  // succeeded or failed and the page said nothing either way.
  //
  // A host can hold more than one island (the egress card has one beside the
  // webhook Save and one beside the feed buttons). `source` is the element that
  // made the request; when it sits inside a [data-status-island-group] whose
  // own .save-status lies in the resolved host, that island wins, so the order
  // of the islands in the card does not matter.
  function requestSourceFromEvent(event) {
    var detail = event ? event.detail : null;
    if (detail && detail.elt) {
      return detail.elt;
    }
    return getEventTarget(event);
  }

  function statusContainerFor(target, source) {
    var resolved = resolveStatusContainer(target);
    if (!resolved || !source || !source.closest) {
      return resolved;
    }
    var group = source.closest("[data-status-island-group]");
    var host = resolved.closest ? resolved.closest("[data-status-island-host]") : null;
    if (!group || !host) {
      return resolved;
    }
    if (!host.contains(group)) {
      // A card that replaced itself took the requesting element with it, so
      // after the swap the source's group is the OLD one; find the new group of
      // the same name in the card that arrived.
      group = sameNamedIslandGroup(host, group.getAttribute("data-status-island-group"));
      if (!group) {
        return resolved;
      }
    }
    return group.querySelector(".save-status") || resolved;
  }

  function sameNamedIslandGroup(host, name) {
    if (!name) {
      return null;
    }
    var groups = host.querySelectorAll("[data-status-island-group]");
    for (var index = 0; index < groups.length; index += 1) {
      if (groups[index].getAttribute("data-status-island-group") === name) {
        return groups[index];
      }
    }
    return null;
  }

  function resolveStatusContainer(target) {
    if (!target || !target.classList) {
      return null;
    }
    if (target.classList.contains("save-status")) {
      return target;
    }
    // Otherwise the island belongs to the nearest element that DECLARES itself
    // its host — the element itself when it is one, else the closest such
    // ancestor. Both directions are needed: a card that replaces itself is the
    // swap target on success, while an error carries whichever element htmx
    // resolved for the request, which can be inside the card. Walking ancestors
    // rather than searching the document is what keeps a webhook error out of
    // the symptoms card: the settings page carries several islands and a bare
    // descendant search would return whichever came first.
    //
    // The host attribute is deliberately not data-success-toast. That one is an
    // opt-in to a toast, declared on the island itself; a card borrowing it to
    // be findable made one name mean two things, and the borrowed meaning was
    // the silent one — the card declared "toast me" and no toast could fire,
    // because the resolved island did not carry the attribute the toast reads.
    var surface =
      target.hasAttribute && target.hasAttribute("data-status-island-host")
        ? target
        : target.closest
          ? target.closest("[data-status-island-host]")
          : null;
    if (!surface || !surface.querySelector) {
      return null;
    }
    return surface.querySelector(".save-status");
  }

  function renderErrorStatus(target, text) {
    target.textContent = "";
    var block = document.createElement("div");
    block.className = "status-error";
    block.textContent = text;
    target.appendChild(block);
  }

  function createToastStack() {
    var existing = document.querySelector(".toast-stack");
    if (existing) {
      return existing;
    }
    var stack = document.createElement("div");
    stack.className = "toast-stack";
    // aria-live without role="status": status implies aria-atomic, which
    // would re-announce every toast already in the stack each time a new
    // one is appended.
    stack.setAttribute("aria-live", "polite");
    document.body.appendChild(stack);
    return stack;
  }

  function appendToastMessage(body, message, kind) {
    var messageWrap = document.createElement("span");
    messageWrap.className = "toast-message-wrap";

    var icon = document.createElement("span");
    icon.className = "toast-icon";
    icon.setAttribute("aria-hidden", "true");
    if (kind === "error") {
      icon.classList.add("toast-icon-error");
      icon.textContent = "⚠";
    } else {
      icon.textContent = "✓";
    }
    messageWrap.appendChild(icon);

    var text = document.createElement("span");
    text.className = "toast-message";
    text.textContent = message;
    messageWrap.appendChild(text);

    body.appendChild(messageWrap);
  }

  var successStatusClearTimers = new WeakMap();

  function initToastAPI() {
    // The live region must already be in the accessibility tree before the
    // first toast lands in it — a region inserted and populated in the same
    // breath is skipped by screen readers — so the stack is created eagerly
    // instead of on first use.
    var stack = createToastStack();

    function getStack() {
      return stack;
    }

    window.showToast = function (message, kind) {
      if (!message) {
        return;
      }

      var container = getStack();
      var toast = document.createElement("div");
      toast.className = (kind === "error" ? "status-error" : "status-ok") + " reveal";
      var body = document.createElement("div");
      body.className = "toast-body";
      appendToastMessage(body, message, kind === "error" ? "error" : "ok");

      var closeButton = document.createElement("button");
      closeButton.type = "button";
      closeButton.className = "toast-close";
      closeButton.setAttribute("aria-label", document.body.getAttribute("data-toast-close") || "Close");
      closeButton.textContent = "×";
      closeButton.addEventListener("click", function () {
        toast.remove();
      });
      body.appendChild(closeButton);

      toast.appendChild(body);
      container.appendChild(toast);

      window.setTimeout(function () {
        if (!toast.parentNode) {
          return;
        }
        toast.classList.add("toast-exit");
        window.setTimeout(function () {
          toast.remove();
        }, TOAST_EXIT_MS);
      }, TOAST_VISIBLE_MS);
    };
  }

  function getSaveFeedbackFormFromEvent(event) {
    var target = getEventTarget(event);
    if (!target || !target.closest) {
      return null;
    }
    return target.closest("form[data-save-feedback]");
  }

  function setSaveButtonState(form, isBusy) {
    if (!form) {
      return;
    }
    var button = form.querySelector("[data-save-button]");
    if (!button) {
      return;
    }

    button.disabled = isBusy;
    if (isBusy) {
      button.setAttribute("aria-busy", "true");
      button.classList.add("btn-loading");
      return;
    }
    button.removeAttribute("aria-busy");
    button.classList.remove("btn-loading");
  }

  function clearStatusTargetIfEmpty(target) {
    if (!target || target.querySelector(".status-ok") || target.querySelector(".status-error")) {
      return;
    }
    // A success status taken down after the account-changed notice replaced it
    // (its clear timer, a dismiss) leaves that notice standing.
    if (saveStatusHoldsAccountChange(target)) {
      return;
    }
    target.textContent = "";
  }

  function closeLabelText() {
    return document.body.getAttribute("data-toast-close") || "Close";
  }

  function ensureDismissibleSuccessStatus(target) {
    if (!target || !target.querySelector) {
      return null;
    }

    var successNode = target.querySelector(".status-ok");
    if (!successNode) {
      return null;
    }

    if (successNode.querySelector(".toast-close")) {
      return successNode;
    }

    var message = String(successNode.textContent || "").trim();
    successNode.textContent = "";

    var body = document.createElement("div");
    body.className = "toast-body";
    appendToastMessage(body, message, "ok");

    var closeButton = document.createElement("button");
    closeButton.type = "button";
    closeButton.className = "toast-close";
    closeButton.setAttribute("aria-label", closeLabelText());
    closeButton.setAttribute("data-dismiss-status", "true");
    closeButton.textContent = "×";
    body.appendChild(closeButton);

    successNode.appendChild(body);
    return successNode;
  }

  function scheduleClearSuccessStatus(target) {
    var successNode = ensureDismissibleSuccessStatus(target);
    if (!successNode) {
      return;
    }

    var existingTimer = successStatusClearTimers.get(successNode);
    if (existingTimer) {
      window.clearTimeout(existingTimer);
      successStatusClearTimers.delete(successNode);
    }

    // A status the server declares persistent carries safety guidance — the
    // prediction pause with its red-flag line — and stays until the owner
    // dismisses it. The server states the kind; nothing here reads the copy.
    if (successNode.getAttribute("data-status-kind") === "persistent") {
      return;
    }

    var timer = window.setTimeout(function () {
      if (!target.contains(successNode)) {
        successStatusClearTimers.delete(successNode);
        clearStatusTargetIfEmpty(target);
        return;
      }

      successNode.classList.add("toast-exit");
      window.setTimeout(function () {
        if (target.contains(successNode)) {
          successNode.remove();
        }
        successStatusClearTimers.delete(successNode);
        clearStatusTargetIfEmpty(target);
      }, TOAST_EXIT_MS);
    }, TOAST_VISIBLE_MS);
    successStatusClearTimers.set(successNode, timer);
  }

  // Take down the success status a region holds, with its pending clear: a later
  // save whose answer says nothing new must not leave an earlier answer standing.
  // A failure notice in the same region is not a success status and stays.
  function withdrawSuccessStatus(target) {
    var successNode = target && target.querySelector ? target.querySelector(".status-ok") : null;
    var timer;
    while (successNode) {
      timer = successStatusClearTimers.get(successNode);
      if (timer) {
        window.clearTimeout(timer);
        successStatusClearTimers.delete(successNode);
      }
      successNode.remove();
      successNode = target.querySelector(".status-ok");
    }
    clearStatusTargetIfEmpty(target);
  }

  // The message each island last raised, remembered by the island's id. The node
  // is not stable: a card that replaces itself brings a NEW island element on
  // every swap, so a key stored on the element resets exactly when the repeat it
  // exists to suppress arrives. An island with no id keeps the per-node key,
  // which is all that can be said about an element nothing can address twice.
  //
  // The registry hangs off window rather than off this closure so that it is one
  // registry per page, not one per evaluation of this bundle.
  function lastToastRegistry() {
    if (!window.__ovumcyLastToastByIsland) {
      window.__ovumcyLastToastByIsland = {};
    }
    return window.__ovumcyLastToastByIsland;
  }

  function lastToastFor(island) {
    return island.id ? lastToastRegistry()[island.id] : island.dataset.toastShown;
  }

  function rememberToastFor(island, identity) {
    if (island.id) {
      lastToastRegistry()[island.id] = identity;
      return;
    }
    island.dataset.toastShown = identity;
  }

  // The inverse of rememberToastFor, for BOTH keys: a request that starts, or
  // fails without an answer, ends the run of repeats, so the next success must
  // be allowed its toast. Clearing only the per-node key left an island with an
  // id suppressing the retry's toast as a repeat of the one before the failure.
  function forgetToastFor(island) {
    if (island.id) {
      delete lastToastRegistry()[island.id];
    }
    delete island.dataset.toastShown;
  }

  // What the repeat is judged by. The flash key names the message the server
  // chose and survives everything done to the rendered node afterwards — the
  // dismiss button appended below is already enough to change the node's text,
  // so text alone identifies a message only until something decorates it.
  function toastIdentity(successNode, message) {
    return successNode.getAttribute("data-flash-key") || message;
  }

  function maybeShowSuccessToast(target) {
    var successNode;
    var message;
    if (!target || target.getAttribute("data-success-toast") !== "true" || typeof window.showToast !== "function") {
      return;
    }

    successNode = target.querySelector(".status-ok");
    if (!successNode) {
      return;
    }

    message = String(successNode.textContent || "").trim();
    if (!message || lastToastFor(target) === toastIdentity(successNode, message)) {
      return;
    }

    rememberToastFor(target, toastIdentity(successNode, message));
    window.showToast(message, "ok");
  }

  function showResponseNotice(xhr) {
    var message;
    if (!xhr || typeof xhr.getResponseHeader !== "function" || typeof window.showToast !== "function") {
      return;
    }

    message = typeof window.__ovumcyDecodeResponseNoticeHeader === "function"
      ? window.__ovumcyDecodeResponseNoticeHeader(xhr.getResponseHeader("X-Ovumcy-Notice"))
      : String(xhr.getResponseHeader("X-Ovumcy-Notice") || "").trim();
    if (!message) {
      return;
    }
    window.showToast(message, "error");
  }

  // Parses a server error response into the message the client may show. The
  // fragment is parsed with DOMParser and only its TEXT is adopted: server
  // templates already escape user-supplied values, so today this is purely
  // defense-in-depth — any future regression that lets unescaped HTML into an
  // error response would otherwise become an instant DOM-XSS through
  // `target.innerHTML = responseText`.
  function parseServerStatusError(responseText) {
    if (!responseText || responseText.indexOf("status-error") === -1) {
      return null;
    }

    var doc = new DOMParser().parseFromString(responseText, "text/html");
    var fragment = doc.querySelector(".status-error");
    return {
      text: fragment ? fragment.textContent : responseText,
      key: fragment ? fragment.getAttribute("data-flash-key") || "" : ""
    };
  }

  // A day entry lives only in the form the owner typed it into: no draft is
  // written to storage, no offline cache, no service worker. That live form is
  // the whole recovery mechanism, so a save that does not land must leave every
  // field untouched and hand back a control that resubmits the same node.
  //
  // A self-hosted instance is regularly unreachable — the owner is off the home
  // network — so a failed save is a transport event, not a finding about the
  // owner's body. It is rendered on the neutral status-notice surface rather
  // than the red status-error one, inside the existing aria-live="polite"
  // container, and never as a success.
  //
  // Both day forms answer to it: the calendar editor, which saves on an
  // explicit press, and the dashboard journal, which saves itself. Two failure
  // surfaces for one kind of event would drift apart, so the selector below is
  // the single membership test — widened rather than duplicated.
  var DAY_SAVE_FORM_SELECTOR = "[data-day-editor-form], [data-dashboard-save-form]";

  function dayEditorFormFromEvent(event) {
    var form = getSaveFeedbackFormFromEvent(event);
    if (!form || !form.matches || !form.matches(DAY_SAVE_FORM_SELECTOR)) {
      return null;
    }
    return form;
  }

  // A notice that the account changed outranks every other status a day form's
  // region can be given until the next save of that form starts: an answer to a
  // request made before it (a save's own feedback, a failure, the conflict
  // choice's read) knows nothing of the change, and whichever lands last would
  // otherwise take the region, leaving a "try again" that cannot help or a
  // success beside a session that is no longer this page's.
  function isAccountChangedNoticeKey(messageKey) {
    return messageKey === ACCOUNT_CHANGED_NOTICE_KEY || messageKey === OPEN_ACCOUNT_CHANGED_NOTICE_KEY;
  }

  function saveStatusHoldsAccountChange(target) {
    var keys = target && target.querySelectorAll ? target.querySelectorAll("[data-notice-key]") : [];
    for (var index = 0; index < keys.length; index++) {
      if (isAccountChangedNoticeKey(keys[index].getAttribute("data-notice-key"))) {
        return true;
      }
    }
    return false;
  }

  // The next save of the form starts: what it answers is about the session as
  // it is now, so the account-changed notice stops holding the region. A
  // request of another control whose answer is written to the same region (the
  // cycle-start form beside a day form) starts the same way.
  function releaseAccountChangedRegion(target) {
    if (saveStatusHoldsAccountChange(target)) {
      target.replaceChildren();
    }
  }

  function releaseAccountChangedNotice(form) {
    releaseAccountChangedRegion(form && form.querySelector ? form.querySelector(".save-status") : null);
  }

  // Whether an answer to a request sent before the region took the
  // account-changed notice is kept out of it: every answer is, except that same
  // refusal. The htmx swap and the error rendering both ask here, so neither
  // route of a stale answer reaches the region.
  function accountChangeHoldRefuses(target, responseText) {
    if (!saveStatusHoldsAccountChange(target)) {
      return false;
    }
    var parsed = parseServerStatusError(String(responseText || ""));
    return !(parsed && isAccountChangedNoticeKey(parsed.key));
  }

  function renderDaySaveFailure(form, message, origin, messageKey) {
    var target = form && form.querySelector ? form.querySelector(".save-status") : null;
    if (!target || !message) {
      return false;
    }
    if (!isAccountChangedNoticeKey(messageKey) && saveStatusHoldsAccountChange(target)) {
      // Rendered, as far as the caller is concerned: the region already says
      // why no save can land, and no fallback may write over it either.
      return true;
    }
    if (messageKey === CSRF_REFUSED_NOTICE_KEY) {
      // The transport's own copy tells the owner to reload, which discards the
      // entry. The token is re-read behind this notice instead
      // (refreshCSRFToken), so the Retry beside it is the way out.
      message = form.getAttribute("data-day-save-refused-text") || message;
    }

    var notice = document.createElement("div");
    notice.className = "status-notice";
    notice.setAttribute("data-day-save-failed", origin);

    var text = document.createElement("span");
    text.className = "status-notice-message";
    text.textContent = message;
    if (messageKey) {
      text.setAttribute("data-notice-key", messageKey);
    }
    notice.appendChild(text);

    if (messageKey === DAY_SAVE_CONFLICT_NOTICE_KEY) {
      // Another device changed a field this save changes too. A retry of the
      // same save would be refused again; the owner chooses whose value the
      // day keeps instead, and the entry stays in the form until then.
      notice.setAttribute("data-day-save-conflict", "true");
      notice.appendChild(daySaveConflictChoice(form, "mine", "data-day-conflict-keep-label", "Keep mine"));
      notice.appendChild(daySaveConflictChoice(form, "theirs", "data-day-conflict-take-label", "Take theirs"));
      target.replaceChildren(notice);
      return true;
    }

    if (isAccountChangedNoticeKey(messageKey)) {
      // The form was rendered for another account than the one now signed in:
      // no retry can land it, so the notice offers none, and no sign-in link
      // either. The entry stays in the form, untouched. The read-side key is the
      // dashboard's header refresh, refused for the same reason.
      target.replaceChildren(notice);
      return true;
    }

    var retry = document.createElement("button");
    // Explicitly type="button": the status container sits inside the form, and
    // a default submit button here would fire a second save on every click that
    // reaches it.
    retry.type = "button";
    retry.className = "status-notice-action";
    retry.setAttribute("data-day-save-retry", "true");
    retry.textContent = form.getAttribute("data-day-save-retry-label") || "Try again";

    if (messageKey === SESSION_EXPIRED_NOTICE_KEY) {
      notice.appendChild(daySaveSignInLink(form));
    }
    notice.appendChild(retry);

    target.replaceChildren(notice);
    return true;
  }

  // A save refused because the session is gone cannot be fixed by retrying in
  // place, and signing in again in THIS tab navigates away from the only copy
  // of the entry. So the refusal offers the sign-in page in a new tab: the entry
  // stays in this page's form, the new tab sets a fresh session cookie, and the
  // retry beside the link then resubmits the same form under it. The CSRF token
  // the retry sends is read from the page at send time and survives the
  // sign-in — the token cookie is not rotated by authentication — so the retry
  // is not refused for a stale token; if the token did go stale meanwhile,
  // refreshCSRFToken below replaces it.
  //
  // The link is a fixed same-origin path carrying nothing from the entry or the
  // account, and nothing from the entry is written anywhere to survive the trip.
  var SESSION_EXPIRED_NOTICE_KEY = "common.error.unauthorized";
  var SIGN_IN_PATH = "/login";

  // If the account signed in from the other tab is not the one this form was
  // rendered for, the server refuses the retry (409) rather than write this
  // entry into that account; the form carries an opaque binding to the account
  // that rendered it.
  var ACCOUNT_CHANGED_NOTICE_KEY = "daylog.save_account_changed";
  // The read-side counterpart: the calendar's fetch of a day from a page
  // rendered for another account is refused with its own copy, since nothing
  // was being saved, and so is the refresh of its grid, with another: the grid
  // on screen stays as it was rendered, and nothing is being opened.
  var OPEN_ACCOUNT_CHANGED_NOTICE_KEY = "daylog.open_account_changed";
  var GRID_ACCOUNT_CHANGED_NOTICE_KEY = "daylog.grid_account_changed";

  // The retry above reads the CSRF token from the page at send time, and the
  // sign-in happens in another tab. The token cookie is not rotated by signing
  // in, but it can be gone by then: the server forgets a token after an hour
  // without a request carrying it, and a restart forgets every one. The other
  // tab's first page load then mints a new token and cookie, and this tab's
  // <meta> keeps the old one for good — every retry and every later autosave
  // would be refused 403 until a reload, which discards the entry.
  //
  // So a write refused by the CSRF check re-reads the token from the page
  // the owner is already on: a plain same-origin GET of the current path, which the server
  // answers with the token its cookie holds. Only the <meta> content (and the
  // hidden token inputs that mirror it) is adopted, after a shape check, and
  // only from a same-origin response; nothing else from the response enters the
  // page. The write is never re-sent from here — the owner's Retry is the only
  // thing that sends it again — and a refresh that fails leaves the notice and
  // the entry exactly as they were.
  var CSRF_TOKEN_SHAPE = /^[A-Za-z0-9._~+/=-]{8,512}$/;
  var csrfRefreshPending = null;

  // The CSRF check answers a 403 under a key of its own; every other 403 the
  // app gives (owner access, onboarding, a required password change, the shared
  // forbidden) carries a different key, and a fresh token cannot help any of
  // them.
  var CSRF_REFUSED_NOTICE_KEY = "common.error.csrf_refused";

  // A 403 that names no key at all did not come from the app, whose every
  // refusal is keyed: it is a proxy's or a firewall's own page standing in front
  // of it. The token may be stale behind it all the same, and the server was
  // reached, so it is treated as the CSRF refusal — token refreshed, refusal
  // copy shown — rather than as an unreachable server. The copy comes from the
  // page (body data-csrf-refused), since no fragment carried it.
  function isCSRFRefusal(status, refusalKey) {
    return Number(status) === 403 && (!refusalKey || refusalKey === CSRF_REFUSED_NOTICE_KEY);
  }

  function csrfRefusalCopy() {
    return document.body.getAttribute("data-csrf-refused") || "That action was refused. Please try again; if it is refused again, reload the page.";
  }

  // What an error answer says, parsed once. A CSRF refusal that carried no key
  // is given the CSRF key and copy here, so every surface renders it as the
  // refusal it is.
  function readServerRefusal(status, responseText) {
    var error = parseServerStatusError(typeof responseText === "string" ? responseText : "");
    var csrf = isCSRFRefusal(status, error ? error.key : "");
    if (csrf && (!error || !error.key)) {
      error = { text: csrfRefusalCopy(), key: CSRF_REFUSED_NOTICE_KEY };
    }
    return { error: error, csrf: csrf };
  }

  function csrfTokenFromPage(text) {
    if (!text || typeof DOMParser !== "function") {
      return "";
    }
    var meta = new DOMParser().parseFromString(text, "text/html").querySelector('meta[name="csrf-token"]');
    var token = meta ? String(meta.getAttribute("content") || "").trim() : "";
    return CSRF_TOKEN_SHAPE.test(token) ? token : "";
  }

  // An answer that does not say where it came from is not taken as same-origin.
  function responseIsSameOrigin(response) {
    if (!response || !response.url) {
      return false;
    }
    try {
      return new URL(response.url, window.location.href).origin === window.location.origin;
    } catch {
      return false;
    }
  }

  function adoptCSRFToken(token) {
    var meta = document.querySelector('meta[name="csrf-token"]');
    if (!meta) {
      return false;
    }
    meta.setAttribute("content", token);
    document.querySelectorAll('input[type="hidden"][name="csrf_token"]').forEach(function (input) {
      input.value = token;
    });
    return true;
  }

  // The page the refresh reads the token from. It is NOT the current page: a
  // refusal can come from a control on a surface whose GET spends or mints a
  // one-time value (the TOTP enrolment page mints a new secret into its setup
  // cookie on every visit, so re-fetching it would invalidate the QR code the
  // owner is still scanning). /privacy is public, renders through the base
  // layout that carries the csrf-token meta, and sets no cookie but the CSRF
  // one. The Go test TestPrivacyPageIsASideEffectFreeCSRFTokenSource pins both
  // halves; change this URL only to another page that test also holds.
  var CSRF_REFRESH_PAGE_URL = "/privacy";

  // One refresh at a time: a second refused write while one is in flight shares
  // it. Resolves true only when the page's token was replaced.
  function refreshCSRFToken() {
    if (csrfRefreshPending) {
      return csrfRefreshPending;
    }
    if (typeof window.fetch !== "function") {
      return Promise.resolve(false);
    }

    var pending = Promise.resolve().then(function () {
      return window.fetch(CSRF_REFRESH_PAGE_URL, {
        method: "GET",
        credentials: "same-origin",
        cache: "no-store",
        headers: { Accept: "text/html" }
      });
    }).then(function (response) {
      if (!response || !response.ok || !responseIsSameOrigin(response) || typeof response.text !== "function") {
        return "";
      }
      return response.text();
    }).then(function (text) {
      var token = csrfTokenFromPage(text);
      return token ? adoptCSRFToken(token) : false;
    }).catch(function () {
      return false;
    }).then(function (refreshed) {
      if (csrfRefreshPending === pending) {
        csrfRefreshPending = null;
      }
      return refreshed;
    });
    csrfRefreshPending = pending;
    return pending;
  }

  // Only the CSRF refusal refreshes: a 403 under any other key is not one a
  // fresh token can fix, and refreshing on it would fetch a whole page on every
  // such refusal. Every surface that shows the CSRF refusal tells the owner to
  // try again, so every one of them re-reads the token here, not only the day
  // writes. The notice is rendered before the refresh settles, so the owner is
  // never made to wait for it; a Retry pressed in the meantime waits for it
  // instead.
  function refreshAfterCSRFRefusal(refusal) {
    if (refusal && refusal.csrf) {
      refreshCSRFToken();
    }
  }

  function afterCSRFRefresh(action) {
    if (csrfRefreshPending) {
      csrfRefreshPending.then(action);
      return;
    }
    action();
  }

  // A day save refused (409) because a field it changes was changed on another
  // device since this page read it.
  var DAY_SAVE_CONFLICT_NOTICE_KEY = "daylog.save_conflict";

  function daySaveConflictChoice(form, choice, labelAttribute, fallback) {
    var button = document.createElement("button");
    // type="button" for the same reason as the retry: the notice sits inside
    // the form.
    button.type = "button";
    button.className = "status-notice-action";
    button.setAttribute("data-day-conflict-choice", choice);
    button.textContent = form.getAttribute(labelAttribute) || fallback;
    return button;
  }

  function daySaveSignInLink(form) {
    var link = document.createElement("a");
    link.className = "status-notice-action";
    link.href = SIGN_IN_PATH;
    link.target = "_blank";
    link.rel = "noopener";
    link.setAttribute("data-day-save-sign-in", "true");
    link.textContent = form.getAttribute("data-day-save-sign-in-label") || "Sign in in a new tab";
    return link;
  }

  // The calendar page renders an empty live region of its own, outside every
  // swap. A refusal's text is written into it: text changing inside a region
  // that already exists is announced, a node arriving with role="status" is not.
  //
  // The region is emptied at once and filled after a delay. Emptied and
  // refilled in the same task, the region ends the task holding the text it
  // already held, so a refusal repeating the previous one is not a change a
  // screen reader sees and is not announced. The delay is not 0: a 0 ms task can
  // still run before the next rendering and accessibility-tree update, which
  // would see empty-then-same-text as no change. It outlasts one frame
  // (about 16 ms at 60 Hz) with room to spare. A second refusal before the fill
  // replaces the pending one: only the latest text is written.
  var CALENDAR_REFUSAL_FILL_DELAY_MS = 150;
  var calendarRefusalFill = null;

  function announceCalendarRefusal(message) {
    var region = document.querySelector("[data-calendar-refusal-announcer]");
    if (!region || !message) {
      return;
    }
    if (calendarRefusalFill !== null) {
      window.clearTimeout(calendarRefusalFill);
    }
    region.textContent = "";
    calendarRefusalFill = window.setTimeout(function () {
      calendarRefusalFill = null;
      region.textContent = message;
    }, CALENDAR_REFUSAL_FILL_DELAY_MS);
  }

  // The calendar fetches its day editor, and every request from the page names
  // the account the page was rendered for. When another account has signed in
  // since, the server refuses the fetch (or a control inside the editor) with
  // the same account-changed key a refused save carries. htmx swaps no error
  // response, so without this the editor area would stay blank, or keep the
  // skeleton, with no word on why. Only the message TEXT is adopted. An editor
  // already open stays: a refused control or day switch must not take the
  // entry typed into it, so the notice leads the editor; only a skeleton or a
  // blank area gives way to the notice.
  //
  // The grid refresh after a save is refused the same way. The grid it would
  // have replaced stays on screen as rendered, so the notice leads it rather
  // than replacing it, and is announced: nothing the owner is looking at
  // changed. A notice inserted as a new node is not announced by a screen
  // reader, so the text is also written into the page's own live region, which
  // exists before it changes.
  //
  // refusal is the parsed answer (parseServerStatusError), read once by the
  // caller.
  function renderCalendarAccountRefusal(target, refusal) {
    if (!target || (target.id !== "day-editor" && target.id !== "calendar-grid-panel")) {
      return false;
    }
    if (
      !refusal ||
      (refusal.key !== OPEN_ACCOUNT_CHANGED_NOTICE_KEY &&
        refusal.key !== GRID_ACCOUNT_CHANGED_NOTICE_KEY &&
        refusal.key !== ACCOUNT_CHANGED_NOTICE_KEY)
    ) {
      return false;
    }

    var notice = document.createElement("div");
    notice.className = "status-notice";
    var text = document.createElement("span");
    text.className = "status-notice-message";
    text.setAttribute("data-notice-key", refusal.key);
    text.textContent = String(refusal.text || "").trim();
    notice.appendChild(text);
    announceCalendarRefusal(text.textContent);

    if (target.id === "day-editor") {
      notice.setAttribute("data-day-editor-refused", "account-changed");
      var previousNotice = target.querySelector("[data-day-editor-refused]");
      if (previousNotice) {
        previousNotice.remove();
      }
      if (target.querySelector("[data-day-editor-form]")) {
        target.prepend(notice);
        // The refused controls sit at the editor's foot; on a long editor the
        // notice above it would land out of view.
        if (typeof notice.scrollIntoView === "function") {
          notice.scrollIntoView({ block: "nearest" });
        }
      } else {
        target.replaceChildren(notice);
      }
      return true;
    }

    notice.setAttribute("data-calendar-grid-refused", "account-changed");
    var previous = target.querySelector("[data-calendar-grid-refused]");
    if (previous) {
      previous.remove();
    }
    target.prepend(notice);
    return true;
  }

  function renderDaySaveUnreachable(form) {
    return renderDaySaveFailure(
      form,
      form.getAttribute("data-day-save-failed-text") || "Couldn't save. Your entry is still here.",
      "unreachable",
      ""
    );
  }

  // A failed htmx day save older than the save the form's base already holds is
  // not shown: the newer save carried everything typed up to it (the dashboard
  // runner keeps the sequence; see __ovumcyHtmxDaySaveRefusalIgnored).
  function daySaveRefusalIgnored(form, responseText) {
    return typeof window.__ovumcyHtmxDaySaveRefusalIgnored === "function" &&
      window.__ovumcyHtmxDaySaveRefusalIgnored(form, responseText);
  }

  function handleDaySaveTransportFailure(event) {
    var form = dayEditorFormFromEvent(event);
    if (!form) {
      return;
    }
    if (!daySaveRefusalIgnored(form, "")) {
      renderDaySaveUnreachable(form);
    }
    // htmx fires afterRequest before sendError, so the button is already back;
    // re-assert it anyway, because a save button left disabled would take the
    // retry with it.
    setSaveButtonState(form, false);
  }

  // An htmx request that never got an answer (offline, aborted, timed out)
  // fires no responseError, so without this the status island kept whatever the
  // previous save said — a stale "Saved" beside a save that did not happen.
  // Every htmx request resolves its island the same way the answered paths
  // do; one whose target resolves to no island has nowhere to say it and is
  // left alone. The day editor and dashboard save are handled before this.
  function handleSettingsTransportFailure(event) {
    var detail = event ? event.detail : null;
    var form = getSaveFeedbackFormFromEvent(event);
    var target = detail ? detail.target : null;
    if (!target && form && form.getAttribute) {
      var selector = form.getAttribute("hx-target");
      try {
        target = selector ? document.querySelector(selector) : null;
      } catch {
        target = null;
      }
    }
    var island = statusContainerFor(target || form || getEventTarget(event), requestSourceFromEvent(event));
    if (!island) {
      return;
    }
    var fallback = document.body.getAttribute("data-request-failed") || "Request failed. Please try again.";
    renderErrorStatus(island, fallback);
    forgetToastFor(island);
    setSaveButtonState(form, false);
  }

  function handleHtmxTransportFailure(event) {
    if (dayEditorFormFromEvent(event)) {
      handleDaySaveTransportFailure(event);
      return;
    }
    handleSettingsTransportFailure(event);
  }

  function retryDaySave(form) {
    // A retry pressed while the token refresh after a 403 is still in flight
    // waits for it, so the resubmit carries the fresh token.
    afterCSRFRefresh(function () {
      resubmitDaySave(form);
    });
  }

  function resubmitDaySave(form) {
    // Resubmit the very same form node. Nothing was copied anywhere, so the
    // retry carries exactly what is on screen.
    //
    // The dashboard journal has no submit button to fall back on: its saves go
    // through the autosave runner, so the retry re-enters that runner instead
    // of asking htmx for a second mechanism on the same form.
    if (form.matches && form.matches("[data-dashboard-save-form]") && typeof window.__ovumcyRetryDashboardAutosave === "function") {
      window.__ovumcyRetryDashboardAutosave(form);
      return;
    }

    if (typeof form.requestSubmit === "function") {
      form.requestSubmit();
      return;
    }

    var saveButton = form.querySelector("[data-save-button]");
    if (saveButton) {
      saveButton.click();
    }
  }

  function initHTMXHooks() {
    document.body.addEventListener("htmx:configRequest", function (event) {
      var tokenMeta = document.querySelector('meta[name="csrf-token"]');
      if (!tokenMeta || !event || !event.detail) {
        return;
      }

      var token = tokenMeta.getAttribute("content");
      if (!token) {
        return;
      }

      // The form parameter is only attached to non-GET requests: htmx puts
      // parameters into the URL query for GET (methodsThatUseUrlParams), and
      // the token must never appear in URLs — browser history and reverse-
      // proxy access logs keep them. GETs are not CSRF-checked; the header
      // still rides along on every request.
      var verb = String(event.detail.verb || "").toLowerCase();
      if (verb !== "get") {
        event.detail.parameters = event.detail.parameters || {};
        event.detail.parameters.csrf_token = token;
      }
      event.detail.headers = event.detail.headers || {};
      event.detail.headers["X-CSRF-Token"] = token;

      var timezone = currentClientTimezone();
      if (timezone) {
        event.detail.headers[TIMEZONE_HEADER_NAME] = timezone;
      }
    });

    document.body.addEventListener("htmx:beforeRequest", function (event) {
      var statusTarget = statusContainerFor(event && event.detail ? event.detail.target : null, requestSourceFromEvent(event));
      if (statusTarget) {
        forgetToastFor(statusTarget);
      }
      // A day save sent through htmx (the calendar's Save and its retry) is the
      // start the dashboard runner marks by clearing its notice.
      releaseAccountChangedNotice(dayEditorFormFromEvent(event));
      releaseAccountChangedRegion(statusTarget);
      setSaveButtonState(getSaveFeedbackFormFromEvent(event), true);
    });

    // htmx writes a successful answer into its target itself, past every
    // renderer above: an answer to a request sent before the account-changed
    // notice (the calendar's Save that landed before the switch) would replace
    // the notice whenever it arrives last.
    document.body.addEventListener("htmx:beforeSwap", function (event) {
      var detail = event ? event.detail : null;
      if (!detail || !detail.shouldSwap) {
        return;
      }
      if (accountChangeHoldRefuses(statusContainerFor(detail.target, requestSourceFromEvent(event)), detail.xhr ? detail.xhr.responseText : "")) {
        detail.shouldSwap = false;
      }
    });

    document.body.addEventListener("htmx:afterRequest", function (event) {
      var form = getSaveFeedbackFormFromEvent(event);
      var xhr = event && event.detail ? event.detail.xhr : null;
      var ignored = !(event && event.detail && event.detail.successful) && form &&
        daySaveRefusalIgnored(form, xhr ? xhr.responseText : "");
      setSaveButtonState(form, false);
      showResponseNotice(xhr);
      // A stale refusal leaves the indicator to the newer save that answered.
      if (!ignored && form && form.matches && form.matches("[data-dashboard-save-form]") && typeof window.__ovumcyFinalizeDashboardManualSave === "function") {
        window.__ovumcyFinalizeDashboardManualSave(form);
      }
    });

    document.body.addEventListener("htmx:afterSwap", function (event) {
      var target = statusContainerFor(event && event.detail ? event.detail.target : null, requestSourceFromEvent(event));
      if (!target) {
        return;
      }

      var successNode = target.querySelector(".status-ok");
      if (!successNode) {
        return;
      }

      // The month grid refresh is the save response's HX-Trigger header; a
      // client trigger here as well fetched the grid twice per save.
      maybeShowSuccessToast(target);
      scheduleClearSuccessStatus(target);
    });

    document.body.addEventListener("htmx:afterSettle", function (event) {
      var target = statusContainerFor(event && event.detail ? event.detail.target : null, requestSourceFromEvent(event));
      if (!target) {
        return;
      }
      scheduleClearSuccessStatus(target);
    });

    document.body.addEventListener("click", function (event) {
      var dismissButton = closestFromEvent(event, "button[data-dismiss-status]");
      if (!dismissButton) {
        return;
      }

      var statusNode = dismissButton.closest(".status-ok, .status-error");
      if (!statusNode) {
        return;
      }

      var parent = statusNode.parentElement;
      statusNode.remove();
      clearStatusTargetIfEmpty(parent);
    });

    document.body.addEventListener("htmx:sendError", handleHtmxTransportFailure);
    document.body.addEventListener("htmx:sendAbort", handleHtmxTransportFailure);
    document.body.addEventListener("htmx:timeout", handleHtmxTransportFailure);

    document.body.addEventListener("click", function (event) {
      var retryButton = closestFromEvent(event, "[data-day-save-retry]");
      if (!retryButton) {
        return;
      }

      var form = retryButton.closest("form[data-day-editor-form], form[data-dashboard-save-form]");
      if (!form) {
        return;
      }

      event.preventDefault();
      retryDaySave(form);
    });

    document.body.addEventListener("click", function (event) {
      var choiceButton = closestFromEvent(event, "[data-day-conflict-choice]");
      var form = choiceButton ? choiceButton.closest("form[data-day-editor-form], form[data-dashboard-save-form]") : null;
      if (!form || typeof window.__ovumcyResolveDaySaveConflict !== "function") {
        return;
      }
      event.preventDefault();
      window.__ovumcyResolveDaySaveConflict(form, choiceButton.getAttribute("data-day-conflict-choice"));
    });

    document.body.addEventListener("htmx:responseError", function (event) {
      var target = event && event.detail ? event.detail.target : null;
      var form = getSaveFeedbackFormFromEvent(event);
      var dayForm = dayEditorFormFromEvent(event);

      // The body is read once; every branch below reuses this result.
      var xhr = event && event.detail ? event.detail.xhr : null;
      var refusal = readServerRefusal(xhr ? xhr.status : 0, xhr ? xhr.responseText : "");
      var serverError = refusal.error;
      refreshAfterCSRFRefusal(refusal);

      if (!dayForm && renderCalendarAccountRefusal(target, serverError)) {
        return;
      }

      if (dayForm && daySaveRefusalIgnored(dayForm, xhr ? xhr.responseText : "")) {
        return;
      }
      if (dayForm) {
        // The server answered, but not with a save. Keep its own message when
        // it sent one — it is more specific than any generic copy — and fall
        // back to the neutral "could not save" line otherwise. Either way the
        // owner gets the retry, and the typed entry is left alone.
        var serverMessage = serverError ? String(serverError.text || "").trim() : "";
        var rendered = serverMessage
          ? renderDaySaveFailure(dayForm, serverMessage, "rejected", serverError.key)
          : renderDaySaveUnreachable(dayForm);
        if (rendered) {
          return;
        }
      }
      target = statusContainerFor(target, requestSourceFromEvent(event));
      if (!target) {
        // A control with no status area of its own (the calendar editor's delete
        // and cycle-start controls, whose target is the editor itself) would
        // otherwise leave a refused write without a word.
        if (refusal.csrf && serverError && typeof window.showToast === "function") {
          window.showToast(String(serverError.text || "").trim(), "error");
        }
        if (form && form.matches && form.matches("[data-dashboard-save-form]") && typeof window.__ovumcyFinalizeDashboardManualSave === "function") {
          window.__ovumcyFinalizeDashboardManualSave(form);
        }
        return;
      }

      if (accountChangeHoldRefuses(target, xhr ? xhr.responseText : "")) {
        // A refusal of a control beside the day form (the cycle-start form)
        // sent before the notice: the region keeps the notice.
        return;
      }

      if (serverError) {
        // Safe-by-construction swap: only the parsed fragment's text is
        // adopted (see parseServerStatusError).
        var safeContainer = document.createElement("div");
        safeContainer.className = "status-error";
        safeContainer.textContent = serverError.text;
        target.replaceChildren(safeContainer);
        return;
      }

      var fallback = document.body.getAttribute("data-request-failed") || "Request failed. Please try again.";
      renderErrorStatus(target, fallback);
      if (form && form.matches && form.matches("[data-dashboard-save-form]") && typeof window.__ovumcyFinalizeDashboardManualSave === "function") {
        window.__ovumcyFinalizeDashboardManualSave(form);
      }
    });
  }
