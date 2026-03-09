  function createCalendarController(context, bounds, onRangeChanged) {
    var activeInput = null;
    var visibleMonth = null;

    populateMonthSelect(context.calendarMonth, context.monthNames);

    function inputLabel(input) {
      if (!input || !input.id) {
        return "";
      }
      var label = context.section.querySelector('label[for="' + input.id + '"]');
      if (!label) {
        return "";
      }
      return String(label.textContent || "").trim();
    }

    function renderWeekdayLabels() {
      if (!context.calendarWeekdays) {
        return;
      }

      context.calendarWeekdays.innerHTML = "";
      for (var weekday = 0; weekday < 7; weekday++) {
        var sample = new Date(2023, 0, 1 + weekday);
        var label = context.weekdayFormatter.format(sample).replace(/\./g, "");
        var cell = document.createElement("span");
        cell.textContent = label;
        context.calendarWeekdays.appendChild(cell);
      }
    }

    function closeCalendar() {
      if (!context.calendarPanel) {
        return;
      }
      context.calendarPanel.classList.add("hidden");
      if (context.calendarJump) {
        context.calendarJump.classList.add("hidden");
      }
      activeInput = null;
    }

    function toggleCalendarJump() {
      if (!context.calendarJump) {
        return;
      }
      context.calendarJump.classList.toggle("hidden");
      if (!context.calendarJump.classList.contains("hidden") && context.calendarYear) {
        context.calendarYear.focus();
      }
    }

    function syncJumpControls() {
      if (!visibleMonth) {
        return;
      }

      if (context.calendarYear) {
        if (bounds.hasBounds) {
          context.calendarYear.min = String(bounds.minBound.getFullYear());
          context.calendarYear.max = String(bounds.maxBound.getFullYear());
        } else {
          context.calendarYear.min = String(CALENDAR_MIN_YEAR);
          context.calendarYear.max = String(CALENDAR_MAX_YEAR);
        }
        context.calendarYear.value = String(visibleMonth.getFullYear());
      }

      if (context.calendarMonth) {
        context.calendarMonth.value = String(visibleMonth.getMonth());
        var jumpYear = visibleMonth.getFullYear();
        for (var monthOption = 0; monthOption < context.calendarMonth.options.length; monthOption++) {
          var option = context.calendarMonth.options[monthOption];
          option.disabled = bounds.hasBounds && !monthIntersectsBounds(bounds, new Date(jumpYear, monthOption, 1));
        }
      }

      if (context.calendarApply && context.calendarMonth && context.calendarYear) {
        var yearValue = Number(context.calendarYear.value);
        var monthValue = Number(context.calendarMonth.value);
        var candidate = new Date(yearValue, monthValue, 1);
        var invalidCandidate = Number.isNaN(yearValue) || Number.isNaN(monthValue);
        setButtonDisabled(context.calendarApply, invalidCandidate || (bounds.hasBounds && !monthIntersectsBounds(bounds, candidate)));
      }
    }

    function updateNavButtons() {
      if (!visibleMonth) {
        return;
      }

      if (context.calendarPrev) {
        var previousMonth = new Date(visibleMonth.getFullYear(), visibleMonth.getMonth() - 1, 1);
        setButtonDisabled(context.calendarPrev, bounds.hasBounds && !monthIntersectsBounds(bounds, previousMonth));
      }

      if (context.calendarNext) {
        var nextMonth = new Date(visibleMonth.getFullYear(), visibleMonth.getMonth() + 1, 1);
        setButtonDisabled(context.calendarNext, bounds.hasBounds && !monthIntersectsBounds(bounds, nextMonth));
      }
    }

    function renderCalendar() {
      if (!context.calendarPanel || !context.calendarTitle || !context.calendarDays || !activeInput || !visibleMonth) {
        return;
      }
      if (!bounds.hasBounds) {
        closeCalendar();
        return;
      }

      visibleMonth = clampMonthToBounds(bounds, visibleMonth);
      context.calendarPanel.classList.remove("hidden");
      context.calendarTitle.textContent = context.monthFormatter.format(visibleMonth);
      if (context.calendarActive) {
        context.calendarActive.textContent = inputLabel(activeInput);
      }

      renderWeekdayLabels();
      syncJumpControls();
      updateNavButtons();
      context.calendarDays.innerHTML = "";

      var year = visibleMonth.getFullYear();
      var month = visibleMonth.getMonth();
      var firstWeekday = new Date(year, month, 1).getDay();
      var daysInMonth = new Date(year, month + 1, 0).getDate();
      var activeField = activeInput === context.fromInput ? context.fromField : context.toField;
      var selectedDate = parseISODate(dateFieldValue(activeField, activeInput));

      for (var blank = 0; blank < firstWeekday; blank++) {
        var placeholder = document.createElement("span");
        placeholder.className = "h-2";
        context.calendarDays.appendChild(placeholder);
      }

      for (var day = 1; day <= daysInMonth; day++) {
        var dayDate = new Date(year, month, day);
        var button = document.createElement("button");
        button.type = "button";
        button.textContent = String(day);
        button.className = "btn-secondary text-sm export-calendar-day-button";

        var isAllowedDay = isWithinBounds(bounds, dayDate);
        if (!isAllowedDay) {
          button.disabled = true;
          button.className = "btn-soft text-sm export-calendar-day-button export-calendar-day-disabled";
        } else {
          (function (selectedDay) {
            button.addEventListener("click", function () {
              if (!activeInput) {
                return;
              }
              var nextValue = formatISODate(selectedDay);
              var field = activeInput === context.fromInput ? context.fromField : context.toField;
              setDateFieldValue(field, activeInput, nextValue);
              onRangeChanged(activeInput === context.toInput ? "to" : "from");
              closeCalendar();
            });
          })(dayDate);
        }

        if (selectedDate && isSameDay(selectedDate, dayDate)) {
          button.className = "btn-primary text-sm export-calendar-day-button";
        }

        context.calendarDays.appendChild(button);
      }
    }
    function openCalendarForInput(input) {
      if (!context.calendarPanel || !input || !bounds.hasBounds) {
        return;
      }
      if (activeInput === input && !context.calendarPanel.classList.contains("hidden")) {
        return;
      }

      activeInput = input;
      var field = input === context.fromInput ? context.fromField : context.toField;
      var selectedValue = parseISODate(dateFieldValue(field, input));
      var reference = selectedValue || cloneDate(bounds.maxBound);
      visibleMonth = clampMonthToBounds(bounds, reference);
      renderCalendar();
    }

    function moveMonth(step) {
      if (!visibleMonth || !activeInput) {
        return;
      }
      var targetMonth = new Date(visibleMonth.getFullYear(), visibleMonth.getMonth() + step, 1);
      if (bounds.hasBounds && !monthIntersectsBounds(bounds, targetMonth)) {
        return;
      }
      visibleMonth = targetMonth;
      renderCalendar();
    }

    function applyJumpSelection() {
      if (!context.calendarMonth || !context.calendarYear || !activeInput) {
        return;
      }

      var monthValue = Number(context.calendarMonth.value);
      var yearValue = Number(String(context.calendarYear.value || "").trim());
      if (Number.isNaN(monthValue) || monthValue < 0 || monthValue > 11) {
        return;
      }
      if (Number.isNaN(yearValue) || yearValue < CALENDAR_MIN_YEAR || yearValue > CALENDAR_MAX_YEAR) {
        return;
      }

      visibleMonth = clampMonthToBounds(bounds, new Date(yearValue, monthValue, 1));
      renderCalendar();
      if (context.calendarJump) {
        context.calendarJump.classList.add("hidden");
      }
    }

    function onYearKeydown(event) {
      if (event.key === "Enter" && context.calendarApply) {
        event.preventDefault();
        context.calendarApply.click();
      }
    }

    function disableControls() {
      setButtonDisabled(context.calendarPrev, true);
      setButtonDisabled(context.calendarNext, true);
      setButtonDisabled(context.calendarApply, true);
      setButtonDisabled(context.calendarTitleToggle, true);
    }

    return {
      closeCalendar: closeCalendar,
      toggleCalendarJump: toggleCalendarJump,
      syncJumpControls: syncJumpControls,
      openCalendarForInput: openCalendarForInput,
      moveMonth: moveMonth,
      applyJumpSelection: applyJumpSelection,
      onYearKeydown: onYearKeydown,
      disableControls: disableControls
    };
  }
