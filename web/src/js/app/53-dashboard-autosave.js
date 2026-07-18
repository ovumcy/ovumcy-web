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

  function setDashboardAutosaveIndicator(form, key) {
    var indicator = dashboardAutosaveIndicator(form);
    if (!indicator) {
      return;
    }
    indicator.textContent = dashboardAutosaveMessage(form, key, indicator.textContent);
    indicator.setAttribute("data-autosave-state", key);
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

  function runDashboardAutosave(form, keepalive) {
    var requestVersion;
    var url;
    var method;
    var headers;
    var body;
    var timezone;

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
    setDashboardAutosaveIndicator(form, "saving");

    requestVersion = form.__ovumcyAutosaveVersion || 0;
    // Pick the HTTP verb from whichever hx-* attribute the form uses so the
    // autosave fetch tracks the canonical REST verb declared in the template
    // (PUT for /api/v1/days/{date} upsert, falling back to POST / action for
    // legacy or non-HTMX forms).
    var hxVerbs = ["hx-put", "hx-patch", "hx-delete", "hx-post"];
    method = "POST";
    url = String(form.getAttribute("action") || "").trim();
    for (var verbIndex = 0; verbIndex < hxVerbs.length; verbIndex += 1) {
      var hxValue = form.getAttribute(hxVerbs[verbIndex]);
      if (hxValue) {
        method = hxVerbs[verbIndex].substring(3).toUpperCase();
        url = String(hxValue).trim();
        break;
      }
    }
    headers = {
      "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8",
      "HX-Request": "true"
    };
    body = buildDashboardAutosaveBody(form);
    timezone = currentClientTimezone();

    if (document.querySelector('meta[name="csrf-token"]')) {
      headers["X-CSRF-Token"] = document.querySelector('meta[name="csrf-token"]').getAttribute("content") || "";
    }
    if (timezone) {
      headers[TIMEZONE_HEADER_NAME] = timezone;
    }

    form.__ovumcyAutosaveInFlight = window.fetch(url, {
      method: method,
      credentials: "same-origin",
      keepalive: !!keepalive,
      headers: headers,
      body: body.toString()
    }).then(function (response) {
      if (!response.ok) {
        throw new Error("autosave_failed");
      }
      notifyAutosaveNotice(response);
      if ((form.__ovumcyAutosaveVersion || 0) === requestVersion) {
        delete form.dataset.autosaveDirty;
      }
      setDashboardAutosaveIndicator(form, "saved");
      scheduleDashboardAutosaveIdleReset(form);
      return true;
    }).catch(function () {
      setDashboardAutosaveIndicator(form, "error");
      scheduleDashboardAutosaveIdleReset(form);
      return false;
    }).finally(function () {
      form.__ovumcyAutosaveInFlight = null;
      if (form.dataset.autosaveDirty === "true") {
        form.__ovumcyAutosaveTimer = window.setTimeout(function () {
          runDashboardAutosave(form, false);
        }, 2000);
      }
    });

    return form.__ovumcyAutosaveInFlight;
  }

  function markDashboardAutosaveDirty(form) {
    if (!form) {
      return;
    }
    form.__ovumcyAutosaveVersion = (form.__ovumcyAutosaveVersion || 0) + 1;
    form.dataset.autosaveDirty = "true";
    if (form.__ovumcyAutosaveInFlight) {
      return;
    }
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
        if (forms[index].dataset.autosaveDirty === "true") {
          runDashboardAutosave(forms[index], true);
        }
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
            syncDashboardPreview(this);
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
            syncDashboardPreview(this);
            syncNoteDisclosure(this);
          }
          if (currentForm && event.target && event.target.name !== "csrf_token") {
            markDashboardAutosaveDirty(currentForm);
          }
        });

        root.addEventListener("click", function (event) {
          var actionButton = closestFromEvent(event, "[data-quick-action]");
          var cycleStartButton = closestFromEvent(event, "[data-dashboard-cycle-start-button]");
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
      syncDashboardPreview(root);
      syncNoteDisclosure(root);
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

