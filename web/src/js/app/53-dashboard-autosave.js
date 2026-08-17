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

  function runDashboardAutosave(form, keepalive, mode) {
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
    form.__ovumcyAutosaveInFlight = window.fetch(url, {
      method: method,
      credentials: "same-origin",
      keepalive: !!keepalive,
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
      return true;
    }).catch(function () {
      failDashboardAutosave(form, "");
      return false;
    }).finally(function () {
      form.__ovumcyAutosaveInFlight = null;
      form.__ovumcyAutosaveInFlightVersion = 0;
      if (form.dataset.autosaveDirty === "true" && !form.__ovumcyAutosaveFailed) {
        form.__ovumcyAutosaveTimer = window.setTimeout(function () {
          runDashboardAutosave(form, false);
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
      runDashboardAutosave(form, false);
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

  function finalizeDashboardManualSave(form, successful) {
    if (!form) {
      return;
    }
    clearDashboardAutosaveTimers(form);
    delete form.dataset.autosaveDirty;
    if (!successful) {
      setDashboardAutosaveIndicator(form, "idle");
      return;
    }
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
    return runDashboardAutosave(form, false);
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
    return runDashboardAutosave(form, false, "undo");
  }

  // The page going away is the last chance the newest journal value gets, and
  // the ordinary runner cannot take it: while a save is open it hands back that
  // pending promise, which carries the older body. An edit made in that window
  // bumps the version and queues nothing — the only thing that would ever send
  // it is the re-arm in the runner's `finally`, a 2 s timer no unload survives
  // (and one that would go out without `keepalive` besides). So a newer version
  // leaves on its own keepalive request, beside the one already on the wire.
  //
  // The day upsert is idempotent, so a version this flush already sent is not
  // taken off the dirty ledger: should the navigation be cancelled, the normal
  // path re-sending the same body costs nothing, while clearing dirty here
  // against a request whose outcome nobody will see could lose the edit twice.
  function flushDashboardAutosaveBeforeUnload(form) {
    var version;
    var endpoint;

    if (!form || form.dataset.autosaveDirty !== "true") {
      return;
    }
    if (!form.__ovumcyAutosaveInFlight) {
      runDashboardAutosave(form, true);
      return;
    }

    version = form.__ovumcyAutosaveVersion || 0;
    // The open request already carries this edit.
    if (version === (form.__ovumcyAutosaveInFlightVersion || 0)) {
      return;
    }
    // beforeunload fires again on every cancelled navigation: send once.
    if (version === (form.__ovumcyAutosaveUnloadFlushedVersion || 0)) {
      return;
    }
    // The same refusal the ordinary runner makes: a body it would not send is
    // not one to smuggle out on the unload path.
    if (!validateTemperatureInputs(form, false)) {
      return;
    }

    endpoint = dashboardAutosaveEndpoint(form);
    form.__ovumcyAutosaveUnloadFlushedVersion = version;
    window.fetch(endpoint.url, {
      method: endpoint.method,
      credentials: "same-origin",
      keepalive: true,
      headers: dashboardRequestHeaders(),
      body: buildDashboardAutosaveBody(form).toString()
    }).catch(function () {
      // The page is leaving; there is no surface left to report to.
    });
  }

  function bindDashboardAutosaveBeforeUnload() {
    if (document.body && document.body.dataset.dashboardAutosaveBeforeUnloadBound === "1") {
      return;
    }
    if (document.body) {
      document.body.dataset.dashboardAutosaveBeforeUnloadBound = "1";
    }

    window.addEventListener("beforeunload", function () {
      var forms = document.querySelectorAll("[data-dashboard-save-form]");
      for (var index = 0; index < forms.length; index++) {
        flushDashboardAutosaveBeforeUnload(forms[index]);
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
  }

