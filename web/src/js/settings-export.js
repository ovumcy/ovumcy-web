(function () {
  "use strict";

  var SUMMARY_ENDPOINT = "/api/export/summary";
  var SUMMARY_REFRESH_DELAY_MS = 160;
  var DOWNLOAD_REVOKE_DELAY_MS = 500;
  var CALENDAR_MIN_YEAR = 1900;
  var CALENDAR_MAX_YEAR = 2200;

  function readTextAttribute(node, name, fallback) {
    return node.getAttribute(name) || fallback;
  }

  function padNumber(value) {
    return value < 10 ? "0" + String(value) : String(value);
  }

  function formatISODate(value) {
    if (!value) {
      return "";
    }
    return [
      String(value.getFullYear()),
      padNumber(value.getMonth() + 1),
      padNumber(value.getDate())
    ].join("-");
  }

  function parseISODate(raw) {
    var normalized = String(raw || "").trim();
    if (!normalized) {
      return null;
    }

    var match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(normalized);
    if (!match) {
      return null;
    }

    var year = Number(match[1]);
    var month = Number(match[2]) - 1;
    var day = Number(match[3]);
    var parsed = new Date(year, month, day);

    if (
      parsed.getFullYear() !== year ||
      parsed.getMonth() !== month ||
      parsed.getDate() !== day
    ) {
      return null;
    }
    return parsed;
  }

  function sanitizeDateInputValue(input) {
    if (!input) {
      return;
    }

    var digits = String(input.value || "").replace(/\D/g, "").slice(0, 8);
    var year = digits.slice(0, 4);
    var month = digits.slice(4, 6);
    var day = digits.slice(6, 8);

    if (month.length === 2) {
      var monthNumber = Number(month);
      if (monthNumber < 1) {
        month = "01";
      } else if (monthNumber > 12) {
        month = "12";
      } else {
        month = monthNumber < 10 ? "0" + String(monthNumber) : String(monthNumber);
      }
    }

    if (day.length === 2) {
      var dayNumber = Number(day);
      if (dayNumber < 1) {
        day = "01";
      } else if (dayNumber > 31) {
        day = "31";
      } else {
        day = dayNumber < 10 ? "0" + String(dayNumber) : String(dayNumber);
      }
    }

    var normalized = year;
    if (month.length > 0) {
      normalized += "-" + month;
    }
    if (day.length > 0) {
      normalized += "-" + day;
    }

    if (normalized !== input.value) {
      input.value = normalized;
    }
  }

  function formatTemplate(template, values) {
    var index = 0;
    return String(template || "").replace(/%[sd]/g, function () {
      var value = index < values.length ? values[index] : "";
      index += 1;
      return String(value);
    });
  }

  function cloneDate(value) {
    return new Date(value.getFullYear(), value.getMonth(), value.getDate());
  }

  function formatDateForDisplay(formatter, rawISODate) {
    var parsed = parseISODate(rawISODate);
    if (!parsed) {
      return String(rawISODate || "").trim();
    }
    if (formatter && typeof formatter.format === "function") {
      return formatter.format(parsed);
    }
    return formatISODate(parsed);
  }

  function dateKey(value) {
    return Number(formatISODate(value).replace(/-/g, ""));
  }

  function toMonthStart(value) {
    return new Date(value.getFullYear(), value.getMonth(), 1);
  }

  function monthEnd(value) {
    return new Date(value.getFullYear(), value.getMonth() + 1, 0);
  }

  function isSameDay(left, right) {
    if (!left || !right) {
      return false;
    }
    return dateKey(left) === dateKey(right);
  }

  function setButtonDisabled(button, disabled) {
    if (!button) {
      return;
    }
    button.disabled = disabled;
    button.setAttribute("aria-disabled", disabled ? "true" : "false");
  }

  function readCSRFToken() {
    var tokenMeta = document.querySelector('meta[name="csrf-token"]');
    if (!tokenMeta) {
      return "";
    }
    return String(tokenMeta.getAttribute("content") || "").trim();
  }

  function buildExportRequestBody(fromValue, toValue) {
    var payload = new URLSearchParams();
    var csrfToken = readCSRFToken();
    if (csrfToken) {
      payload.set("csrf_token", csrfToken);
    }
    if (fromValue) {
      payload.set("from", fromValue);
    }
    if (toValue) {
      payload.set("to", toValue);
    }
    return payload;
  }

  function buildAcceptLanguageHeaders() {
    var headers = {};
    var currentLang = (document.documentElement.getAttribute("lang") || "").trim();
    if (currentLang) {
      headers["Accept-Language"] = currentLang;
    }
    headers["Content-Type"] = "application/x-www-form-urlencoded;charset=UTF-8";
    return headers;
  }

  function normalizeLanguageCode(raw) {
    if (!raw) {
      return "";
    }

    var normalized = String(raw).trim().toLowerCase().replace(/_/g, "-");
    if (!normalized) {
      return "";
    }
    if (normalized.indexOf("-") !== -1) {
      normalized = normalized.split("-")[0];
    }
    return normalized;
  }

  function browserLocaleFromLanguage(raw) {
    var normalized = normalizeLanguageCode(raw);
    if (normalized === "ru") {
      return "ru-RU";
    }
    if (normalized === "es") {
      return "es-ES";
    }
    if (normalized === "fr") {
      return "fr-FR";
    }
    return "en-US";
  }

  function dateFieldController(target) {
    if (typeof window.__ovumcyGetDateFieldController !== "function") {
      return null;
    }
    return window.__ovumcyGetDateFieldController(target);
  }

  function dateFieldValue(field, fallbackInput) {
    if (field && typeof field.getValue === "function") {
      return field.getValue();
    }
    return fallbackInput ? String(fallbackInput.value || "").trim() : "";
  }

  function setDateFieldValue(field, fallbackInput, value) {
    if (field && typeof field.setValue === "function") {
      field.setValue(value);
      return;
    }
    if (fallbackInput) {
      fallbackInput.value = String(value || "");
    }
  }

  function validateDateField(field, fallbackInput, invalidMessage) {
    if (field && typeof field.validate === "function") {
      return field.validate({
        invalidMessage: invalidMessage,
        outOfRangeMessage: invalidMessage
      });
    }

    if (!fallbackInput) {
      return true;
    }
    fallbackInput.setCustomValidity("");
    var raw = String(fallbackInput.value || "").trim();
    if (!raw) {
      return true;
    }
    if (!parseISODate(raw)) {
      fallbackInput.setCustomValidity(invalidMessage);
      return false;
    }
    return true;
  }

  function clearDateFieldValidity(field, fallbackInput) {
    if (field && typeof field.setCustomValidity === "function") {
      field.setCustomValidity("");
      return;
    }
    if (fallbackInput) {
      fallbackInput.setCustomValidity("");
    }
  }

  function dateFieldValidationMessage(field, fallbackInput) {
    if (field && typeof field.validationMessage === "function") {
      return field.validationMessage();
    }
    return fallbackInput ? String(fallbackInput.validationMessage || "") : "";
  }

  function reportDateFieldValidity(field, fallbackInput) {
    if (field && typeof field.reportValidity === "function") {
      return field.reportValidity();
    }
    if (fallbackInput && typeof fallbackInput.reportValidity === "function") {
      return fallbackInput.reportValidity();
    }
    return false;
  }

  function setDateFieldDisabled(field, fallbackInput, disabled) {
    if (field && typeof field.setDisabled === "function") {
      field.setDisabled(disabled);
      return;
    }
    if (fallbackInput) {
      fallbackInput.disabled = !!disabled;
    }
  }

  function parseFilenameFromDisposition(disposition, fallbackName) {
    if (!disposition) {
      return fallbackName;
    }
    var match = disposition.match(/filename\*?=(?:UTF-8'')?"?([^";]+)"?/i);
    if (!match || !match[1]) {
      return fallbackName;
    }
    try {
      return decodeURIComponent(match[1]);
    } catch {
      return match[1];
    }
  }

  function buildMonthNames(formatter) {
    var monthNames = [];
    for (var monthIndex = 0; monthIndex < 12; monthIndex++) {
      monthNames.push(formatter.format(new Date(2024, monthIndex, 1)));
    }
    return monthNames;
  }

  function populateMonthSelect(selectNode, monthNames) {
    if (!selectNode) {
      return;
    }
    selectNode.innerHTML = "";
    for (var index = 0; index < monthNames.length; index++) {
      var option = document.createElement("option");
      option.value = String(index);
      option.textContent = monthNames[index];
      selectNode.appendChild(option);
    }
  }

  function createBounds(rawMinDate, rawMaxDate) {
    var minBound = parseISODate(rawMinDate);
    var maxBound = parseISODate(rawMaxDate);
    var hasBounds = !!(minBound && maxBound && dateKey(minBound) <= dateKey(maxBound));
    return {
      minBound: minBound,
      maxBound: maxBound,
      hasBounds: hasBounds
    };
  }

  function isWithinBounds(bounds, value) {
    if (!bounds.hasBounds || !value) {
      return true;
    }
    var key = dateKey(value);
    return key >= dateKey(bounds.minBound) && key <= dateKey(bounds.maxBound);
  }

  function monthIntersectsBounds(bounds, monthValue) {
    if (!bounds.hasBounds) {
      return true;
    }
    var start = toMonthStart(monthValue);
    var end = monthEnd(monthValue);
    return dateKey(end) >= dateKey(bounds.minBound) && dateKey(start) <= dateKey(bounds.maxBound);
  }

  function clampMonthToBounds(bounds, monthValue) {
    if (!monthValue) {
      return bounds.hasBounds ? toMonthStart(bounds.maxBound) : toMonthStart(new Date());
    }
    var normalized = toMonthStart(monthValue);
    if (!bounds.hasBounds || monthIntersectsBounds(bounds, normalized)) {
      return normalized;
    }

    if (dateKey(normalized) < dateKey(toMonthStart(bounds.minBound))) {
      return toMonthStart(bounds.minBound);
    }
    return toMonthStart(bounds.maxBound);
  }

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

  function bindRangeInput(input, side, onRangeChanged) {
    input.addEventListener("input", function () {
      sanitizeDateInputValue(input);
      onRangeChanged(side);
    });
    input.addEventListener("blur", function () {
      onRangeChanged(side);
    });
  }

  function createExportHandler(context, rangeController) {
    return async function handleExport(event) {
      event.preventDefault();
      var action = event.currentTarget;
      var baseEndpoint = action.getAttribute("data-export-endpoint");
      if (!baseEndpoint) {
        return;
      }

      if (!rangeController.validate("export")) {
        var fromMessage = dateFieldValidationMessage(context.fromField, context.fromInput);
        var toMessage = dateFieldValidationMessage(context.toField, context.toInput);
        if (fromMessage) {
          reportDateFieldValidity(context.fromField, context.fromInput);
        } else if (toMessage) {
          reportDateFieldValidity(context.toField, context.toInput);
        }

        if (typeof window.showToast === "function") {
          var message = context.invalidRangeMessage;
          if (fromMessage) {
            message = fromMessage;
          } else if (toMessage) {
            message = toMessage;
          }
          window.showToast(message, "error");
        }
        return;
      }

      var requestBody = rangeController.buildExportRequestBody().toString();
      var type = (action.getAttribute("data-export-type") || "csv").toLowerCase();

      action.classList.add("btn-loading");
      setButtonDisabled(action, true);

      try {
        var response = await fetch(baseEndpoint, {
          method: "POST",
          body: requestBody,
          credentials: "same-origin",
          headers: buildAcceptLanguageHeaders()
        });
        if (!response.ok) {
          throw new Error("request_failed");
        }

        var blob = await response.blob();
        var extension = "csv";
        if (type === "json") {
          extension = "json";
        } else if (type === "pdf") {
          extension = "pdf";
        }
        var fallbackName = "ovumcy-export." + extension;
        var filename = parseFilenameFromDisposition(response.headers.get("Content-Disposition") || "", fallbackName);

        var objectURL = URL.createObjectURL(blob);
        var downloadLink = document.createElement("a");
        downloadLink.href = objectURL;
        downloadLink.download = filename;
        document.body.appendChild(downloadLink);
        downloadLink.click();
        downloadLink.remove();
        window.setTimeout(function () {
          URL.revokeObjectURL(objectURL);
        }, DOWNLOAD_REVOKE_DELAY_MS);

        if (typeof window.showToast === "function") {
          window.showToast(context.successMessage, "success");
        }
      } catch {
        if (typeof window.showToast === "function") {
          window.showToast(context.failedMessage, "error");
        }
      } finally {
        action.classList.remove("btn-loading");
        setButtonDisabled(action, false);
      }
    };
  }
  var section = document.querySelector("[data-export-section]");
  if (!section) {
    return;
  }

  var context = createContext(section);
  if (!context) {
    return;
  }

  var bounds = createBounds(context.rawMinDate, context.rawMaxDate);
  var rangeController = createDateRangeController(context, bounds);
  var summaryController = createSummaryController(context, bounds, rangeController);
  var useNativeDatePicker = context.fromInput.type === "date" && context.toInput.type === "date";

  function onRangeChanged(side) {
    rangeController.validate(side);
    rangeController.updatePresetState();
    summaryController.scheduleRefresh();
  }

  var calendarController = createCalendarController(context, bounds, onRangeChanged);

  if (context.calendarTitleToggle) {
    context.calendarTitleToggle.title = context.jumpTitle;
  }

  if (!bounds.hasBounds) {
    setDateFieldDisabled(context.fromField, context.fromInput, true);
    setDateFieldDisabled(context.toField, context.toInput, true);
    setDateFieldValue(context.fromField, context.fromInput, "");
    setDateFieldValue(context.toField, context.toInput, "");
    calendarController.disableControls();
    rangeController.updatePresetState();
    rangeController.setExportActionsDisabled(false);
  } else {
    setDateFieldDisabled(context.fromField, context.fromInput, false);
    setDateFieldDisabled(context.toField, context.toInput, false);
    rangeController.syncInitialRange();
    rangeController.updatePresetState();
    rangeController.setExportActionsDisabled(false);
    summaryController.scheduleRefresh();
  }

  bindRangeInput(context.fromInput, "from", onRangeChanged);
  bindRangeInput(context.toInput, "to", onRangeChanged);

  if (!useNativeDatePicker) {
    if (context.fromField && context.fromField.openButton) {
      context.fromField.openButton.addEventListener("click", function () {
        calendarController.openCalendarForInput(context.fromInput);
      });
    }
    if (context.toField && context.toField.openButton) {
      context.toField.openButton.addEventListener("click", function () {
        calendarController.openCalendarForInput(context.toInput);
      });
    }
  }

  for (var presetIndex = 0; presetIndex < context.presetButtons.length; presetIndex++) {
    (function (button) {
      button.addEventListener("click", function () {
        var presetValue = button.getAttribute("data-export-preset") || "";
        if (rangeController.applyPreset(presetValue)) {
          summaryController.scheduleRefresh();
        }
      });
    })(context.presetButtons[presetIndex]);
  }

  if (!useNativeDatePicker) {
    if (context.calendarTitleToggle) {
      context.calendarTitleToggle.addEventListener("click", calendarController.toggleCalendarJump);
    }
    if (context.calendarMonth) {
      context.calendarMonth.addEventListener("change", calendarController.syncJumpControls);
    }
    if (context.calendarYear) {
      context.calendarYear.addEventListener("input", calendarController.syncJumpControls);
      context.calendarYear.addEventListener("keydown", calendarController.onYearKeydown);
    }
    if (context.calendarPrev) {
      context.calendarPrev.addEventListener("click", function () {
        calendarController.moveMonth(-1);
      });
    }
    if (context.calendarNext) {
      context.calendarNext.addEventListener("click", function () {
        calendarController.moveMonth(1);
      });
    }
    if (context.calendarApply) {
      context.calendarApply.addEventListener("click", calendarController.applyJumpSelection);
    }
    if (context.calendarClose) {
      context.calendarClose.addEventListener("click", calendarController.closeCalendar);
    }

    document.addEventListener("click", function (event) {
      if (!context.calendarPanel || context.calendarPanel.classList.contains("hidden")) {
        return;
      }
      var target = event.target;
      if (!target) {
        return;
      }
      if (context.calendarPanel.contains(target)) {
        return;
      }
      if (target === context.fromInput || target === context.toInput) {
        return;
      }
      if (context.fromField && context.fromField.root && context.fromField.root.contains(target)) {
        return;
      }
      if (context.toField && context.toField.root && context.toField.root.contains(target)) {
        return;
      }
      calendarController.closeCalendar();
    });

    document.addEventListener("keydown", function (event) {
      if (event.key === "Escape") {
        calendarController.closeCalendar();
      }
    });
  } else if (context.calendarPanel) {
    context.calendarPanel.classList.add("hidden");
  }

  var handleExport = createExportHandler(context, rangeController);
  for (var actionIndex = 0; actionIndex < context.actions.length; actionIndex++) {
    context.actions[actionIndex].addEventListener("click", handleExport);
  }
})();
