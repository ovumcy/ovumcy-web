  function dashboardAutosaveIndicator(form) {
    if (!form || !form.querySelector) {
      return null;
    }
    return form.querySelector("[data-dashboard-autosave-indicator]");
  }

  function dashboardAutosaveMessage(form, key, fallback) {
    if (!form || !form.getAttribute) {
      return fallback || "";
    }
    return String(form.getAttribute("data-autosave-" + key) || fallback || "");
  }

  // The journal has no save button, so this row is the whole report on the
  // save. Idle says nothing at all — a standing "auto-save is ready" line is
  // noise — and the error state speaks through the neutral retry notice in the
  // save-status region rather than adding a second, terser voice beside it.
  function dashboardIndicatorMessageNode(indicator) {
    var node = indicator.querySelector(".dashboard-autosave-message");
    if (node) {
      return node;
    }
    node = document.createElement("span");
    node.className = "dashboard-autosave-message";
    indicator.insertBefore(node, indicator.firstChild);
    return node;
  }

  function setDashboardAutosaveIndicator(form, key) {
    var indicator = dashboardAutosaveIndicator(form);
    if (!indicator) {
      return;
    }

    indicator.setAttribute("data-autosave-state", key);
    dashboardIndicatorMessageNode(indicator).textContent =
      key === "idle" || key === "error" ? "" : dashboardAutosaveMessage(form, key, "");
    syncDashboardUndoControl(form, indicator);
  }

  // Depth one, in memory only: the snapshot lives on the form node and dies
  // with the page. A day entry is health data — it is never written to
  // localStorage, sessionStorage or any other client store, here or anywhere
  // else in this bundle.
  //
  // The control is created once and left alone. Rebuilding the row on every
  // state change tore it out from under the pointer: clicking Undo blurs
  // whatever field was being typed in, the browser's own change event marks the
  // form dirty, and the row would re-render — so the click landed on a node
  // that no longer existed and the undo never ran (measured in the browser).
  function syncDashboardUndoControl(form, indicator) {
    var existing = indicator.querySelector("[data-dashboard-autosave-undo]");
    var label = String(form.getAttribute("data-autosave-undo") || "").trim();
    var button;

    if (!label || !form.__ovumcyAutosaveUndo) {
      if (existing) {
        existing.remove();
      }
      return;
    }
    if (existing) {
      return;
    }

    button = document.createElement("button");
    // The indicator sits inside the form: a default-type button here would
    // submit it.
    button.type = "button";
    button.className = "autosave-undo-button";
    button.setAttribute("data-dashboard-autosave-undo", "true");
    // Text, not a glyph: the control names itself for screen readers and for
    // keyboard users who reach it by tabbing.
    button.textContent = label;
    indicator.appendChild(button);
  }

  function dashboardFormEntries(form) {
    var entries = [];
    if (!form || typeof window.FormData !== "function") {
      return entries;
    }
    new window.FormData(form).forEach(function (value, name) {
      if (name === "csrf_token") {
        return;
      }
      entries.push([name, String(value)]);
    });
    return entries;
  }

  function dashboardFormState(form, empty) {
    return { entries: dashboardFormEntries(form), empty: !!empty };
  }

  function dashboardStateKey(state) {
    return state ? JSON.stringify(state.entries) : "";
  }

  function captureDashboardPersistedState(form) {
    if (!form || form.__ovumcyPersistedState) {
      return;
    }
    // What the server rendered is, by definition, what the server holds.
    form.__ovumcyPersistedState = dashboardFormState(
      form,
      form.getAttribute("data-today-entry-exists") !== "true"
    );
  }

  function syncRestoredPregnancyTestFields(form) {
    var fields = form.querySelectorAll("[data-pregnancy-test]");
    var checked;

    for (var index = 0; index < fields.length; index++) {
      checked = fields[index].querySelector("input[name='pregnancy_test']:checked");
      syncPregnancyTestField(fields[index], !!checked && checked.value !== "none");
    }
  }

  function restoreDashboardFormState(form, entries) {
    var selected = {};
    var index;
    var control;
    var values;
    var root;

    for (index = 0; index < entries.length; index++) {
      values = selected[entries[index][0]] || [];
      values.push(entries[index][1]);
      selected[entries[index][0]] = values;
    }

    for (index = 0; index < form.elements.length; index++) {
      control = form.elements[index];
      if (!control.name || control.name === "csrf_token" || control.type === "hidden") {
        continue;
      }

      values = selected[control.name] || [];
      if (control.type === "checkbox" || control.type === "radio") {
        control.checked = values.indexOf(control.value) !== -1;
        continue;
      }
      control.value = values.length > 0 ? values[0] : "";
    }

    // Restoring a radio does not fire change, so the pregnancy-test field's own
    // wording and Remove button would keep describing the value that was just
    // undone. Re-derive each from the radio that is checked now.
    syncRestoredPregnancyTestFields(form);

    root = typeof form.closest === "function" ? form.closest("[data-dashboard-editor]") : null;
    root = root || form;
    bindBinaryToggles(root);
    bindDashboardNotesCounters(root);
    syncPeriodToggleState(root);
    syncNoteDisclosure(root);
  }

  function clearDashboardAutosaveTimers(form) {
    if (!form) {
      return;
    }
    if (form.__ovumcyAutosaveTimer) {
      window.clearTimeout(form.__ovumcyAutosaveTimer);
      form.__ovumcyAutosaveTimer = 0;
    }
    if (form.__ovumcyAutosaveResetTimer) {
      window.clearTimeout(form.__ovumcyAutosaveResetTimer);
      form.__ovumcyAutosaveResetTimer = 0;
    }
  }

  function scheduleDashboardAutosaveIdleReset(form) {
    if (!form) {
      return;
    }
    if (form.__ovumcyAutosaveResetTimer) {
      window.clearTimeout(form.__ovumcyAutosaveResetTimer);
    }
    form.__ovumcyAutosaveResetTimer = window.setTimeout(function () {
      setDashboardAutosaveIndicator(form, "idle");
      form.__ovumcyAutosaveResetTimer = 0;
    }, 2200);
  }

  function notifyAutosaveNotice(response) {
    var notice;
    if (!response || typeof response.headers.get !== "function" || typeof window.showToast !== "function") {
      return;
    }
    notice = typeof window.__ovumcyDecodeResponseNoticeHeader === "function"
      ? window.__ovumcyDecodeResponseNoticeHeader(response.headers.get("X-Ovumcy-Notice"))
      : String(response.headers.get("X-Ovumcy-Notice") || "").trim();
    if (!notice) {
      return;
    }
    window.showToast(notice, "error");
  }

  // A day save answers with the server's own sentence about the day: "Saved.",
  // the self-care or fertile-window line, or — after a positive pregnancy test
  // — the prediction pause with its red-flag guidance. The htmx path swaps that
  // fragment into the status region; the autosave runs on fetch, so it reads
  // the body itself and shows the sentence in the same region, the way the
  // calendar editor's swap does. Only the TEXT is adopted: the fragment is
  // parsed, never assigned as markup, and the node is rebuilt by the shared
  // dismissible-status helper, so markup in a response renders as characters.
  // The sentence is the server's; nothing here composes copy. Its kind is the
  // server's too, read from data-status-kind, never inferred from the words.
  function parseServerStatusSuccess(responseText) {
    var doc;
    var status;
    var node;
    if (!responseText || responseText.indexOf("status-ok") === -1 || typeof DOMParser !== "function") {
      return null;
    }
    doc = new DOMParser().parseFromString(responseText, "text/html");
    status = doc.querySelector(".status-ok");
    if (!status) {
      return null;
    }
    node = status.querySelector(".toast-message") || status;
    return {
      message: String(node.textContent || "").trim(),
      kind: String(status.getAttribute("data-status-kind") || "")
    };
  }

  function renderDashboardSaveFeedback(form, responseText) {
    var target = form && form.querySelector ? form.querySelector(".save-status") : null;
    var status = parseServerStatusSuccess(String(responseText || ""));
    var node;
    if (!target || !status || !status.message) {
      return;
    }
    // A neutral status only says the day was saved, which the journal's own
    // indicator has already said; the dashboard does not say it twice. It also
    // says nothing about the earlier save, so whatever that one left in the
    // region — the persistent pregnancy-pause sentence included, which no timer
    // ever clears — is withdrawn: the region reflects the latest save.
    if (status.kind === "neutral") {
      withdrawSuccessStatus(target);
      return;
    }
    node = document.createElement("div");
    node.className = "status-ok";
    // The rebuilt node keeps a persistent kind, so the shared clear scheduler
    // leaves the safety sentence up until it is dismissed.
    if (status.kind === "persistent") {
      node.setAttribute("data-status-kind", "persistent");
    }
    node.textContent = status.message;
    target.replaceChildren(node);
    scheduleClearSuccessStatus(target);
  }

  function readDashboardSaveFeedback(response) {
    if (!response || typeof response.text !== "function") {
      return Promise.resolve("");
    }
    return response.text().catch(function () {
      return "";
    });
  }

  function buildDashboardAutosaveBody(form) {
    return new URLSearchParams(new FormData(form));
  }

  function dashboardRequestHeaders() {
    var headers = {
      "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8",
      "HX-Request": "true"
    };
    var tokenMeta = document.querySelector('meta[name="csrf-token"]');
    var timezone = currentClientTimezone();

    if (tokenMeta) {
      headers["X-CSRF-Token"] = tokenMeta.getAttribute("content") || "";
    }
    if (timezone) {
      headers[TIMEZONE_HEADER_NAME] = timezone;
    }
    return headers;
  }

  function clearDashboardSaveNotice(form) {
    var target = form && form.querySelector ? form.querySelector(".save-status") : null;
    var notice = target ? target.querySelector("[data-day-save-failed]") : null;
    if (notice) {
      notice.remove();
    }
  }

  // A save that did not land is a transport event, not a finding about the
  // owner's body: it reuses the day editor's neutral notice with its retry, and
  // it leaves every typed value exactly where it is. Nothing is retried behind
  // the owner's back — the runner stops until they press retry or type again,
  // so an unreachable instance is not hammered every two seconds.
  function failDashboardAutosave(form, responseText) {
    var parsed = parseServerStatusError(String(responseText || ""));
    var message = parsed ? String(parsed.text || "").trim() : "";

    form.__ovumcyAutosaveFailed = true;
    setDashboardAutosaveIndicator(form, "error");
    if (message) {
      renderDaySaveFailure(form, message, "rejected", parsed.key);
      return;
    }
    renderDaySaveUnreachable(form);
  }

  // Pick the HTTP verb from whichever hx-* attribute the form uses so the
  // autosave fetch tracks the canonical REST verb declared in the template
  // (PUT for /api/v1/days/{date} upsert, falling back to POST / action for
  // legacy or non-HTMX forms).
  function dashboardAutosaveEndpoint(form) {
    var hxVerbs = ["hx-put", "hx-patch", "hx-delete", "hx-post"];
    var endpoint = {
      method: "POST",
      url: String(form.getAttribute("action") || "").trim()
    };
    var hxValue;

    for (var verbIndex = 0; verbIndex < hxVerbs.length; verbIndex += 1) {
      hxValue = form.getAttribute(hxVerbs[verbIndex]);
      if (hxValue) {
        endpoint.method = hxVerbs[verbIndex].substring(3).toUpperCase();
        endpoint.url = String(hxValue).trim();
        break;
      }
    }
    return endpoint;
  }

  var dashboardStatusRefreshToken = 0;

  function dashboardStateValue(state, name) {
    var entries = state ? state.entries : [];
    for (var index = 0; index < entries.length; index++) {
      if (entries[index][0] === name) {
        return entries[index][1];
      }
    }
    return "";
  }

  // The blocks of the status header that are computed from the saved days, and
  // so can change when a pregnancy-test result does: a positive result pauses
  // the predictions (status line, ribbon, banner, warnings, explainer), and
  // removing it resumes them. Each is addressed by its own hook and replaced on
  // its own. The goal chip beside them is a live <details> with htmx-driven
  // forms inside it, and a node cloned out of a fetched page is never
  // htmx-processed — so nothing around these blocks is ever swapped. None of
  // them holds a form or an hx-* control (only plain links), so the clones need
  // no htmx.process() or re-initialisation. Order is the template's order: a
  // block that appears or disappears is placed after the nearest block before it.
  // The disclaimer is static and listed only as that anchor.
  var DASHBOARD_PREGNANCY_ALWAYS_PRESENT = [
    { selector: "[data-dashboard-cycle-day]", inPlace: false },
    { selector: "[data-dashboard-status-line]", inPlace: true }
  ];
  var DASHBOARD_PREGNANCY_ORDERED = [
    { selector: "[data-dashboard-cycle-ribbon]", anchorOnly: false },
    { selector: "[data-dashboard-prediction-explainer]", anchorOnly: false },
    { selector: "[data-dashboard-reminder-banner]", anchorOnly: false },
    { selector: "[data-dashboard-cycle-warnings]", anchorOnly: false },
    { selector: "[data-dashboard-prediction-disclaimer]", anchorOnly: true },
    { selector: "[data-dashboard-factor-hint]", anchorOnly: false }
  ];

  function syncDashboardDataAttributes(current, next) {
    var index;
    var name;

    for (index = current.attributes.length - 1; index >= 0; index--) {
      name = current.attributes[index].name;
      if (name.indexOf("data-") === 0 && !next.hasAttribute(name)) {
        current.removeAttribute(name);
      }
    }
    for (index = 0; index < next.attributes.length; index++) {
      name = next.attributes[index].name;
      if (name.indexOf("data-") === 0) {
        current.setAttribute(name, next.attributes[index].value);
      }
    }
  }

  // A node taken out of a fetched page arrives with its entrance animation
  // class: left on, the block would fade in again as if the page had loaded.
  function importDashboardNode(node) {
    var clone = document.importNode(node, true);
    var revealed;

    if (clone.classList) {
      clone.classList.remove("reveal");
    }
    if (typeof clone.querySelectorAll === "function") {
      revealed = clone.querySelectorAll(".reveal");
      for (var index = 0; index < revealed.length; index++) {
        revealed[index].classList.remove("reveal");
      }
    }
    return clone;
  }

  function dashboardSwappableSelectors() {
    var selectors = DASHBOARD_PREGNANCY_ALWAYS_PRESENT.map(function (block) {
      return block.selector;
    });
    DASHBOARD_PREGNANCY_ORDERED.forEach(function (block) {
      if (!block.anchorOnly) {
        selectors.push(block.selector);
      }
    });
    return selectors;
  }

  // What the swap is about to replace may hold the keyboard focus (a link in
  // the warnings block); the replaced node takes focus with it to the body.
  // Remember which block held it and what the focused element was, by its href
  // or its data-* hooks, so the equivalent element in the new block can have it.
  function captureDashboardBlockFocus(header) {
    var active = document.activeElement;
    var selectors = dashboardSwappableSelectors();
    var block;
    var hooks = [];

    if (!active || active === document.body || !header.contains(active)) {
      return null;
    }
    for (var index = 0; index < selectors.length; index++) {
      block = header.querySelector(selectors[index]);
      if (!block || !block.contains(active)) {
        continue;
      }
      if (block !== active) {
        for (var attrIndex = 0; attrIndex < active.attributes.length; attrIndex++) {
          if (active.attributes[attrIndex].name.indexOf("data-") === 0) {
            hooks.push({ name: active.attributes[attrIndex].name, value: active.attributes[attrIndex].value });
          }
        }
      }
      return {
        selector: selectors[index],
        tag: active.tagName,
        href: block === active ? null : active.getAttribute("href"),
        hooks: hooks
      };
    }
    return null;
  }

  // The same href wins across every candidate first; only then a shared hook,
  // by name AND value, because several links in one block can share a hook
  // name (data-late-cycle-action) and differ only by what it says.
  function equivalentDashboardFocusTarget(block, saved) {
    var candidates = block.querySelectorAll("*");
    var candidate;

    for (var index = 0; index < candidates.length; index++) {
      candidate = candidates[index];
      if (
        candidate.tagName === saved.tag &&
        saved.href !== null &&
        candidate.getAttribute("href") === saved.href
      ) {
        return candidate;
      }
    }
    for (var hookPass = 0; hookPass < candidates.length; hookPass++) {
      candidate = candidates[hookPass];
      if (candidate.tagName !== saved.tag) {
        continue;
      }
      for (var hookIndex = 0; hookIndex < saved.hooks.length; hookIndex++) {
        if (candidate.getAttribute(saved.hooks[hookIndex].name) === saved.hooks[hookIndex].value) {
          return candidate;
        }
      }
    }
    return null;
  }

  // Focus follows the owner's place: the same link, or the same hook, in the
  // new block. With no equivalent, the block itself takes it (tabindex -1 only
  // here, so it is focusable by script and never a tab stop); a block the
  // server dropped hands it to the status line, which is always there.
  function restoreDashboardBlockFocus(header, saved) {
    var block;
    var target;

    if (!saved) {
      return;
    }
    block = header.querySelector(saved.selector) || header.querySelector("[data-dashboard-status-line]");
    if (!block) {
      return;
    }
    target = block.matches(saved.selector) ? equivalentDashboardFocusTarget(block, saved) : null;
    if (!target) {
      target = block;
      if (!block.hasAttribute("tabindex")) {
        block.setAttribute("tabindex", "-1");
        block.addEventListener(
          "blur",
          function () {
            block.removeAttribute("tabindex");
          },
          { once: true }
        );
      }
    }
    if (typeof target.focus === "function") {
      target.focus();
    }
  }

  function swapDashboardPregnancyBlocks(header, nextHeader) {
    var previous = null;
    var savedFocus = captureDashboardBlockFocus(header);

    syncDashboardDataAttributes(header, nextHeader);

    // The status line is a live region: it keeps its node and takes the new
    // content, because a region that is itself inserted is not announced.
    DASHBOARD_PREGNANCY_ALWAYS_PRESENT.forEach(function (block) {
      var existing = header.querySelector(block.selector);
      var fresh = nextHeader.querySelector(block.selector);
      if (!existing || !fresh) {
        return;
      }
      if (!block.inPlace) {
        existing.replaceWith(importDashboardNode(fresh));
        return;
      }
      syncDashboardDataAttributes(existing, fresh);
      while (existing.firstChild) {
        existing.removeChild(existing.firstChild);
      }
      for (var child = fresh.firstChild; child; child = child.nextSibling) {
        existing.appendChild(importDashboardNode(child));
      }
    });

    DASHBOARD_PREGNANCY_ORDERED.forEach(function (block) {
      var existing = header.querySelector(block.selector);
      var fresh = block.anchorOnly ? null : nextHeader.querySelector(block.selector);
      var clone;

      if (!block.anchorOnly) {
        if (fresh) {
          clone = importDashboardNode(fresh);
          if (existing) {
            existing.replaceWith(clone);
          } else if (previous) {
            previous.after(clone);
          } else {
            header.appendChild(clone);
          }
          existing = clone;
        } else if (existing) {
          existing.remove();
          existing = null;
        }
      }
      previous = existing || previous;
    });

    restoreDashboardBlockFocus(header, savedFocus);
  }

  // The status header is computed server-side from the saved days, and a
  // pregnancy-test result is one of the inputs. The journal itself updates as
  // the owner clicks, so the dashboard page is fetched again and only the
  // dependent blocks above are swapped: the form, its focus and the goal chip
  // stay as they are. Newest request wins; a failed refresh leaves the header as
  // it was, since the save itself already succeeded.
  function refreshDashboardStatusHeader() {
    var current = document.querySelector("[data-dashboard-status-header]");
    var headers;
    var token;

    if (!current || typeof window.fetch !== "function" || typeof window.DOMParser !== "function") {
      return Promise.resolve(false);
    }

    dashboardStatusRefreshToken += 1;
    token = dashboardStatusRefreshToken;
    headers = dashboardRequestHeaders();
    delete headers["Content-Type"];
    delete headers["HX-Request"];
    headers.Accept = "text/html";

    return window.fetch(window.location.pathname + window.location.search, {
      method: "GET",
      credentials: "same-origin",
      headers: headers
    }).then(function (response) {
      return response.ok ? response.text() : "";
    }).then(function (text) {
      var next;
      var latest;

      if (!text || dashboardStatusRefreshToken !== token) {
        return false;
      }
      next = new window.DOMParser().parseFromString(text, "text/html").querySelector("[data-dashboard-status-header]");
      latest = document.querySelector("[data-dashboard-status-header]");
      if (!next || !latest) {
        return false;
      }
      swapDashboardPregnancyBlocks(latest, next);
      return true;
    }).catch(function () {
      return false;
    });
  }

  function runDashboardAutosave(form, mode) {
    var requestVersion;
    var endpoint;
    var url;
    var method;
    var headers;
    var body;
    var previousState;
    var sentState;

    if (!form || form.dataset.autosaveDirty !== "true") {
      return Promise.resolve(true);
    }
    if (form.__ovumcyAutosaveInFlight) {
      return form.__ovumcyAutosaveInFlight;
    }

    clearDashboardAutosaveTimers(form);
    if (!validateTemperatureInputs(form, false)) {
      setDashboardAutosaveIndicator(form, "invalid");
      scheduleDashboardAutosaveIdleReset(form);
      return Promise.resolve(false);
    }
    clearDashboardSaveNotice(form);
    setDashboardAutosaveIndicator(form, "saving");

    requestVersion = form.__ovumcyAutosaveVersion || 0;
    captureDashboardPersistedState(form);
    previousState = form.__ovumcyPersistedState;
    endpoint = dashboardAutosaveEndpoint(form);
    method = endpoint.method;
    url = endpoint.url;
    headers = dashboardRequestHeaders();
    body = buildDashboardAutosaveBody(form);
    // What is on the wire is what the server will hold: snapshot it here, and
    // promote it to "persisted" only once the server has said yes.
    sentState = dashboardFormState(form, false);

    // Which edit is on the wire, readable from outside this call: the unload
    // flush has to know whether the open request already carries the newest
    // body or an older one.
    form.__ovumcyAutosaveInFlightVersion = requestVersion;
    // Every save rides a keepalive request, not only the unload flush: a save
    // the debounce already put on the wire is one the owner has seen start, and
    // a reload or a closed tab must not cancel it. One request carries the
    // edit; nothing is re-sent behind it. The body stays well inside the
    // browser's 64 KiB keepalive budget — the note is capped at 2000
    // characters, under 18 KiB URL-encoded.
    form.__ovumcyAutosaveInFlight = window.fetch(url, {
      method: method,
      credentials: "same-origin",
      keepalive: true,
      headers: headers,
      body: body.toString()
    }).then(function (response) {
      if (!response.ok) {
        return response.text().catch(function () {
          return "";
        }).then(function (text) {
          failDashboardAutosave(form, text);
          return false;
        });
      }
      notifyAutosaveNotice(response);
      if ((form.__ovumcyAutosaveVersion || 0) === requestVersion) {
        delete form.dataset.autosaveDirty;
      }
      // Undo goes back one step, to the state the server held before this
      // save. Undoing an undo is not offered: depth stays at one. A save that
      // carried no change — a blur can fire one — is not a step, so it leaves
      // the existing step back alone instead of collapsing it onto itself.
      if (mode === "undo") {
        form.__ovumcyAutosaveUndo = null;
      } else if (previousState && dashboardStateKey(sentState) !== dashboardStateKey(previousState)) {
        form.__ovumcyAutosaveUndo = previousState;
      }
      form.__ovumcyPersistedState = sentState;
      form.__ovumcyAutosaveFailed = false;
      setDashboardAutosaveIndicator(form, "saved");
      if (previousState && dashboardStateValue(sentState, "pregnancy_test") !== dashboardStateValue(previousState, "pregnancy_test")) {
        refreshDashboardStatusHeader();
      }
      return readDashboardSaveFeedback(response).then(function (text) {
        renderDashboardSaveFeedback(form, text);
        return true;
      });
    }).catch(function () {
      failDashboardAutosave(form, "");
      return false;
    }).finally(function () {
      form.__ovumcyAutosaveInFlight = null;
      form.__ovumcyAutosaveInFlightVersion = 0;
      if (form.dataset.autosaveDirty === "true" && !form.__ovumcyAutosaveFailed) {
        form.__ovumcyAutosaveTimer = window.setTimeout(function () {
          runDashboardAutosave(form);
        }, 2000);
      }
    });

    return form.__ovumcyAutosaveInFlight;
  }

  // The item-32 safety rail: only a control the owner actually touched marks
  // the form dirty, and only a dirty form is ever sent. An untouched dashboard
  // therefore issues no request at all, and no default value is ever recorded
  // as an observation about the day.
  function markDashboardAutosaveDirty(form) {
    if (!form) {
      return;
    }
    captureDashboardPersistedState(form);
    form.__ovumcyAutosaveVersion = (form.__ovumcyAutosaveVersion || 0) + 1;
    form.dataset.autosaveDirty = "true";
    form.__ovumcyAutosaveFailed = false;
    if (form.__ovumcyAutosaveInFlight) {
      return;
    }
    // The row reports saves, not keystrokes: an edit leaves the last outcome
    // standing until the next save replaces it. Rewriting it on every change
    // reflowed the row under the pointer — clicking Undo blurs the field being
    // typed in, the browser's change event lands first, the row re-laid out and
    // the click hit the container instead of the button (measured in the
    // browser: the click's target was the indicator DIV).
    if (form.__ovumcyAutosaveTimer) {
      window.clearTimeout(form.__ovumcyAutosaveTimer);
    }
    form.__ovumcyAutosaveTimer = window.setTimeout(function () {
      runDashboardAutosave(form);
    }, 2000);
  }

  function handleDashboardQuickAction(root, action) {
    var periodToggle = root.querySelector("[data-period-toggle]");
    var moodSection = root.querySelector("[data-dashboard-section='mood']");
    var symptomSection = root.querySelector("[data-dashboard-section='symptoms']");

    switch (action) {
      case "period":
        if (!periodToggle) {
          return;
        }
        periodToggle.checked = !periodToggle.checked;
        periodToggle.dispatchEvent(new Event("change", { bubbles: true }));
        if (periodToggle.checked) {
          maybeAcknowledgePeriodTip(root);
        }
        break;
      case "mood":
        focusSectionControl(moodSection, "input[name='mood']:checked, input[name='mood']");
        break;
      case "symptom":
        focusSectionControl(symptomSection, "input[name='symptom_ids']:checked, input[name='symptom_ids']");
        break;
    }
  }

  // Outcome-independent by design: the indicator row reports save state, and a
  // failure is reported by the save-status swap instead. The finalizer only
  // stands the row down.
  function finalizeDashboardManualSave(form) {
    if (!form) {
      return;
    }
    clearDashboardAutosaveTimers(form);
    delete form.dataset.autosaveDirty;
    setDashboardAutosaveIndicator(form, "idle");
  }

  window.__ovumcyFinalizeDashboardManualSave = finalizeDashboardManualSave;

  // The retry the failure notice offers re-enters this runner rather than
  // asking htmx to submit the form: one save mechanism per form.
  function retryDashboardAutosave(form) {
    if (!form) {
      return Promise.resolve(false);
    }
    clearDashboardAutosaveTimers(form);
    form.__ovumcyAutosaveFailed = false;
    form.dataset.autosaveDirty = "true";
    return runDashboardAutosave(form);
  }

  window.__ovumcyRetryDashboardAutosave = retryDashboardAutosave;

  // Undoing the first save of a day that was empty cannot be expressed as
  // another upsert: an empty entry is an absent entry, so the undo issues the
  // same DELETE the "clear today" action does.
  function runDashboardUndoClear(form, undoState) {
    var url = String(form.getAttribute("data-autosave-clear-url") || "").trim();
    if (!url) {
      return Promise.resolve(false);
    }

    setDashboardAutosaveIndicator(form, "saving");
    form.__ovumcyAutosaveInFlight = window.fetch(url, {
      method: "DELETE",
      credentials: "same-origin",
      // Started is started: like every autosave, the undo outlives the page.
      keepalive: true,
      headers: dashboardRequestHeaders()
    }).then(function (response) {
      if (!response.ok) {
        return response.text().catch(function () {
          return "";
        }).then(function (text) {
          failDashboardAutosave(form, text);
          return false;
        });
      }
      delete form.dataset.autosaveDirty;
      form.__ovumcyPersistedState = undoState;
      form.__ovumcyAutosaveFailed = false;
      setDashboardAutosaveIndicator(form, "saved");
      // The day is gone server-side, and the page around the journal (cycle
      // day, warnings, the clear action itself) was rendered against it. The
      // clear endpoint asks for the dashboard back; honor it.
      reloadDashboardAfterUndo(response);
      return true;
    }).catch(function () {
      failDashboardAutosave(form, "");
      return false;
    }).finally(function () {
      form.__ovumcyAutosaveInFlight = null;
    });

    return form.__ovumcyAutosaveInFlight;
  }

  function reloadDashboardAfterUndo(response) {
    var target = response && response.headers && typeof response.headers.get === "function"
      ? String(response.headers.get("HX-Redirect") || "").trim()
      : "";
    if (!target || typeof window.location.assign !== "function") {
      return;
    }
    window.location.assign(target);
  }

  function runDashboardUndo(form) {
    var undoState = form ? form.__ovumcyAutosaveUndo : null;
    if (!undoState) {
      return Promise.resolve(false);
    }

    clearDashboardAutosaveTimers(form);
    clearDashboardSaveNotice(form);
    // Depth one: the step back is consumed by taking it.
    form.__ovumcyAutosaveUndo = null;
    restoreDashboardFormState(form, undoState.entries);

    if (undoState.empty) {
      return runDashboardUndoClear(form, undoState);
    }

    form.__ovumcyAutosaveVersion = (form.__ovumcyAutosaveVersion || 0) + 1;
    form.dataset.autosaveDirty = "true";
    form.__ovumcyAutosaveFailed = false;
    // Same path, same status surface: an undo that fails is reported exactly
    // like a save that fails.
    return runDashboardAutosave(form, "undo");
  }

  function isDashboardSaveForm(form) {
    return !!(form && form.matches && form.matches("[data-dashboard-save-form]"));
  }

  // One request that outlives the page. Nothing is sent from `beforeunload`:
  // it fires before the owner answers the leave prompt, and a body sent from a
  // page that then stays is on the wire unordered against every save made
  // after it — an older body landing last reverts a newer commit. Every unload
  // write goes out from `pagehide`, which fires only once the page is really
  // going (a cancelled leave never reaches it).
  function sendKeepaliveOnPageHide(request) {
    window.fetch(request.url, {
      method: request.method,
      credentials: "same-origin",
      keepalive: true,
      headers: request.headers,
      body: request.body
    }).catch(function () {
      // The page is leaving; there is no surface left to report to.
    });
  }

  // The page going away is the last chance the newest journal value gets, and
  // the ordinary runner cannot take it: while a save is open it hands back that
  // pending promise, which carries the older body. An edit made in that window
  // bumps the version and queues nothing — the only thing that would ever send
  // it is the re-arm in the runner's `finally`, a 2 s timer no unload survives.
  // So a newer version leaves on its own keepalive request.
  //
  // This is the dashboard form's ONLY unload writer. The form carries hx-put,
  // so an htmx submit (Enter in a field) can be open too, carrying the form as
  // it stood when it went out; that XMLHttpRequest dies with the page. What
  // goes out in its place is the form as it stands now — the journal autosaves,
  // so the newest value typed is the one owed — on one request, never the htmx
  // snapshot beside it: two unordered writes let the older one land last.
  //
  // The day upsert is idempotent, so a version this flush already sent is not
  // taken off the dirty ledger: should the page come back from the
  // back/forward cache, the normal path re-sending the same body costs
  // nothing, while clearing dirty here against a request whose outcome nobody
  // will see could lose the edit twice.
  function flushDashboardAutosaveOnPageHide(form) {
    var htmxSave;
    var version;
    var endpoint;
    var request;

    if (!form) {
      return;
    }
    htmxSave = form.__ovumcyDaySaveInFlight || null;
    if (form.dataset.autosaveDirty !== "true" && !htmxSave) {
      return;
    }
    if (!htmxSave && !form.__ovumcyAutosaveInFlight) {
      runDashboardAutosave(form);
      return;
    }

    version = form.__ovumcyAutosaveVersion || 0;
    // The open autosave already carries this edit, and it is a keepalive one:
    // it outlives the page on its own.
    if (form.__ovumcyAutosaveInFlight && version === (form.__ovumcyAutosaveInFlightVersion || 0)) {
      return;
    }
    // pagehide fires again after a back/forward-cache restore: send once.
    if (form.__ovumcyAutosaveUnloadFlushedVersion === version) {
      return;
    }

    // The same refusal the ordinary runner makes: a body it would not send is
    // not one to smuggle out on the unload path. The open htmx request's body
    // was accepted for sending, so it is the newest one left to keep.
    if (validateTemperatureInputs(form, false)) {
      endpoint = dashboardAutosaveEndpoint(form);
      request = {
        method: endpoint.method,
        url: endpoint.url,
        headers: dashboardRequestHeaders(),
        body: buildDashboardAutosaveBody(form).toString()
      };
    } else if (htmxSave) {
      request = htmxSave;
    } else {
      return;
    }

    form.__ovumcyAutosaveUnloadFlushedVersion = version;
    if (htmxSave) {
      htmxSave.resent = true;
    }
    sendKeepaliveOnPageHide(request);
  }

  function bindDashboardAutosaveBeforeUnload() {
    if (document.body && document.body.dataset.dashboardAutosaveBeforeUnloadBound === "1") {
      return;
    }
    if (document.body) {
      document.body.dataset.dashboardAutosaveBeforeUnloadBound = "1";
    }

    // Decides the prompt, sends nothing: the owner may yet stay.
    window.addEventListener("beforeunload", function (event) {
      var forms = document.querySelectorAll("[data-dashboard-save-form]");
      var saving = false;
      for (var index = 0; index < forms.length; index++) {
        if (forms[index].__ovumcyAutosaveInFlight || forms[index].__ovumcyDaySaveInFlight) {
          saving = true;
        }
      }
      if (saving) {
        warnBeforeLeavingDuringSave(event);
      }
    });

    window.addEventListener("pagehide", function () {
      var forms = document.querySelectorAll("[data-dashboard-save-form]");
      for (var index = 0; index < forms.length; index++) {
        flushDashboardAutosaveOnPageHide(forms[index]);
      }
    });
  }

  // The keepalive request is what keeps the edit; the browser's own leave
  // prompt is the second rail, for whatever the network does to a request
  // that has to outlive its page.
  function warnBeforeLeavingDuringSave(event) {
    event.preventDefault();
    event.returnValue = "";
  }

  // An explicit Save goes out through htmx on an XMLHttpRequest, which the
  // browser cancels with the page. The body that request carries is noted
  // when it is sent, so the unload path can hand that same body — not
  // whatever the form holds by then — to a keepalive request that outlives
  // the page. The day upsert is a full-form, idempotent PUT: should the
  // original also land, the second write changes nothing.
  function rememberDaySaveInFlight(event) {
    var form = dayEditorFormFromEvent(event);
    var config = event && event.detail ? event.detail.requestConfig : null;
    var verb;

    if (!form || !config || config.elt !== form) {
      return;
    }
    verb = String(config.verb || "").toUpperCase();
    if (!verb || verb === "GET") {
      return;
    }
    form.__ovumcyDaySaveInFlight = {
      method: verb,
      url: String(config.path || ""),
      headers: Object.assign({}, config.headers || {}),
      body: new URLSearchParams(config.formData).toString(),
      resent: false
    };
  }

  function forgetDaySaveInFlight(event) {
    var form = dayEditorFormFromEvent(event);
    if (form) {
      form.__ovumcyDaySaveInFlight = null;
    }
  }

  // What the calendar owner expects kept is the explicit Save they pressed —
  // the noted body, never an edit typed after it and not saved.
  function resendDaySaveOnPageHide(form) {
    var pending = form.__ovumcyDaySaveInFlight;
    // pagehide fires again after a back/forward-cache restore: send once.
    if (!pending || pending.resent) {
      return;
    }
    pending.resent = true;
    sendKeepaliveOnPageHide(pending);
  }

  // Every day-save form gets exactly one unload writer. The dashboard form
  // matches the selector too (it carries hx-put), but its own pagehide flush
  // owns it — it sends the newer of the htmx body and the form — so this guard
  // only notes its htmx request and leaves the writing alone.
  function daySaveFormsOwnedByUnloadGuard() {
    var forms = document.querySelectorAll(DAY_SAVE_FORM_SELECTOR);
    var owned = [];
    for (var index = 0; index < forms.length; index++) {
      if (!isDashboardSaveForm(forms[index])) {
        owned.push(forms[index]);
      }
    }
    return owned;
  }

  function bindDaySaveUnloadGuard() {
    if (!document.body || document.body.dataset.daySaveUnloadGuardBound === "1") {
      return;
    }
    document.body.dataset.daySaveUnloadGuardBound = "1";

    document.body.addEventListener("htmx:beforeSend", rememberDaySaveInFlight);
    document.body.addEventListener("htmx:afterRequest", forgetDaySaveInFlight);
    // Decides the prompt, sends nothing: the owner may yet stay.
    window.addEventListener("beforeunload", function (event) {
      var forms = daySaveFormsOwnedByUnloadGuard();
      for (var index = 0; index < forms.length; index++) {
        if (forms[index].__ovumcyDaySaveInFlight) {
          warnBeforeLeavingDuringSave(event);
          return;
        }
      }
    });
    window.addEventListener("pagehide", function () {
      var forms = daySaveFormsOwnedByUnloadGuard();
      for (var index = 0; index < forms.length; index++) {
        resendDaySaveOnPageHide(forms[index]);
      }
    });
  }

  function bindDashboardEditors() {
    var roots = document.querySelectorAll("[data-dashboard-editor]");
    for (var index = 0; index < roots.length; index++) {
      var root = roots[index];
      var form = root.querySelector("[data-dashboard-save-form]");
      if (root.dataset.dashboardEditorBound !== "1") {
        root.dataset.dashboardEditorBound = "1";

        root.addEventListener("change", function (event) {
          var currentForm = this.querySelector("[data-dashboard-save-form]");
          var periodToggle = event.target && event.target.matches && event.target.matches("[data-period-toggle]") ? event.target : null;
          if (periodToggle || (event.target && (event.target.name === "symptom_ids" || event.target.name === "mood"))) {
            syncPeriodToggleState(this);
          }
          if (periodToggle && periodToggle.checked) {
            maybeAcknowledgePeriodTip(this);
          }
          if (currentForm && event.target && event.target.name !== "csrf_token") {
            markDashboardAutosaveDirty(currentForm);
          }
        });

        root.addEventListener("input", function (event) {
          var currentForm = this.querySelector("[data-dashboard-save-form]");
          if (event.target && event.target.matches && event.target.matches("[data-dashboard-notes]")) {
            syncPeriodToggleState(this);
            syncNoteDisclosure(this);
          }
          if (currentForm && event.target && event.target.name !== "csrf_token") {
            markDashboardAutosaveDirty(currentForm);
          }
        });

        root.addEventListener("click", function (event) {
          var actionButton = closestFromEvent(event, "[data-quick-action]");
          var cycleStartButton = closestFromEvent(event, "[data-dashboard-cycle-start-button]");
          var undoButton = closestFromEvent(event, "[data-dashboard-autosave-undo]");
          if (undoButton && this.contains(undoButton)) {
            event.preventDefault();
            runDashboardUndo(this.querySelector("[data-dashboard-save-form]"));
            return;
          }
          if (actionButton && this.contains(actionButton)) {
            event.preventDefault();
            handleDashboardQuickAction(this, actionButton.getAttribute("data-quick-action"));
            return;
          }
          if (cycleStartButton && this.contains(cycleStartButton)) {
            maybeAcknowledgePeriodTip(cycleStartButton.form || this);
          }
        });

        if (form) {
          form.addEventListener("submit", function () {
            clearDashboardAutosaveTimers(this);
          });
        }
      }

      bindNoteDisclosures(root);
      bindAutosizeNoteFields(root);
      revealOnceTips(root);
      syncPeriodToggleState(root);
      syncNoteDisclosure(root);
      captureDashboardPersistedState(form);
      setDashboardAutosaveIndicator(form, "idle");
    }

    bindDashboardAutosaveBeforeUnload();
  }

  function syncDayEditorForm(form) {
    var periodToggle = form.querySelector("[data-period-toggle]");
    var isPeriod = !!(periodToggle && periodToggle.checked);
    syncPeriodFieldsets(form, isPeriod);
    syncPeriodToggleLabels(form, isPeriod);
    syncNoteDisclosure(form);
  }

  function bindDayEditorForms() {
    var forms = document.querySelectorAll("[data-day-editor-form]");
    for (var index = 0; index < forms.length; index++) {
      var form = forms[index];
      if (form.dataset.dayEditorBound !== "1") {
        form.dataset.dayEditorBound = "1";

        form.addEventListener("change", function (event) {
          if (!event.target || !event.target.matches || !event.target.matches("[data-period-toggle]")) {
            return;
          }

          if (event.target.checked) {
            maybeAcknowledgePeriodTip(this);
          }
          syncDayEditorForm(this);
        });

        form.addEventListener("click", function (event) {
          var cycleStartButton = closestFromEvent(event, "[data-day-cycle-start-button]");
          if (!cycleStartButton || !this.contains(cycleStartButton)) {
            return;
          }
          maybeAcknowledgePeriodTip(cycleStartButton.form || this);
        });
      }

      bindNoteDisclosures(form);
      bindAutosizeNoteFields(form);
      revealOnceTips(form);
      syncDayEditorForm(form);
    }

    bindDaySaveUnloadGuard();
  }

