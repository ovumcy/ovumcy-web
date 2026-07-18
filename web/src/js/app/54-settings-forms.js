  function syncSettingsCycleForm(root) {
    var cycleInput = root.querySelector("[data-settings-cycle-length]");
    var periodInput = root.querySelector("[data-settings-period-length]");
    var cycleValue = root.querySelector("[data-settings-cycle-length-value]");
    var periodValue = root.querySelector("[data-settings-period-length-value]");
    var unpredictableInput = root.querySelector('input[name="unpredictable_cycle"]');
    if (!cycleInput || !periodInput) {
      return;
    }

    var cycleLength = clampInteger(cycleInput.value, 28, 15, 90);
    var periodLength = clampInteger(periodInput.value, 5, 1, 14);
    var guidance = cycleGuidanceState(cycleLength, periodLength);
    var showShortCycleWarning = guidance.cycleShort && !(unpredictableInput && unpredictableInput.checked);
    periodLength = guidance.periodLength;

    cycleInput.value = String(cycleLength);
    periodInput.value = String(periodLength);
    if (cycleValue) {
      cycleValue.textContent = String(cycleLength);
    }
    if (periodValue) {
      periodValue.textContent = String(periodLength);
    }

    setNodeHidden(root.querySelector("[data-settings-cycle-message='error']"), !guidance.invalid);
    setNodeHidden(root.querySelector("[data-settings-cycle-message='warning']"), !guidance.warning);
    setNodeHidden(root.querySelector("[data-settings-cycle-message='adjusted']"), !guidance.adjusted);
    setNodeHidden(root.querySelector("[data-settings-cycle-message='period-long']"), !guidance.periodLong);
    setNodeHidden(root.querySelector("[data-settings-cycle-message='cycle-short']"), !showShortCycleWarning);
  }

  function bindSettingsCycleForms() {
    var roots = document.querySelectorAll("[data-settings-cycle-form]");
    if (!roots.length) {
      return;
    }

    bindSettingsDraftLeaveGuard();

    for (var index = 0; index < roots.length; index++) {
      var root = roots[index];
      var draftForm = root.querySelector('form[data-settings-draft-form="cycle"]');
      var lastPeriodStartField = typeof window.__ovumcyGetDateFieldController === "function"
        ? window.__ovumcyGetDateFieldController(root.querySelector('[data-date-field-id="settings-last-period-start"], #settings-last-period-start'))
        : null;
      if (!draftForm) {
        continue;
      }

      if (root.dataset.settingsCycleBound !== "1") {
        root.dataset.settingsCycleBound = "1";
        draftForm.__ovumcySettingsDraftReset = function () {
          resetSettingsCycleDraft(this.closest("[data-settings-cycle-form]"));
        };

        root.addEventListener("input", function (event) {
          if (!event.target || !event.target.matches) {
            return;
          }
          if (event.target.matches("[data-settings-cycle-length], [data-settings-period-length], input[name='unpredictable_cycle']")) {
            syncSettingsCycleForm(this);
          }
          syncSettingsCycleDraftState(this);
        });

        root.addEventListener("change", function () {
          syncSettingsCycleDraftState(this);
        });

        root.addEventListener("submit", function (event) {
          var form = event.target;
          if (!form || !form.matches || !form.matches("form")) {
            return;
          }

          var cycleInput = this.querySelector("[data-settings-cycle-length]");
          var periodInput = this.querySelector("[data-settings-period-length]");
          var dateFieldController = typeof window.__ovumcyGetDateFieldController === "function"
            ? window.__ovumcyGetDateFieldController(this.querySelector('[data-date-field-id="settings-last-period-start"], #settings-last-period-start'))
            : null;
          if (dateFieldController && !dateFieldController.validate()) {
            event.preventDefault();
            dateFieldController.reportValidity();
            return;
          }

          var guidance = cycleGuidanceState(
            clampInteger(cycleInput ? cycleInput.value : 28, 28, 15, 90),
            clampInteger(periodInput ? periodInput.value : 5, 5, 1, 14)
          );
          if (guidance.invalid) {
            event.preventDefault();
            return;
          }

          setSettingsDraftTransition(form, true);
        });

        root.addEventListener("htmx:afterRequest", function (event) {
          var source = event && event.detail && event.detail.elt ? event.detail.elt : event.target;
          var form = source && source.matches && source.matches('form[data-settings-draft-form="cycle"]') ? source : null;
          var currentRoot = this;
          if (!form) {
            return;
          }

          if (event.detail && event.detail.successful) {
            commitSettingsDraftDefaults(form);
          } else {
            setSettingsDraftTransition(form, false);
          }
          window.setTimeout(function () {
            syncSettingsCycleForm(currentRoot);
            syncSettingsCycleDraftState(currentRoot);
          }, 0);
        });

        if (draftForm.querySelector("[data-settings-cycle-discard]")) {
          draftForm.querySelector("[data-settings-cycle-discard]").addEventListener("click", function () {
            resetSettingsCycleDraft(this.form.closest("[data-settings-cycle-form]"));
          });
        }
      }

      if (lastPeriodStartField) {
        lastPeriodStartField.validate();
      }
      syncSettingsCycleForm(root);
      syncSettingsCycleDraftState(root);
    }
  }

  function shouldTrackSettingsDraftControl(control) {
    var type;
    if (!control || !("name" in control)) {
      return false;
    }

    if (!String(control.name || "").trim() || String(control.name || "").trim() === "csrf_token") {
      return false;
    }
    if (control.matches && control.matches("[data-date-field-part]")) {
      return false;
    }

    type = String(control.type || "").toLowerCase();
    return type !== "submit" && type !== "button" && type !== "reset" && type !== "image" && type !== "file";
  }

  function isSettingsDraftFormDirty(form) {
    var controls;
    var control;
    var type;
    if (!form || !("elements" in form)) {
      return false;
    }

    controls = form.elements;
    for (var index = 0; index < controls.length; index++) {
      control = controls[index];
      if (!shouldTrackSettingsDraftControl(control)) {
        continue;
      }

      type = String(control.type || "").toLowerCase();
      if (type === "checkbox" || type === "radio") {
        if (!!control.checked !== !!control.defaultChecked) {
          return true;
        }
        continue;
      }

      if (String(control.value || "") !== String(control.defaultValue || "")) {
        return true;
      }
    }

    return false;
  }

  function commitSettingsDraftDefaults(form) {
    var controls;
    var control;
    var type;
    if (!form || !("elements" in form)) {
      return;
    }

    controls = form.elements;
    for (var index = 0; index < controls.length; index++) {
      control = controls[index];
      if (!shouldTrackSettingsDraftControl(control)) {
        continue;
      }

      type = String(control.type || "").toLowerCase();
      if (type === "checkbox" || type === "radio") {
        control.defaultChecked = !!control.checked;
        continue;
      }

      control.defaultValue = String(control.value || "");
    }
  }

  function syncSettingsDraftDateFields(scope) {
    var root = scope && scope.querySelectorAll ? scope : document;
    var fields = root.querySelectorAll("[data-date-field]");
    for (var index = 0; index < fields.length; index++) {
      var controller = typeof window.__ovumcyGetDateFieldController === "function"
        ? window.__ovumcyGetDateFieldController(fields[index])
        : null;
      if (controller && controller.input) {
        controller.setValue(String(controller.input.defaultValue || ""));
      }
    }
  }

  function syncSettingsDraftButton(button, enabled) {
    if (!button) {
      return;
    }

    button.disabled = !enabled;
    button.classList.toggle("btn--disabled", !enabled);
    button.setAttribute("aria-disabled", enabled ? "false" : "true");
  }

  function setSettingsDraftTransition(form, active) {
    if (!form) {
      return;
    }

    if (form.__ovumcySettingsDraftTransitionTimer) {
      window.clearTimeout(form.__ovumcySettingsDraftTransitionTimer);
      form.__ovumcySettingsDraftTransitionTimer = 0;
    }

    if (!active) {
      delete form.dataset.settingsDraftNavigating;
      return;
    }

    form.dataset.settingsDraftNavigating = "1";
    form.__ovumcySettingsDraftTransitionTimer = window.setTimeout(function () {
      delete form.dataset.settingsDraftNavigating;
      form.__ovumcySettingsDraftTransitionTimer = 0;
    }, 1500);
  }

  function dirtySettingsDraftForms() {
    var forms = document.querySelectorAll("form[data-settings-draft-form]");
    var dirty = [];
    for (var index = 0; index < forms.length; index++) {
      if (forms[index].dataset.settingsDraftDirty === "true" && forms[index].dataset.settingsDraftNavigating !== "1") {
        dirty.push(forms[index]);
      }
    }
    return dirty;
  }

  function firstDirtySettingsDraftForm() {
    var forms = dirtySettingsDraftForms();
    return forms.length ? forms[0] : null;
  }

  function confirmSettingsDraftDiscard(form, onAccept) {
    var message = String(form && form.getAttribute ? form.getAttribute("data-settings-unsaved-prompt") || "" : "");
    var acceptLabel = String(form && form.getAttribute ? form.getAttribute("data-settings-unsaved-accept") || "" : "");

    if (typeof window.__ovumcyOpenConfirm === "function") {
      window.__ovumcyOpenConfirm(message, acceptLabel).then(function (accepted) {
        if (accepted && typeof onAccept === "function") {
          onAccept();
        }
      });
      return;
    }

    if (window.confirm(message || "Leave without saving?") && typeof onAccept === "function") {
      onAccept();
    }
  }

  function shouldGuardSettingsDraftLink(link) {
    var href;
    var url;
    if (!link || !link.getAttribute) {
      return false;
    }
    if (link.getAttribute("target") === "_blank" || link.hasAttribute("download")) {
      return false;
    }

    href = String(link.getAttribute("href") || "").trim();
    if (!href || href.charAt(0) === "#") {
      return false;
    }

    try {
      url = new URL(link.href, window.location.href);
    } catch {
      return false;
    }

    if (url.origin !== window.location.origin) {
      return false;
    }
    if (url.pathname === window.location.pathname && url.search === window.location.search && url.hash) {
      return false;
    }
    return true;
  }

  function resetDirtySettingsDraftForms(forms) {
    var currentForms = forms || dirtySettingsDraftForms();
    for (var index = 0; index < currentForms.length; index++) {
      var form = currentForms[index];
      if (form && typeof form.__ovumcySettingsDraftReset === "function") {
        form.__ovumcySettingsDraftReset();
        continue;
      }
      if (form && typeof form.reset === "function") {
        form.reset();
        syncSettingsDraftDateFields(form);
        bindBinaryToggles(form);
      }
    }
  }

  function bindSettingsDraftLeaveGuard() {
    if (document.body.dataset.settingsDraftLeaveGuardBound === "1") {
      return;
    }
    document.body.dataset.settingsDraftLeaveGuardBound = "1";

    window.addEventListener("beforeunload", function (event) {
      if (!firstDirtySettingsDraftForm()) {
        return;
      }

      event.preventDefault();
      event.returnValue = "";
    });

    document.addEventListener("click", function (event) {
      var dirtyForms = dirtySettingsDraftForms();
      var dirtyForm = dirtyForms.length ? dirtyForms[0] : null;
      var link;
      if (!dirtyForm) {
        return;
      }
      if (event.defaultPrevented || !isPrimaryClick(event)) {
        return;
      }

      link = closestFromEvent(event, "a[href]");
      if (!link || !shouldGuardSettingsDraftLink(link)) {
        return;
      }

      event.preventDefault();
      confirmSettingsDraftDiscard(dirtyForm, function () {
        resetDirtySettingsDraftForms(dirtyForms);
        window.location.assign(link.href);
      });
    });

    document.addEventListener("submit", function (event) {
      var dirtyForms = dirtySettingsDraftForms();
      var dirtyForm = dirtyForms.length ? dirtyForms[0] : null;
      var targetForm = event.target;
      if (!dirtyForm || !targetForm || !targetForm.matches || !targetForm.matches("form")) {
        return;
      }
      if (targetForm.matches("form[data-settings-draft-form]")) {
        return;
      }

      event.preventDefault();
      confirmSettingsDraftDiscard(dirtyForm, function () {
        resetDirtySettingsDraftForms(dirtyForms);
        if (typeof targetForm.requestSubmit === "function") {
          targetForm.requestSubmit();
          return;
        }
        targetForm.submit();
      });
    }, true);
  }

  function syncSettingsCycleDraftState(root) {
    var form = root && root.querySelector ? root.querySelector('form[data-settings-draft-form="cycle"]') : null;
    var dirty = isSettingsDraftFormDirty(form);
    if (!form) {
      return;
    }

    form.dataset.settingsDraftDirty = dirty ? "true" : "false";
    syncSettingsDraftButton(form.querySelector("[data-settings-cycle-save]"), dirty);
    syncSettingsDraftButton(form.querySelector("[data-settings-cycle-discard]"), dirty);
    if (!dirty) {
      setSettingsDraftTransition(form, false);
    }
  }

  function resetSettingsCycleDraft(root) {
    var form = root && root.querySelector ? root.querySelector('form[data-settings-draft-form="cycle"]') : null;
    if (!root || !form) {
      return;
    }

    form.reset();
    syncSettingsDraftDateFields(form);
    bindBinaryToggles(root);
    syncSettingsCycleForm(root);
    syncSettingsCycleDraftState(root);
  }

  function syncSettingsTrackingDraftForm(form) {
    var dirty = isSettingsDraftFormDirty(form);
    if (!form) {
      return;
    }

    form.dataset.settingsDraftDirty = dirty ? "true" : "false";
    syncSettingsDraftButton(form.querySelector("[data-settings-tracking-save]"), dirty);
    syncSettingsDraftButton(form.querySelector("[data-settings-tracking-discard]"), dirty);
    if (!dirty) {
      setSettingsDraftTransition(form, false);
    }
  }

  function resetSettingsTrackingDraftForm(form) {
    if (!form || typeof form.reset !== "function") {
      return;
    }

    form.reset();
    bindBinaryToggles(form);
    syncSettingsTrackingDraftForm(form);
  }

  function bindSettingsTrackingForms() {
    var forms = document.querySelectorAll('form[data-settings-draft-form="tracking"]');
    if (!forms.length) {
      return;
    }

    bindSettingsDraftLeaveGuard();

    for (var index = 0; index < forms.length; index++) {
      var form = forms[index];
      if (form.dataset.settingsTrackingBound !== "1") {
        form.dataset.settingsTrackingBound = "1";
        form.__ovumcySettingsDraftReset = function () {
          resetSettingsTrackingDraftForm(this);
        };

        form.addEventListener("input", function () {
          syncSettingsTrackingDraftForm(this);
        });

        form.addEventListener("change", function () {
          syncSettingsTrackingDraftForm(this);
        });

        form.addEventListener("submit", function () {
          setSettingsDraftTransition(this, true);
        });

        form.addEventListener("htmx:afterRequest", function (event) {
          var source = event && event.detail && event.detail.elt ? event.detail.elt : event.target;
          var currentForm = this;
          if (!source || source !== this) {
            return;
          }

          if (event.detail && event.detail.successful) {
            commitSettingsDraftDefaults(this);
          } else {
            setSettingsDraftTransition(this, false);
          }
          window.setTimeout(function () {
            bindBinaryToggles(currentForm);
            syncSettingsTrackingDraftForm(currentForm);
          }, 0);
        });

        if (form.querySelector("[data-settings-tracking-discard]")) {
          form.querySelector("[data-settings-tracking-discard]").addEventListener("click", function () {
            resetSettingsTrackingDraftForm(this.form);
          });
        }
      }

      bindBinaryToggles(form);
      syncSettingsTrackingDraftForm(form);
    }
  }

