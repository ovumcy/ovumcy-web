  function copyTextWithExecCommand(text) {
    return new Promise(function (resolve, reject) {
      var textarea = document.createElement("textarea");
      textarea.value = text;
      textarea.setAttribute("readonly", "readonly");
      textarea.className = "clipboard-helper";
      textarea.setAttribute("aria-hidden", "true");
      textarea.tabIndex = -1;
      document.body.appendChild(textarea);
      textarea.select();

      try {
        var copied = document.execCommand("copy");
        document.body.removeChild(textarea);
        if (copied) {
          resolve();
          return;
        }
      } catch {
        document.body.removeChild(textarea);
      }

      reject(new Error("copy_failed"));
    });
  }

  function writeTextToClipboard(text) {
    if (navigator.clipboard && typeof navigator.clipboard.writeText === "function") {
      return navigator.clipboard.writeText(text).catch(function () {
        return copyTextWithExecCommand(text);
      });
    }

    return copyTextWithExecCommand(text);
  }

  function setNodeHidden(node, hidden) {
    if (!node) {
      return;
    }
    if (hidden) {
      node.setAttribute("hidden", "");
      return;
    }
    node.removeAttribute("hidden");
  }

  function parseDateValue(value) {
    var normalized = String(value || "").trim();
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
      isNaN(parsed.getTime()) ||
      parsed.getFullYear() !== year ||
      parsed.getMonth() !== month ||
      parsed.getDate() !== day
    ) {
      return null;
    }
    return parsed;
  }

  function formatDateValue(value) {
    var year = value.getFullYear();
    var month = String(value.getMonth() + 1).padStart(2, "0");
    var day = String(value.getDate()).padStart(2, "0");
    return year + "-" + month + "-" + day;
  }

  function decodeResponseNoticeHeader(raw) {
    var value = String(raw || "").trim();
    if (!value) {
      return "";
    }

    try {
      return decodeURIComponent(value.replace(/\+/g, "%20")).trim();
    } catch {
      return value;
    }
  }

  function buildDayOptions(minDateRaw, maxDateRaw, locale, relativeLabels) {
    var minDate = parseDateValue(minDateRaw);
    var maxDate = parseDateValue(maxDateRaw);
    if (!minDate || !maxDate || minDate > maxDate) {
      return [];
    }

    var result = [];
    var formatter = new Intl.DateTimeFormat(locale || "en", {
      day: "numeric",
      month: "short"
    });

    for (var cursor = new Date(maxDate); cursor >= minDate; cursor.setDate(cursor.getDate() - 1)) {
      var current = new Date(cursor);
      var dayOffset = Math.round((maxDate.getTime() - current.getTime()) / 86400000);
      var isToday = dayOffset === 0;
      var relativeLabel = "";
      if (dayOffset === 0) {
        relativeLabel = String(relativeLabels && relativeLabels.today || "");
      } else if (dayOffset === 1) {
        relativeLabel = String(relativeLabels && relativeLabels.yesterday || "");
      } else if (dayOffset === 2) {
        relativeLabel = String(relativeLabels && relativeLabels.twoDaysAgo || "");
      }
      var formattedDate = formatter.format(current);
      result.push({
        value: formatDateValue(current),
        label: relativeLabel || formattedDate,
        secondaryLabel: relativeLabel ? formattedDate : "",
        isToday: isToday
      });
    }
    return result;
  }

  function sanitizeDateFieldDigits(raw, maxDigits) {
    return String(raw || "").replace(/\D/g, "").slice(0, maxDigits);
  }

  function padDateFieldSegment(raw, targetLength) {
    var sanitized = String(raw || "").trim();
    if (!sanitized) {
      return "";
    }
    return sanitized.length >= targetLength ? sanitized : sanitized.padStart(targetLength, "0");
  }

  function findDateFieldRoot(target) {
    if (!target) {
      return null;
    }
    if (target.matches && target.matches("[data-date-field]")) {
      return target;
    }
    return target.closest ? target.closest("[data-date-field]") : null;
  }

  function createLocalizedDateFieldController(root) {
    if (!root || !root.querySelector) {
      return null;
    }
    if (root.__ovumcyDateFieldController) {
      return root.__ovumcyDateFieldController;
    }

    var transportInput = root.querySelector("[data-date-field-value]");
    var dayInput = root.querySelector('[data-date-field-part="day"]');
    var monthInput = root.querySelector('[data-date-field-part="month"]');
    var yearInput = root.querySelector('[data-date-field-part="year"]');
    var openButton = root.querySelector("[data-date-field-open]");
    if (!transportInput || !dayInput || !monthInput || !yearInput) {
      return null;
    }

    var required = transportInput.getAttribute("data-date-field-required") === "true";
    var invalidMessage = String(root.getAttribute("data-date-field-invalid-message") || "Use a valid date.");
    var requiredMessage = String(root.getAttribute("data-date-field-required-message") || "Please enter a date.");
    var outOfRangeMessage = String(root.getAttribute("data-date-field-out-of-range-message") || "Choose a date in the allowed range.");
    var minDate = parseDateValue(transportInput.getAttribute("min") || "");
    var maxDate = parseDateValue(transportInput.getAttribute("max") || "");
    var currentValidationMessage = "";
    var syncingTransport = false;
    var syncingSegments = false;

    function setFieldValidation(message) {
      currentValidationMessage = String(message || "");
      dayInput.setCustomValidity(currentValidationMessage);
      monthInput.setCustomValidity(currentValidationMessage);
      yearInput.setCustomValidity(currentValidationMessage);
    }

    function readSegmentState() {
      var day = sanitizeDateFieldDigits(dayInput.value, 2);
      var month = sanitizeDateFieldDigits(monthInput.value, 2);
      var year = sanitizeDateFieldDigits(yearInput.value, 4);

      if (!day && !month && !year) {
        return {
          empty: true,
          valid: true,
          value: "",
          date: null
        };
      }

      if (day.length !== 2 || month.length !== 2 || year.length !== 4) {
        return {
          empty: false,
          valid: false,
          reason: "incomplete",
          value: ""
        };
      }

      var isoValue = year + "-" + month + "-" + day;
      var parsed = parseDateValue(isoValue);
      if (!parsed) {
        return {
          empty: false,
          valid: false,
          reason: "invalid",
          value: ""
        };
      }

      if ((minDate && parsed < minDate) || (maxDate && parsed > maxDate)) {
        return {
          empty: false,
          valid: false,
          reason: "out_of_range",
          value: isoValue,
          date: parsed
        };
      }

      return {
        empty: false,
        valid: true,
        value: isoValue,
        date: parsed
      };
    }

    function commitTransportValue(value, notify) {
      var nextValue = String(value || "");
      var changed = transportInput.value !== nextValue;
      transportInput.value = nextValue;
      if (notify && changed) {
        transportInput.dispatchEvent(new Event("input", { bubbles: true }));
        transportInput.dispatchEvent(new Event("change", { bubbles: true }));
      }
    }

    function syncSegmentsFromTransport() {
      if (syncingSegments) {
        return;
      }
      syncingTransport = true;
      var parsed = parseDateValue(transportInput.value);
      if (!parsed) {
        dayInput.value = "";
        monthInput.value = "";
        yearInput.value = "";
      } else {
        dayInput.value = String(parsed.getDate()).padStart(2, "0");
        monthInput.value = String(parsed.getMonth() + 1).padStart(2, "0");
        yearInput.value = String(parsed.getFullYear());
      }
      syncingTransport = false;
    }

    function syncTransportFromSegments(notify) {
      if (syncingTransport) {
        return;
      }

      syncingSegments = true;
      var state = readSegmentState();
      if (state.valid || state.reason === "out_of_range") {
        commitTransportValue(state.value, notify);
      } else {
        commitTransportValue("", notify);
      }
      syncingSegments = false;
    }

    function clearValidation() {
      setFieldValidation("");
    }

    function validate(options) {
      var state = readSegmentState();
      var resolvedInvalidMessage = options && options.invalidMessage ? String(options.invalidMessage) : invalidMessage;
      var resolvedRequiredMessage = options && options.requiredMessage ? String(options.requiredMessage) : requiredMessage;
      var resolvedOutOfRangeMessage = options && options.outOfRangeMessage ? String(options.outOfRangeMessage) : outOfRangeMessage;

      if (state.empty) {
        commitTransportValue("", false);
        if (required) {
          setFieldValidation(resolvedRequiredMessage);
          return false;
        }
        clearValidation();
        return true;
      }

      if (!state.valid) {
        if (state.reason === "out_of_range") {
          commitTransportValue(state.value, false);
          setFieldValidation(resolvedOutOfRangeMessage);
          return false;
        }

        commitTransportValue("", false);
        setFieldValidation(resolvedInvalidMessage);
        return false;
      }

      commitTransportValue(state.value, false);
      clearValidation();
      return true;
    }

    function focusFirstEditable() {
      var day = sanitizeDateFieldDigits(dayInput.value, 2);
      var month = sanitizeDateFieldDigits(monthInput.value, 2);
      var year = sanitizeDateFieldDigits(yearInput.value, 4);

      if (day.length < 2) {
        dayInput.focus();
        return dayInput;
      }
      if (month.length < 2) {
        monthInput.focus();
        return monthInput;
      }
      if (year.length < 4) {
        yearInput.focus();
        return yearInput;
      }
      dayInput.focus();
      return dayInput;
    }

    function reportValidity() {
      var target = focusFirstEditable();
      return target && typeof target.reportValidity === "function" ? target.reportValidity() : false;
    }

    function setSegmentValue(input, rawValue, maxDigits) {
      var nextValue = sanitizeDateFieldDigits(rawValue, maxDigits);
      if (input.value !== nextValue) {
        input.value = nextValue;
      }
    }

    function maybeAdvanceFocus(input, maxDigits, nextInput) {
      if (!nextInput) {
        return;
      }
      if (sanitizeDateFieldDigits(input.value, maxDigits).length === maxDigits && document.activeElement === input) {
        nextInput.focus();
        nextInput.select();
      }
    }

    function handleSegmentInput(input, maxDigits, nextInput) {
      return function () {
        setSegmentValue(input, input.value, maxDigits);
        clearValidation();
        syncTransportFromSegments(true);
        maybeAdvanceFocus(input, maxDigits, nextInput);
      };
    }

    function handleSegmentBlur(input, maxDigits) {
      return function () {
        var nextValue = sanitizeDateFieldDigits(input.value, maxDigits);
        if (maxDigits === 2 && nextValue.length === 1) {
          nextValue = padDateFieldSegment(nextValue, 2);
        }
        if (input.value !== nextValue) {
          input.value = nextValue;
        }
        syncTransportFromSegments(true);
      };
    }

    dayInput.addEventListener("input", handleSegmentInput(dayInput, 2, monthInput));
    monthInput.addEventListener("input", handleSegmentInput(monthInput, 2, yearInput));
    yearInput.addEventListener("input", handleSegmentInput(yearInput, 4, null));

    dayInput.addEventListener("blur", handleSegmentBlur(dayInput, 2));
    monthInput.addEventListener("blur", handleSegmentBlur(monthInput, 2));
    yearInput.addEventListener("blur", handleSegmentBlur(yearInput, 4));

    transportInput.addEventListener("input", function () {
      if (!syncingSegments) {
        clearValidation();
        syncSegmentsFromTransport();
      }
    });
    transportInput.addEventListener("change", function () {
      if (!syncingSegments) {
        clearValidation();
        syncSegmentsFromTransport();
      }
    });

    syncSegmentsFromTransport();
    clearValidation();

    root.__ovumcyDateFieldController = {
      root: root,
      input: transportInput,
      dayInput: dayInput,
      monthInput: monthInput,
      yearInput: yearInput,
      openButton: openButton,
      isCustom: true,
      getValue: function () {
        return String(transportInput.value || "");
      },
      setValue: function (value) {
        commitTransportValue(value, false);
        syncSegmentsFromTransport();
        clearValidation();
      },
      clear: function () {
        commitTransportValue("", false);
        syncSegmentsFromTransport();
        clearValidation();
      },
      readState: readSegmentState,
      validate: validate,
      validationMessage: function () {
        return currentValidationMessage;
      },
      setCustomValidity: setFieldValidation,
      reportValidity: reportValidity,
      focus: function () {
        focusFirstEditable();
      },
      setDisabled: function (disabled) {
        var nextDisabled = !!disabled;
        transportInput.disabled = nextDisabled;
        dayInput.disabled = nextDisabled;
        monthInput.disabled = nextDisabled;
        yearInput.disabled = nextDisabled;
        if (openButton) {
          openButton.disabled = nextDisabled;
          openButton.setAttribute("aria-disabled", nextDisabled ? "true" : "false");
        }
      }
    };

    return root.__ovumcyDateFieldController;
  }

  window.__ovumcyDecodeResponseNoticeHeader = decodeResponseNoticeHeader;

  function bindLocalizedDateFields(scope) {
    var root = scope && scope.querySelectorAll ? scope : document;
    var fields = root.querySelectorAll("[data-date-field]");
    for (var index = 0; index < fields.length; index++) {
      createLocalizedDateFieldController(fields[index]);
    }
  }

  function getLocalizedDateFieldController(target) {
    var root = findDateFieldRoot(target);
    if (!root) {
      return null;
    }
    return createLocalizedDateFieldController(root);
  }

  window.__ovumcyBindLocalizedDateFields = bindLocalizedDateFields;
  window.__ovumcyGetDateFieldController = getLocalizedDateFieldController;

  function fieldCharacterLength(value) {
    return Array.from(String(value || "")).length;
  }

  function getRecoveryCodeText(refs) {
    var node = refs && refs.code ? refs.code : null;
    return node ? String(node.textContent || "").trim() : "";
  }

  function collectCheckedSymptomLabels(scope) {
    if (!scope || !scope.querySelectorAll) {
      return [];
    }

    var checked = scope.querySelectorAll("input[name='symptom_ids']:checked");
    var labels = [];
    for (var index = 0; index < checked.length; index++) {
      var label = String(checked[index].dataset.symptomLabel || "").trim();
      if (label) {
        labels.push(label);
      }
    }
    return labels;
  }
