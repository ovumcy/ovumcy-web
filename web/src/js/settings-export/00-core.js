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
