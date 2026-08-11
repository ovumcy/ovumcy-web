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

  function onboardingMonthKey(value) {
    return String(value.getFullYear()) + "-" + String(value.getMonth() + 1).padStart(2, "0");
  }

  function onboardingMonthStart(value) {
    return new Date(value.getFullYear(), value.getMonth(), 1);
  }

  function onboardingMonthOffset(value, months) {
    return new Date(value.getFullYear(), value.getMonth() + months, 1);
  }

  function onboardingMonthAllowed(state, month) {
    if (!state.minDate || !state.maxDate) {
      return false;
    }
    return month >= onboardingMonthStart(state.minDate) && month <= onboardingMonthStart(state.maxDate);
  }

  function clampOnboardingMonth(state, month) {
    if (!state.minDate || !state.maxDate) {
      return month;
    }
    if (month < onboardingMonthStart(state.minDate)) {
      return onboardingMonthStart(state.minDate);
    }
    if (month > onboardingMonthStart(state.maxDate)) {
      return onboardingMonthStart(state.maxDate);
    }
    return month;
  }

  function onboardingDateAllowed(state, value) {
    if (!value || !state.minDate || !state.maxDate) {
      return false;
    }
    return value >= state.minDate && value <= state.maxDate;
  }

  function renderOnboardingWeekdays(state) {
    if (!state.weekdaysContainer) {
      return;
    }

    state.weekdaysContainer.textContent = "";
    for (var weekday = 0; weekday < 7; weekday++) {
      // Jan 1 2023 was a Sunday, so 1 + weekday is Sunday-first; adding the
      // shift rotates the first column to Monday for a Monday-first owner.
      var sample = new Date(2023, 0, 1 + weekday + state.weekStartShift);
      var cell = document.createElement("span");
      cell.textContent = state.weekdayFormatter.format(sample);
      state.weekdaysContainer.appendChild(cell);
    }
  }

  function renderOnboardingDayOptions(state) {
    var container = state.dayOptionsContainer;
    if (!container || !state.visibleMonth) {
      return;
    }

    container.textContent = "";

    var year = state.visibleMonth.getFullYear();
    var month = state.visibleMonth.getMonth();
    // Leading blanks before day 1: the day-of-week index inside the owner's week.
    var firstWeekday = (new Date(year, month, 1).getDay() + 7 - state.weekStartShift) % 7;
    var daysInMonth = new Date(year, month + 1, 0).getDate();

    for (var blank = 0; blank < firstWeekday; blank++) {
      var placeholder = document.createElement("span");
      placeholder.className = "onboarding-day-blank";
      placeholder.setAttribute("aria-hidden", "true");
      container.appendChild(placeholder);
    }

    for (var day = 1; day <= daysInMonth; day++) {
      var dayDate = new Date(year, month, day);
      var value = formatDateValue(dayDate);
      var button = document.createElement("button");

      button.type = "button";
      button.className = "onboarding-day-cell";
      button.textContent = String(day);
      button.setAttribute("data-onboarding-day-option", "true");
      button.setAttribute("data-onboarding-day-value", value);
      button.setAttribute("aria-label", state.dayNameFormatter.format(dayDate));

      if (!onboardingDateAllowed(state, dayDate)) {
        // A period cannot start in the future, and the server accepts nothing
        // older than the window it published, so both edges are inert here.
        button.disabled = true;
        button.classList.add("onboarding-day-cell-disabled");
        button.setAttribute("aria-pressed", "false");
      } else {
        button.setAttribute("aria-pressed", state.selectedDate === value ? "true" : "false");
        if (state.selectedDate === value) {
          button.classList.add("onboarding-day-cell-selected");
        }
        if (state.maxDate && value === formatDateValue(state.maxDate)) {
          button.classList.add("onboarding-day-cell-today");
        }
      }

      container.appendChild(button);
    }
  }

  function syncOnboardingMonthNav(state) {
    if (state.monthTitle && state.visibleMonth) {
      state.monthTitle.textContent = state.monthFormatter.format(state.visibleMonth);
    }
    if (state.picker && state.visibleMonth) {
      state.picker.setAttribute("data-onboarding-visible-month", onboardingMonthKey(state.visibleMonth));
    }
    if (state.previousMonthButton) {
      state.previousMonthButton.disabled = !state.visibleMonth
        || !onboardingMonthAllowed(state, onboardingMonthOffset(state.visibleMonth, -1));
    }
    if (state.nextMonthButton) {
      state.nextMonthButton.disabled = !state.visibleMonth
        || !onboardingMonthAllowed(state, onboardingMonthOffset(state.visibleMonth, 1));
    }
  }

  function syncOnboardingShortcuts(state) {
    for (var index = 0; index < state.shortcutButtons.length; index++) {
      var button = state.shortcutButtons[index];
      var shortcutDate = onboardingShortcutDate(state, button.getAttribute("data-onboarding-shortcut"));
      button.disabled = !onboardingDateAllowed(state, shortcutDate);
      button.setAttribute(
        "aria-pressed",
        shortcutDate && state.selectedDate === formatDateValue(shortcutDate) ? "true" : "false"
      );
    }
  }

  function onboardingShortcutDate(state, shortcut) {
    if (!state.maxDate) {
      return null;
    }
    if (shortcut === "today") {
      return new Date(state.maxDate);
    }
    if (shortcut === "yesterday") {
      var yesterday = new Date(state.maxDate);
      yesterday.setDate(yesterday.getDate() - 1);
      return yesterday;
    }
    return null;
  }

  function syncOnboardingSelectedReadout(state) {
    if (!state.selectedReadout) {
      return;
    }

    var selected = parseDateValue(state.selectedDate);
    if (!selected) {
      state.selectedReadout.textContent = "";
      return;
    }
    state.selectedReadout.textContent = state.selectedLabel
      ? state.selectedLabel.replace("%s", state.dayNameFormatter.format(selected))
      : state.dayNameFormatter.format(selected);
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
    if (selectedDate && !onboardingDateAllowed(state, selectedDate)) {
      selectedDate = null;
    }

    state.selectedDate = selectedDate ? formatDateValue(selectedDate) : "";
    if (state.startDateInput) {
      state.startDateInput.value = state.selectedDate;
    }

    var reference = selectedDate || state.maxDate;
    if (!state.visibleMonth && reference) {
      state.visibleMonth = onboardingMonthStart(reference);
    }
    if (state.visibleMonth) {
      state.visibleMonth = clampOnboardingMonth(state, state.visibleMonth);
    }

    syncOnboardingMonthNav(state);
    syncOnboardingShortcuts(state);
    renderOnboardingDayOptions(state);
    syncOnboardingSelectedReadout(state);
  }

  function selectOnboardingDate(state, value) {
    var selected = parseDateValue(value);
    if (!onboardingDateAllowed(state, selected)) {
      return;
    }

    state.selectedDate = formatDateValue(selected);
    state.visibleMonth = onboardingMonthStart(selected);
    clearOnboardingStatus(state, "1");
    syncOnboardingStartDate(state);
  }

  function moveOnboardingMonth(state, step) {
    if (!state.visibleMonth) {
      return;
    }

    var target = onboardingMonthOffset(state.visibleMonth, step);
    if (!onboardingMonthAllowed(state, target)) {
      return;
    }

    state.visibleMonth = target;
    syncOnboardingMonthNav(state);
    renderOnboardingDayOptions(state);
  }

  function validateOnboardingStartDate(state) {
    var selected = parseDateValue(state.selectedDate);
    if (!selected) {
      return state.requiredMessage;
    }
    if (!onboardingDateAllowed(state, selected)) {
      return state.outOfRangeMessage;
    }
    return "";
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

  // Skipping the mode question answers nothing: every usage_goal radio is
  // cleared so the submit carries no value at all and the server applies the
  // neutral default. The skip control stays a submit button, so a browser
  // without this script still completes onboarding on the same default.
  function clearOnboardingUsageGoalChoice(root) {
    if (!root || !root.querySelectorAll) {
      return;
    }

    var choices = root.querySelectorAll("input[name='usage_goal']");
    for (var index = 0; index < choices.length; index++) {
      choices[index].checked = false;
    }
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
        var picker = root.querySelector("[data-onboarding-picker]");
        var lang = String(root.getAttribute("data-lang") || "en") || "en";

        state = {
          root: root,
          step: normalizeOnboardingStep(root.getAttribute("data-initial-step")),
          minDate: parseDateValue(root.getAttribute("data-min-date")),
          maxDate: parseDateValue(root.getAttribute("data-max-date")),
          selectedDate: String(root.getAttribute("data-last-period-start") || ""),
          cycleLength: clampInteger(root.getAttribute("data-cycle-length"), 28, 15, 90),
          periodLength: clampInteger(root.getAttribute("data-period-length"), 5, 1, 14),
          periodExceedsCycleMessage: String(root.getAttribute("data-period-exceeds-cycle-message") || "Period length must not exceed cycle length."),
          lang: lang,
          weekStartShift: root.getAttribute("data-week-start") === "monday" ? 1 : 0,
          monthFormatter: new Intl.DateTimeFormat(lang, { month: "long", year: "numeric" }),
          weekdayFormatter: new Intl.DateTimeFormat(lang, { weekday: "short" }),
          dayNameFormatter: new Intl.DateTimeFormat(lang, { day: "numeric", month: "long", year: "numeric" }),
          visibleMonth: null,
          progress: root.querySelector("[data-onboarding-progress]"),
          progressBar: root.querySelector("[data-onboarding-progress-bar]"),
          picker: picker,
          selectedLabel: picker ? String(picker.getAttribute("data-selected-label") || "") : "",
          requiredMessage: picker ? String(picker.getAttribute("data-required-message") || "") : "",
          outOfRangeMessage: picker ? String(picker.getAttribute("data-out-of-range-message") || "") : "",
          startDateInput: root.querySelector("[data-onboarding-start-date]"),
          monthTitle: root.querySelector("[data-onboarding-month-title]"),
          previousMonthButton: root.querySelector("[data-onboarding-month-prev]"),
          nextMonthButton: root.querySelector("[data-onboarding-month-next]"),
          weekdaysContainer: root.querySelector("[data-onboarding-weekdays]"),
          dayOptionsContainer: root.querySelector("[data-onboarding-day-options]"),
          selectedReadout: root.querySelector("[data-onboarding-selected-date]"),
          shortcutButtons: root.querySelectorAll("[data-onboarding-shortcut]"),
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
          }
        };
        root.__ovumcyOnboardingState = state;
        renderOnboardingWeekdays(state);

        root.addEventListener("click", function (event) {
          var currentState = this.__ovumcyOnboardingState;

          var skipUsageGoalButton = closestFromEvent(event, "[data-onboarding-usage-goal-skip]");
          if (skipUsageGoalButton && this.contains(skipUsageGoalButton)) {
            clearOnboardingUsageGoalChoice(this);
            return;
          }

          var stepButton = closestFromEvent(event, "[data-onboarding-go-step]");
          if (stepButton && this.contains(stepButton)) {
            goToOnboardingStep(currentState, stepButton.getAttribute("data-onboarding-go-step"));
            return;
          }

          var monthButton = closestFromEvent(event, "[data-onboarding-month-prev], [data-onboarding-month-next]");
          if (monthButton && this.contains(monthButton)) {
            moveOnboardingMonth(currentState, monthButton.hasAttribute("data-onboarding-month-next") ? 1 : -1);
            return;
          }

          var shortcutButton = closestFromEvent(event, "[data-onboarding-shortcut]");
          if (shortcutButton && this.contains(shortcutButton)) {
            var shortcutDate = onboardingShortcutDate(currentState, shortcutButton.getAttribute("data-onboarding-shortcut"));
            if (shortcutDate) {
              selectOnboardingDate(currentState, formatDateValue(shortcutDate));
            }
            return;
          }

          var dayButton = closestFromEvent(event, "button[data-onboarding-day-option]");
          if (dayButton && this.contains(dayButton)) {
            selectOnboardingDate(currentState, dayButton.getAttribute("data-onboarding-day-value"));
          }
        });

        root.addEventListener("input", function (event) {
          var currentState = this.__ovumcyOnboardingState;
          if (!event.target || !event.target.matches) {
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
            var startDateError = validateOnboardingStartDate(currentState);
            if (startDateError) {
              event.preventDefault();
              clearOnboardingStatus(currentState, "1");
              if (currentState.statusTargets["1"]) {
                renderErrorStatus(currentState.statusTargets["1"], startDateError);
              }
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
