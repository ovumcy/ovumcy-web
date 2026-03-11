  function themeMessagesFromDataset() {
    var body = document.body;
    var dataset = body && body.dataset ? body.dataset : {};
    return {
      toggleToDark: String(dataset.themeLabelDark || "Switch to dark mode"),
      toggleToLight: String(dataset.themeLabelLight || "Switch to light mode"),
      modeDark: String(dataset.themeNameDark || "Dark"),
      modeLight: String(dataset.themeNameLight || "Light")
    };
  }

  function clampInteger(value, fallback, minValue, maxValue) {
    var numeric = Number(value);
    if (!isFinite(numeric)) {
      numeric = fallback;
    }
    numeric = Math.round(numeric);
    if (isFinite(minValue)) {
      numeric = Math.max(minValue, numeric);
    }
    if (isFinite(maxValue)) {
      numeric = Math.min(maxValue, numeric);
    }
    return numeric;
  }

  function cycleGuidanceState(cycleLength, periodLength) {
    var gap = cycleLength - periodLength;
    return {
      invalid: gap < 8,
      warning: gap >= 8 && gap < 15,
      periodLong: gap >= 15 && periodLength > 8,
      cycleShort: gap >= 15 && periodLength <= 8 && cycleLength < 24
    };
  }

  function setDisabledByPeriod(root, isPeriod) {
    if (!root || !root.querySelectorAll) {
      return;
    }

    var dependentInputs = root.querySelectorAll("[data-disable-without-period='true']");
    for (var index = 0; index < dependentInputs.length; index++) {
      dependentInputs[index].disabled = !isPeriod;
    }
  }

  function syncPeriodFieldsets(root, isPeriod) {
    if (!root || !root.querySelectorAll) {
      return;
    }

    var fieldsets = root.querySelectorAll("[data-period-fields]");
    for (var index = 0; index < fieldsets.length; index++) {
      setNodeHidden(fieldsets[index], !isPeriod);
    }
    setDisabledByPeriod(root, isPeriod);
  }

  function syncThemeToggleButtons() {
    var buttons = document.querySelectorAll("[data-theme-toggle]");
    var theme = currentTheme();
    var messages = themeMessagesFromDataset();
    var isDark = theme === THEME_DARK;
    var iconText = isDark ? "\u2600" : "\u{1F319}";
    var nextModeLabel = isDark ? messages.modeLight : messages.modeDark;
    var toggleLabel = isDark ? messages.toggleToLight : messages.toggleToDark;

    for (var index = 0; index < buttons.length; index++) {
      var button = buttons[index];
      var icon = button.querySelector("[data-theme-toggle-icon]");
      var text = button.querySelector("[data-theme-toggle-text]");

      button.setAttribute("aria-label", toggleLabel);
      button.setAttribute("title", toggleLabel);
      if (icon) {
        icon.textContent = iconText;
      }
      if (text) {
        text.textContent = nextModeLabel;
      }
    }
  }

  function bindThemeToggleButtons() {
    var buttons = document.querySelectorAll("[data-theme-toggle]");
    for (var index = 0; index < buttons.length; index++) {
      var button = buttons[index];
      if (button.dataset.themeToggleBound === "1") {
        continue;
      }

      button.dataset.themeToggleBound = "1";
      button.addEventListener("click", function () {
        toggleThemePreference();
        syncThemeToggleButtons();
      });
    }

    syncThemeToggleButtons();
  }

  function syncMobileMenu(button, menu) {
    var expanded = button.getAttribute("aria-expanded") === "true";
    setNodeHidden(menu, !expanded);
  }

  function bindMobileMenu() {
    var button = document.querySelector("[data-mobile-menu-toggle]");
    var menu = document.querySelector("[data-mobile-menu]");
    if (!button || !menu) {
      return;
    }

    if (button.dataset.mobileMenuBound !== "1") {
      button.dataset.mobileMenuBound = "1";
      button.addEventListener("click", function () {
        var expanded = button.getAttribute("aria-expanded") === "true";
        button.setAttribute("aria-expanded", expanded ? "false" : "true");
        syncMobileMenu(button, menu);
      });
    }

    syncMobileMenu(button, menu);
  }

  function syncPWAInstallBanner(banner, state) {
    var safeState = state || {};
    var visible = !!safeState.available && !safeState.installed;
    var mode = String(safeState.mode || "");
    var installButton = banner.querySelector("[data-pwa-install-action='install']");
    var promptCopy = banner.querySelector("[data-pwa-install-copy='prompt']");
    var iosCopy = banner.querySelector("[data-pwa-install-copy='ios']");
    var menuCopy = banner.querySelector("[data-pwa-install-copy='menu']");

    setNodeHidden(banner, !visible);
    if (!visible) {
      return;
    }

    if (installButton) {
      setNodeHidden(installButton, mode !== "prompt");
      installButton.disabled = !!safeState.busy;
    }

    setNodeHidden(promptCopy, mode !== "prompt");
    setNodeHidden(iosCopy, mode !== "ios");
    setNodeHidden(menuCopy, mode !== "menu");
  }

  function bindPWAInstallBanner() {
    var banner = document.querySelector("[data-pwa-install-banner]");
    if (!banner) {
      return;
    }

    if (banner.dataset.pwaInstallBound !== "1") {
      banner.dataset.pwaInstallBound = "1";

      var installButton = banner.querySelector("[data-pwa-install-action='install']");
      var dismissButton = banner.querySelector("[data-pwa-install-action='dismiss']");
      if (installButton) {
        installButton.addEventListener("click", function () {
          requestPWAInstallation();
        });
      }
      if (dismissButton) {
        dismissButton.addEventListener("click", function () {
          dismissPWAInstallPrompt();
        });
      }

      subscribePWAInstallState(function (state) {
        syncPWAInstallBanner(banner, state);
      });
    }
  }

  function syncDashboardPreview(root) {
    var periodToggle = root.querySelector("[data-period-toggle]");
    var notesField = root.querySelector("[data-dashboard-notes]");
    var preview = root.querySelector("[data-dashboard-preview]");
    var isPeriod = !!(periodToggle && periodToggle.checked);
    var notes = notesField ? String(notesField.value || "") : "";
    var trimmedNotes = notes.trim();
    var symptoms = collectCheckedSymptomLabels(root);
    var hasSymptoms = symptoms.length > 0;
    var hasNotes = trimmedNotes.length > 0;
    var showPreview = isPeriod || hasSymptoms || hasNotes;
    var symptomList = root.querySelector("[data-dashboard-symptom-list]");
    var symptomEmpty = root.querySelector("[data-dashboard-symptom-empty]");
    var notesValue = root.querySelector("[data-dashboard-notes-value]");
    var notesEmpty = root.querySelector("[data-dashboard-notes-empty]");

    syncPeriodFieldsets(root, isPeriod);

    if (!preview) {
      return;
    }

    setNodeHidden(preview, !showPreview);
    setNodeHidden(root.querySelector("[data-dashboard-preview-heading='period']"), !isPeriod);
    setNodeHidden(root.querySelector("[data-dashboard-preview-heading='other']"), isPeriod);
    setNodeHidden(root.querySelector("[data-dashboard-period-summary]"), !isPeriod);
    setNodeHidden(root.querySelector("[data-dashboard-other-summary]"), isPeriod);

    if (symptomList) {
      symptomList.textContent = "";
      for (var index = 0; index < symptoms.length; index++) {
        var item = document.createElement("li");
        item.textContent = symptoms[index];
        symptomList.appendChild(item);
      }
      setNodeHidden(symptomList, !hasSymptoms);
    }

    setNodeHidden(symptomEmpty, hasSymptoms);
    if (notesValue) {
      notesValue.textContent = notes;
      setNodeHidden(notesValue, !hasNotes);
    }
    setNodeHidden(notesEmpty, hasNotes);
  }

  function bindDashboardEditors() {
    var roots = document.querySelectorAll("[data-dashboard-editor]");
    for (var index = 0; index < roots.length; index++) {
      var root = roots[index];
      if (root.dataset.dashboardEditorBound !== "1") {
        root.dataset.dashboardEditorBound = "1";

        root.addEventListener("change", function (event) {
          var periodToggle = event.target && event.target.matches && event.target.matches("[data-period-toggle]") ? event.target : null;
          if (periodToggle || (event.target && event.target.name === "symptom_ids")) {
            syncDashboardPreview(this);
          }
        });

        root.addEventListener("input", function (event) {
          if (event.target && event.target.matches && event.target.matches("[data-dashboard-notes]")) {
            syncDashboardPreview(this);
          }
        });
      }

      syncDashboardPreview(root);
    }
  }

  function syncDayEditorForm(form) {
    var periodToggle = form.querySelector("[data-period-toggle]");
    syncPeriodFieldsets(form, !!(periodToggle && periodToggle.checked));
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

          syncDayEditorForm(this);
        });
      }

      syncDayEditorForm(form);
    }
  }

  function syncSettingsCycleForm(root) {
    var cycleInput = root.querySelector("[data-settings-cycle-length]");
    var periodInput = root.querySelector("[data-settings-period-length]");
    var cycleValue = root.querySelector("[data-settings-cycle-length-value]");
    var periodValue = root.querySelector("[data-settings-period-length-value]");
    if (!cycleInput || !periodInput) {
      return;
    }

    var cycleLength = clampInteger(cycleInput.value, 28, 15, 90);
    var periodLength = clampInteger(periodInput.value, 5, 1, 14);
    var guidance = cycleGuidanceState(cycleLength, periodLength);

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
    setNodeHidden(root.querySelector("[data-settings-cycle-message='period-long']"), !guidance.periodLong);
    setNodeHidden(root.querySelector("[data-settings-cycle-message='cycle-short']"), !guidance.cycleShort);
  }

  function bindSettingsCycleForms() {
    var roots = document.querySelectorAll("[data-settings-cycle-form]");
    for (var index = 0; index < roots.length; index++) {
      var root = roots[index];
      var lastPeriodStartField = typeof window.__ovumcyGetDateFieldController === "function"
        ? window.__ovumcyGetDateFieldController(root.querySelector('[data-date-field-id="settings-last-period-start"], #settings-last-period-start'))
        : null;
      if (root.dataset.settingsCycleBound !== "1") {
        root.dataset.settingsCycleBound = "1";

        root.addEventListener("input", function (event) {
          if (!event.target || !event.target.matches) {
            return;
          }
          if (event.target.matches("[data-settings-cycle-length], [data-settings-period-length]")) {
            syncSettingsCycleForm(this);
          }
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
          }
        });
      }

      if (lastPeriodStartField) {
        lastPeriodStartField.validate();
      }
      syncSettingsCycleForm(root);
    }
  }

  function syncIconOptionButtons(root, activeIcon) {
    if (!root || !root.querySelectorAll) {
      return;
    }

    var normalized = String(activeIcon || "").trim();
    var buttons = root.querySelectorAll("[data-icon-option]");
    for (var index = 0; index < buttons.length; index++) {
      var button = buttons[index];
      var selected = String(button.getAttribute("data-icon-option") || "") === normalized;
      button.setAttribute("aria-pressed", selected ? "true" : "false");
      button.setAttribute("data-selected", selected ? "true" : "false");
    }
  }

  function syncIconControl(root, nextValue) {
    if (!root || !root.querySelector) {
      return;
    }

    var valueInput = root.querySelector("[data-icon-value]");
    var normalized = String(nextValue || "").trim();
    if (!normalized && valueInput) {
      normalized = String(valueInput.value || "").trim();
    }
    if (!normalized) {
      normalized = "✨";
    }

    if (valueInput) {
      valueInput.value = normalized;
    }

    syncIconOptionButtons(root, normalized);
  }

  function bindIconControls() {
    var roots = document.querySelectorAll("[data-icon-control]");
    for (var index = 0; index < roots.length; index++) {
      var root = roots[index];
      if (root.dataset.iconControlBound !== "1") {
        root.dataset.iconControlBound = "1";

        root.addEventListener("click", function (event) {
          var button = closestFromEvent(event, "[data-icon-option]");
          if (!button || !this.contains(button)) {
            return;
          }

          event.preventDefault();
          syncIconControl(this, button.getAttribute("data-icon-option"));
        });
      }

      syncIconControl(root);
    }
  }

  function syncCalendarURL(selectedDate) {
    if (!window.history || typeof window.history.replaceState !== "function") {
      return;
    }

    try {
      var currentURL = new URL(window.location.href);
      if (selectedDate) {
        currentURL.searchParams.set("day", selectedDate);
      } else {
        currentURL.searchParams.delete("day");
      }
      var nextPath = currentURL.pathname + currentURL.search + currentURL.hash;
      window.history.replaceState({}, "", nextPath);
    } catch {
      // Ignore malformed URLs and keep current location unchanged.
    }
  }

  function syncCalendarSelection(root) {
    var selectedDate = String(root.getAttribute("data-selected-date") || "");
    var buttons = root.querySelectorAll("button[data-day]");

    for (var index = 0; index < buttons.length; index++) {
      buttons[index].classList.toggle("selected", buttons[index].getAttribute("data-day") === selectedDate);
    }
  }

  function bindCalendarViews() {
    var roots = document.querySelectorAll("[data-calendar-view]");
    for (var index = 0; index < roots.length; index++) {
      var root = roots[index];
      if (root.dataset.calendarViewBound !== "1") {
        root.dataset.calendarViewBound = "1";

        root.addEventListener("click", function (event) {
          var button = closestFromEvent(event, "button[data-day]");
          if (!button || !this.contains(button)) {
            return;
          }

          var selectedDate = String(button.getAttribute("data-day") || "");
          this.setAttribute("data-selected-date", selectedDate);
          syncCalendarSelection(this);
          syncCalendarURL(selectedDate);
        });
      }

      syncCalendarSelection(root);
    }
  }

  function normalizeOnboardingStep(rawStep) {
    return clampInteger(rawStep, 1, 1, 2);
  }

  function clearOnboardingStatus(state, stepKey) {
    var status = state.statusTargets[stepKey];
    if (status) {
      status.textContent = "";
    }
  }

  function clearAllOnboardingStatuses(state) {
    clearOnboardingStatus(state, "1");
    clearOnboardingStatus(state, "2");
  }

  function syncOnboardingURL(state) {
    if (!window.history || typeof window.history.replaceState !== "function") {
      return;
    }

    try {
      var currentURL = new URL(window.location.href);
      if (state.step > 1) {
        currentURL.searchParams.set("step", String(state.step));
      } else {
        currentURL.searchParams.delete("step");
      }
      var nextPath = currentURL.pathname + currentURL.search + currentURL.hash;
      if (nextPath !== (window.location.pathname + window.location.search + window.location.hash)) {
        window.history.replaceState({}, "", nextPath);
      }
    } catch {
      // Ignore malformed URLs and keep current location unchanged.
    }
  }

  function renderOnboardingDayOptions(state) {
    var container = state.dayOptionsContainer;
    if (!container) {
      return;
    }

    container.textContent = "";
    for (var index = 0; index < state.dayOptions.length; index++) {
      var day = state.dayOptions[index];
      var button = document.createElement("button");
      button.type = "button";
      button.className = "check-chip check-chip-sm justify-center";
      button.setAttribute("data-onboarding-day-option", "true");
      button.setAttribute("data-onboarding-day-value", day.value);
      if (state.selectedDate === day.value) {
        button.classList.add("choice-chip-active");
      }
      button.textContent = day.label;
      container.appendChild(button);
    }
  }

  function syncOnboardingStepUI(state) {
    setNodeHidden(state.progress, false);

    for (var panelStep = 1; panelStep <= 2; panelStep++) {
      setNodeHidden(state.panels[String(panelStep)], state.step !== panelStep);
    }
    for (var kickerStep = 1; kickerStep <= 2; kickerStep++) {
      setNodeHidden(state.progressKickers[String(kickerStep)], state.step !== kickerStep);
    }
    if (state.progressBar) {
      state.progressBar.setAttribute("data-step", String(state.step));
    }
  }

  function syncOnboardingStartDate(state) {
    var selectedDate = parseDateValue(state.selectedDate);
    var minDate = parseDateValue(state.minDate);
    var maxDate = parseDateValue(state.maxDate);

    if (selectedDate && minDate && selectedDate < minDate) {
      selectedDate = minDate;
    }
    if (selectedDate && maxDate && selectedDate > maxDate) {
      selectedDate = maxDate;
    }

    state.selectedDate = selectedDate ? formatDateValue(selectedDate) : "";
    if (state.startDateField) {
      state.startDateField.setValue(state.selectedDate);
    } else if (state.startDateInput) {
      state.startDateInput.value = state.selectedDate;
    }
    renderOnboardingDayOptions(state);
  }

  function syncOnboardingTimezoneFields(state) {
    if (!state || !state.timezoneFields) {
      return;
    }

    var timezone = currentClientTimezone();
    for (var index = 0; index < state.timezoneFields.length; index++) {
      state.timezoneFields[index].value = timezone;
    }
  }

  function syncOnboardingStepTwo(state) {
    var guidance;

    state.cycleLength = clampInteger(state.cycleLength, 28, 15, 90);
    state.periodLength = clampInteger(state.periodLength, 5, 1, 14);
    guidance = cycleGuidanceState(state.cycleLength, state.periodLength);

    if (state.cycleInput) {
      state.cycleInput.value = String(state.cycleLength);
    }
    if (state.periodInput) {
      state.periodInput.value = String(state.periodLength);
    }
    if (state.cycleValue) {
      state.cycleValue.textContent = String(state.cycleLength);
    }
    if (state.periodValue) {
      state.periodValue.textContent = String(state.periodLength);
    }

    setNodeHidden(state.stepTwoMessages.error, !guidance.invalid);
    setNodeHidden(state.stepTwoMessages.warning, !guidance.warning);
    setNodeHidden(state.stepTwoMessages.periodLong, !guidance.periodLong);
    setNodeHidden(state.stepTwoMessages.cycleShort, !guidance.cycleShort);

    if (state.stepTwoSubmit) {
      state.stepTwoSubmit.disabled = guidance.invalid;
      state.stepTwoSubmit.classList.toggle("btn--disabled", guidance.invalid);
    }

    return guidance;
  }

  function goToOnboardingStep(state, nextStep) {
    state.step = normalizeOnboardingStep(nextStep);
    clearAllOnboardingStatuses(state);
    syncOnboardingStepUI(state);
    syncOnboardingURL(state);
  }

  function bindOnboardingFlows() {
    var roots = document.querySelectorAll("[data-onboarding-flow]");
    for (var index = 0; index < roots.length; index++) {
      var root = roots[index];
      var state = root.__ovumcyOnboardingState;

      if (!state) {
        state = {
          root: root,
          step: normalizeOnboardingStep(root.getAttribute("data-initial-step")),
          minDate: String(root.getAttribute("data-min-date") || ""),
          maxDate: String(root.getAttribute("data-max-date") || ""),
          selectedDate: String(root.getAttribute("data-last-period-start") || ""),
          cycleLength: clampInteger(root.getAttribute("data-cycle-length"), 28, 15, 90),
          periodLength: clampInteger(root.getAttribute("data-period-length"), 5, 1, 14),
          periodExceedsCycleMessage: String(root.getAttribute("data-period-exceeds-cycle-message") || "Period length must not exceed cycle length."),
          lang: String(root.getAttribute("data-lang") || "en"),
          progress: root.querySelector("[data-onboarding-progress]"),
          progressBar: root.querySelector("[data-onboarding-progress-bar]"),
          startDateField: typeof window.__ovumcyGetDateFieldController === "function"
            ? window.__ovumcyGetDateFieldController(root.querySelector("#last-period-start"))
            : null,
          startDateInput: root.querySelector("#last-period-start"),
          dayOptionsContainer: root.querySelector("[data-onboarding-day-options]"),
          cycleInput: root.querySelector("[data-onboarding-cycle-length]"),
          periodInput: root.querySelector("[data-onboarding-period-length]"),
          cycleValue: root.querySelector("[data-onboarding-cycle-length-value]"),
          periodValue: root.querySelector("[data-onboarding-period-length-value]"),
          stepTwoSubmit: root.querySelector("[data-onboarding-step2-submit]"),
          panels: {
            "1": root.querySelector("[data-onboarding-panel='1']"),
            "2": root.querySelector("[data-onboarding-panel='2']")
          },
          progressKickers: {
            "1": root.querySelector("[data-onboarding-progress-kicker='1']"),
            "2": root.querySelector("[data-onboarding-progress-kicker='2']")
          },
          stepTwoMessages: {
            error: root.querySelector("[data-onboarding-step2-message='error']"),
            warning: root.querySelector("[data-onboarding-step2-message='warning']"),
            periodLong: root.querySelector("[data-onboarding-step2-message='period-long']"),
            cycleShort: root.querySelector("[data-onboarding-step2-message='cycle-short']")
          },
          timezoneFields: root.querySelectorAll("[data-onboarding-timezone-field]"),
          statusTargets: {
            "1": root.querySelector("#onboarding-step1-status"),
            "2": root.querySelector("#onboarding-step2-status")
          },
          dayOptions: []
        };
        state.dayOptions = buildDayOptions(state.minDate, state.maxDate, state.lang);
        root.__ovumcyOnboardingState = state;

        root.addEventListener("click", function (event) {
          var stepButton = closestFromEvent(event, "[data-onboarding-go-step]");
          if (stepButton && this.contains(stepButton)) {
            goToOnboardingStep(this.__ovumcyOnboardingState, stepButton.getAttribute("data-onboarding-go-step"));
            return;
          }

          var dayButton = closestFromEvent(event, "button[data-onboarding-day-option]");
          if (dayButton && this.contains(dayButton)) {
            this.__ovumcyOnboardingState.selectedDate = String(dayButton.getAttribute("data-onboarding-day-value") || "");
            clearOnboardingStatus(this.__ovumcyOnboardingState, "1");
            syncOnboardingStartDate(this.__ovumcyOnboardingState);
          }
        });

        root.addEventListener("input", function (event) {
          var currentState = this.__ovumcyOnboardingState;
          if (!event.target || !event.target.matches) {
            return;
          }

          if (currentState.startDateInput && event.target === currentState.startDateInput) {
            currentState.selectedDate = String(event.target.value || "");
            clearOnboardingStatus(currentState, "1");
            syncOnboardingStartDate(currentState);
            return;
          }

          if (event.target.matches("[data-onboarding-cycle-length]")) {
            currentState.cycleLength = event.target.value;
            clearOnboardingStatus(currentState, "2");
            syncOnboardingStepTwo(currentState);
            return;
          }

          if (event.target.matches("[data-onboarding-period-length]")) {
            currentState.periodLength = event.target.value;
            clearOnboardingStatus(currentState, "2");
            syncOnboardingStepTwo(currentState);
          }
        });

        root.addEventListener("submit", function (event) {
          var form = event.target;
          var currentState = this.__ovumcyOnboardingState;
          var guidance;
          if (form && form.matches && form.matches("form[data-onboarding-form-step='1']")) {
            syncOnboardingTimezoneFields(currentState);
            if (currentState.startDateField && !currentState.startDateField.validate()) {
              event.preventDefault();
              clearOnboardingStatus(currentState, "1");
              if (currentState.statusTargets["1"]) {
                renderErrorStatus(
                  currentState.statusTargets["1"],
                  currentState.startDateField.validationMessage()
                );
              }
              currentState.startDateField.reportValidity();
            }
            return;
          }

          if (!form || !form.matches || !form.matches("form[data-onboarding-form-step='2']")) {
            return;
          }

          guidance = syncOnboardingStepTwo(currentState);
          syncOnboardingTimezoneFields(currentState);
          if (!guidance.invalid) {
            clearOnboardingStatus(currentState, "2");
            return;
          }

          event.preventDefault();
          if (currentState.statusTargets["2"]) {
            renderErrorStatus(currentState.statusTargets["2"], currentState.periodExceedsCycleMessage);
          }
        });

        root.addEventListener("htmx:afterRequest", function (event) {
          var source = event && event.detail && event.detail.elt ? event.detail.elt : event.target;
          var form = source && source.matches && source.matches("form[data-onboarding-form-step]") ? source : null;
          if (!form || !event.detail || !event.detail.successful) {
            return;
          }

          switch (form.getAttribute("data-onboarding-form-step")) {
            case "1":
              goToOnboardingStep(this.__ovumcyOnboardingState, 2);
              break;
          }
        });
      }

      syncOnboardingStepUI(state);
      syncOnboardingURL(state);
      syncOnboardingTimezoneFields(state);
      syncOnboardingStartDate(state);
      syncOnboardingStepTwo(state);
    }
  }

  function clearRecoveryStatuses(root) {
    var nodes = root.querySelectorAll("[data-recovery-status]");
    for (var index = 0; index < nodes.length; index++) {
      setNodeHidden(nodes[index], true);
    }
  }

  function showRecoveryStatus(root, statusKey) {
    var nodes = root.querySelectorAll("[data-recovery-status]");
    for (var index = 0; index < nodes.length; index++) {
      var node = nodes[index];
      setNodeHidden(node, node.getAttribute("data-recovery-status") !== statusKey);
    }

    if (root.__ovumcyRecoveryTimer) {
      window.clearTimeout(root.__ovumcyRecoveryTimer);
    }
    root.__ovumcyRecoveryTimer = window.setTimeout(function () {
      clearRecoveryStatuses(root);
      root.__ovumcyRecoveryTimer = 0;
    }, STATUS_CLEAR_MS);
  }

  function recoveryMessage(root, key, fallback) {
    var dataset = root && root.dataset ? root.dataset : {};
    return String(dataset[key] || fallback || "");
  }

  function notifyRecovery(root, key, fallback, kind) {
    var message = recoveryMessage(root, key, fallback);
    if (message && typeof window.showToast === "function") {
      window.showToast(message, kind);
    }
  }

  function downloadRecoveryCode(root) {
    var code = getRecoveryCodeText({
      code: root.querySelector("[data-recovery-code-value]")
    });
    if (!code) {
      return;
    }

    try {
      var content = "Ovumcy recovery code\n\n" + code + "\n\nStore this code offline and private.";
      var blob = new Blob([content], { type: "text/plain;charset=utf-8" });
      var objectURL = URL.createObjectURL(blob);
      var link = document.createElement("a");
      link.href = objectURL;
      link.download = "ovumcy-recovery-code.txt";
      document.body.appendChild(link);
      link.click();
      link.remove();

      window.setTimeout(function () {
        URL.revokeObjectURL(objectURL);
      }, DOWNLOAD_REVOKE_MS);

      showRecoveryStatus(root, "downloaded");
      notifyRecovery(root, "downloadSuccessMessage", "Recovery code downloaded.", "ok");
    } catch {
      showRecoveryStatus(root, "download-failed");
      notifyRecovery(root, "downloadFailedMessage", "Failed to download recovery code.", "error");
    }
  }

  function bindRecoveryCodeTools() {
    var roots = document.querySelectorAll("[data-recovery-code-tools]");
    for (var index = 0; index < roots.length; index++) {
      var root = roots[index];
      if (root.dataset.recoveryCodeBound !== "1") {
        root.dataset.recoveryCodeBound = "1";

        root.addEventListener("click", function (event) {
          var actionButton = closestFromEvent(event, "[data-recovery-action]");
          var action;
          var code;
          var currentRoot = this;
          if (!actionButton || !this.contains(actionButton)) {
            return;
          }

          action = actionButton.getAttribute("data-recovery-action");
          if (action === "download") {
            downloadRecoveryCode(currentRoot);
            return;
          }
          if (action !== "copy") {
            return;
          }

          code = getRecoveryCodeText({
            code: currentRoot.querySelector("[data-recovery-code-value]")
          });
          if (!code) {
            return;
          }

          writeTextToClipboard(code).then(function () {
            showRecoveryStatus(currentRoot, "copied");
            notifyRecovery(currentRoot, "copySuccessMessage", "Recovery code copied.", "ok");
          }).catch(function () {
            showRecoveryStatus(currentRoot, "copy-failed");
            notifyRecovery(currentRoot, "copyFailedMessage", "Failed to copy recovery code.", "error");
          });
        });
      }

      clearRecoveryStatuses(root);
    }
  }

  function initCSPFriendlyComponents() {
    bindThemeToggleButtons();
    bindMobileMenu();
    bindPWAInstallBanner();
    if (typeof window.__ovumcyBindLocalizedDateFields === "function") {
      window.__ovumcyBindLocalizedDateFields(document);
    }
    bindSettingsCycleForms();
    bindIconControls();
    bindDashboardEditors();
    bindDayEditorForms();
    bindCalendarViews();
    bindOnboardingFlows();
    bindRecoveryCodeTools();
  }
