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
      var title;
      button.type = "button";
      button.className = "check-chip check-chip-sm justify-center onboarding-day-chip";
      button.setAttribute("data-onboarding-day-option", "true");
      button.setAttribute("data-onboarding-day-value", day.value);
      button.setAttribute("aria-pressed", state.selectedDate === day.value ? "true" : "false");
      title = day.secondaryLabel ? day.label + " " + day.secondaryLabel : day.label;
      button.setAttribute("title", title);
      if (day.isToday) {
        button.classList.add("onboarding-day-chip-today");
      }
      if (state.selectedDate === day.value) {
        button.classList.add("choice-chip-active");
      }
      if (day.secondaryLabel) {
        var primary = document.createElement("span");
        primary.className = "onboarding-day-chip-primary";
        primary.textContent = day.label;
        button.appendChild(primary);

        var secondary = document.createElement("span");
        secondary.className = "onboarding-day-chip-secondary";
        secondary.textContent = day.secondaryLabel;
        button.appendChild(secondary);
      } else {
        button.textContent = day.label;
      }
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
    state.periodLength = guidance.periodLength;

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
    setNodeHidden(state.stepTwoMessages.adjusted, !guidance.adjusted);
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
          relativeDayLabels: {
            today: String(root.getAttribute("data-today-label") || ""),
            yesterday: String(root.getAttribute("data-yesterday-label") || ""),
            twoDaysAgo: String(root.getAttribute("data-two-days-ago-label") || "")
          },
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
            adjusted: root.querySelector("[data-onboarding-step2-message='adjusted']"),
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
        state.dayOptions = buildDayOptions(state.minDate, state.maxDate, state.lang, state.relativeDayLabels);
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

