  function createContext(section) {
    var locale = browserLocaleFromLanguage(document.documentElement.getAttribute("lang") || "");
    var monthFormatter = new Intl.DateTimeFormat(locale, { month: "long", year: "numeric" });
    var weekdayFormatter = new Intl.DateTimeFormat(locale, { weekday: "short" });
    var monthNameFormatter = new Intl.DateTimeFormat(locale, { month: "long" });
    var summaryDateFormatter = new Intl.DateTimeFormat(locale, { year: "numeric", month: "short", day: "numeric" });

    var context = {
      section: section,
      rawMinDate: readTextAttribute(section, "data-export-min", ""),
      rawMaxDate: readTextAttribute(section, "data-export-max", ""),
      successMessage: readTextAttribute(section, "data-export-success", "Data exported successfully"),
      failedMessage: readTextAttribute(section, "data-export-failed", "Failed to export data"),
      invalidRangeMessage: readTextAttribute(section, "data-export-invalid-range", "End date must be on or after start date"),
      invalidDateMessage: readTextAttribute(section, "data-export-invalid-date", "Use a valid date"),
      jumpTitle: readTextAttribute(section, "data-export-jump-title", "Choose month and year"),
      summaryTotalTemplate: readTextAttribute(section, "data-export-summary-total-template", "Total entries: %d"),
      summaryRangeTemplate: readTextAttribute(section, "data-export-summary-range-template", "Date range: %s to %s"),
      summaryRangeEmpty: readTextAttribute(section, "data-export-summary-range-empty", "Date range: -"),
      actions: section.querySelectorAll("button[data-export-action]"),
      presetButtons: section.querySelectorAll("button[data-export-preset]"),
      fromInput: section.querySelector("#export-from"),
      toInput: section.querySelector("#export-to"),
      fromField: dateFieldController(section.querySelector("#export-from")),
      toField: dateFieldController(section.querySelector("#export-to")),
      summaryTotalNode: section.querySelector("[data-export-summary-total]"),
      summaryRangeNode: section.querySelector("[data-export-summary-range]"),
      calendarPanel: section.querySelector("[data-export-calendar-panel]"),
      calendarTitle: section.querySelector("[data-export-calendar-title]"),
      calendarTitleToggle: section.querySelector("[data-export-calendar-title-toggle]"),
      calendarActive: section.querySelector("[data-export-calendar-active]"),
      calendarJump: section.querySelector("[data-export-calendar-jump]"),
      calendarMonth: section.querySelector("[data-export-calendar-month]"),
      calendarYear: section.querySelector("[data-export-calendar-year]"),
      calendarApply: section.querySelector("[data-export-calendar-apply]"),
      calendarWeekdays: section.querySelector("[data-export-calendar-weekdays]"),
      calendarDays: section.querySelector("[data-export-calendar-days]"),
      calendarPrev: section.querySelector("[data-export-calendar-prev]"),
      calendarNext: section.querySelector("[data-export-calendar-next]"),
      calendarClose: section.querySelector("[data-export-calendar-close]"),
      monthFormatter: monthFormatter,
      weekdayFormatter: weekdayFormatter,
      summaryDateFormatter: summaryDateFormatter,
      monthNames: buildMonthNames(monthNameFormatter)
    };

    if (!context.actions.length || !context.fromInput || !context.toInput) {
      return null;
    }
    return context;
  }

  function createDateRangeController(context, bounds) {
    function effectivePresetUpperBound() {
      if (!bounds.hasBounds) {
        return null;
      }

      var now = new Date();
      var browserToday = new Date(now.getFullYear(), now.getMonth(), now.getDate());
      if (dateKey(bounds.maxBound) < dateKey(browserToday)) {
        return cloneDate(bounds.maxBound);
      }
      return browserToday;
    }

    function clampDateToBounds(value) {
      if (!bounds.hasBounds || !value) {
        return value;
      }

      if (dateKey(value) < dateKey(bounds.minBound)) {
        return cloneDate(bounds.minBound);
      }
      if (dateKey(value) > dateKey(bounds.maxBound)) {
        return cloneDate(bounds.maxBound);
      }
      return cloneDate(value);
    }

    function setExportActionsDisabled(disabled) {
      for (var index = 0; index < context.actions.length; index++) {
        var action = context.actions[index];
        action.classList.toggle("export-link-disabled", disabled);
        setButtonDisabled(action, disabled);
      }
    }

    function parseAndNormalizeInput(field, input) {
      if (!validateDateField(field, input, context.invalidDateMessage)) {
        return { ok: false, date: null };
      }

      var raw = dateFieldValue(field, input);
      if (!raw) {
        clearDateFieldValidity(field, input);
        return { ok: true, date: null };
      }

      var parsed = parseISODate(raw);
      if (!parsed) {
        if (field && typeof field.setCustomValidity === "function") {
          field.setCustomValidity(context.invalidDateMessage);
        } else if (input) {
          input.setCustomValidity(context.invalidDateMessage);
        }
        return { ok: false, date: null };
      }

      setDateFieldValue(field, input, formatISODate(parsed));
      clearDateFieldValidity(field, input);
      return { ok: true, date: parsed };
    }

    function validate(changedSide) {
      var fromResult = parseAndNormalizeInput(context.fromField, context.fromInput);
      var toResult = parseAndNormalizeInput(context.toField, context.toInput);
      if (!fromResult.ok || !toResult.ok) {
        setExportActionsDisabled(true);
        return false;
      }

      var fromDate = fromResult.date;
      var toDate = toResult.date;

      if (!fromDate || !toDate) {
        clearDateFieldValidity(context.fromField, context.fromInput);
        clearDateFieldValidity(context.toField, context.toInput);
        setExportActionsDisabled(true);
        return false;
      }

      clearDateFieldValidity(context.fromField, context.fromInput);
      clearDateFieldValidity(context.toField, context.toInput);
      if (fromDate && toDate && dateKey(toDate) < dateKey(fromDate)) {
        var invalidField = changedSide === "from" ? context.fromField : context.toField;
        var invalidInput = changedSide === "from" ? context.fromInput : context.toInput;
        if (invalidField && typeof invalidField.setCustomValidity === "function") {
          invalidField.setCustomValidity(context.invalidRangeMessage);
        } else if (invalidInput) {
          invalidInput.setCustomValidity(context.invalidRangeMessage);
        }
        setExportActionsDisabled(true);
        return false;
      }

      setExportActionsDisabled(false);
      return true;
    }

    function computePresetRange(rawPreset) {
      if (!bounds.hasBounds) {
        return null;
      }

      var preset = String(rawPreset || "").trim().toLowerCase();
      if (preset === "all") {
        return { from: cloneDate(bounds.minBound), to: cloneDate(bounds.maxBound) };
      }

      var days = Number(preset);
      if (!Number.isFinite(days) || days < 1) {
        return null;
      }

      var toDate = effectivePresetUpperBound();
      if (!toDate) {
        return null;
      }
      var fromDate = new Date(toDate.getFullYear(), toDate.getMonth(), toDate.getDate() - days + 1);
      fromDate = clampDateToBounds(fromDate);
      toDate = clampDateToBounds(toDate);
      return { from: fromDate, to: toDate };
    }

    function updatePresetState() {
      if (!context.presetButtons.length) {
        return;
      }

      var fromDate = parseISODate(dateFieldValue(context.fromField, context.fromInput));
      var toDate = parseISODate(dateFieldValue(context.toField, context.toInput));

      for (var index = 0; index < context.presetButtons.length; index++) {
        var button = context.presetButtons[index];
        var presetValue = button.getAttribute("data-export-preset") || "";
        var range = computePresetRange(presetValue);
        var active = !!(range && fromDate && toDate && isSameDay(fromDate, range.from) && isSameDay(toDate, range.to));

        setButtonDisabled(button, !bounds.hasBounds);
        button.classList.toggle("btn-primary", active);
        button.classList.toggle("btn-soft", !active);
      }
    }

    function applyPreset(rawPreset) {
      var range = computePresetRange(rawPreset);
      if (!range) {
        return false;
      }
      setDateFieldValue(context.fromField, context.fromInput, formatISODate(range.from));
      setDateFieldValue(context.toField, context.toInput, formatISODate(range.to));
      validate("to");
      updatePresetState();
      return true;
    }

    function syncInitialRange() {
      if (!bounds.hasBounds) {
        return;
      }

      var fromValue = parseISODate(dateFieldValue(context.fromField, context.fromInput));
      var toValue = parseISODate(dateFieldValue(context.toField, context.toInput));
      fromValue = clampDateToBounds(fromValue || cloneDate(bounds.minBound));
      toValue = clampDateToBounds(toValue || cloneDate(bounds.maxBound));

      setDateFieldValue(context.fromField, context.fromInput, formatISODate(fromValue));
      setDateFieldValue(context.toField, context.toInput, formatISODate(toValue));
      validate("init");
    }

    return {
      setExportActionsDisabled: setExportActionsDisabled,
      validate: validate,
      updatePresetState: updatePresetState,
      applyPreset: applyPreset,
      syncInitialRange: syncInitialRange,
      buildExportRequestBody: function () {
        return buildExportRequestBody(
          dateFieldValue(context.fromField, context.fromInput),
          dateFieldValue(context.toField, context.toInput)
        );
      }
    };
  }
  function createSummaryController(context, bounds, rangeController) {
    var summaryTimer = 0;
    var summaryRequestID = 0;
    var lastSummaryBody = "";
    var summaryAbortController = null;

    function updateSummaryText(totalEntries, hasData, dateFrom, dateTo, selectedFrom, selectedTo) {
      if (context.summaryTotalNode) {
        context.summaryTotalNode.textContent = formatTemplate(context.summaryTotalTemplate, [Number(totalEntries) || 0]);
      }
      if (!context.summaryRangeNode) {
        return;
      }

      var selectedRangeFrom = String(selectedFrom || "").trim();
      var selectedRangeTo = String(selectedTo || "").trim();
      if (selectedRangeFrom && selectedRangeTo) {
        context.summaryRangeNode.textContent = formatTemplate(context.summaryRangeTemplate, [
          formatDateForDisplay(context.summaryDateFormatter, selectedRangeFrom),
          formatDateForDisplay(context.summaryDateFormatter, selectedRangeTo)
        ]);
        return;
      }

      if (hasData && dateFrom && dateTo) {
        context.summaryRangeNode.textContent = formatTemplate(context.summaryRangeTemplate, [
          formatDateForDisplay(context.summaryDateFormatter, dateFrom),
          formatDateForDisplay(context.summaryDateFormatter, dateTo)
        ]);
      } else {
        context.summaryRangeNode.textContent = context.summaryRangeEmpty;
      }
    }

    function buildSummaryRequestBody() {
      return rangeController.buildExportRequestBody().toString();
    }

    async function refresh() {
      if (!bounds.hasBounds) {
        return;
      }
      if (!rangeController.validate("summary")) {
        lastSummaryBody = "";
        return;
      }

      var requestBody = buildSummaryRequestBody();
      if (requestBody === lastSummaryBody) {
        return;
      }
      lastSummaryBody = requestBody;

      if (summaryAbortController) {
        summaryAbortController.abort();
      }
      summaryAbortController = typeof AbortController === "function" ? new AbortController() : null;

      var requestID = ++summaryRequestID;
      try {
        var response = await fetch(SUMMARY_ENDPOINT, {
          method: "POST",
          body: requestBody,
          credentials: "same-origin",
          headers: buildAcceptLanguageHeaders(),
          signal: summaryAbortController ? summaryAbortController.signal : undefined
        });
        if (!response.ok) {
          throw new Error("summary_failed");
        }

        var payload = await response.json();
        if (requestID !== summaryRequestID) {
          return;
        }
        updateSummaryText(
          payload.total_entries,
          payload.has_data,
          payload.date_from,
          payload.date_to,
          dateFieldValue(context.fromField, context.fromInput),
          dateFieldValue(context.toField, context.toInput)
        );
      } catch (error) {
        if (error && error.name === "AbortError") {
          return;
        }
        // Keep previous summary values if refresh fails.
      } finally {
        if (requestID === summaryRequestID) {
          summaryAbortController = null;
        }
      }
    }

    function scheduleRefresh() {
      if (!bounds.hasBounds) {
        return;
      }
      if (summaryTimer) {
        window.clearTimeout(summaryTimer);
      }
      summaryTimer = window.setTimeout(function () {
        refresh();
      }, SUMMARY_REFRESH_DELAY_MS);
    }

    return {
      scheduleRefresh: scheduleRefresh
    };
  }
