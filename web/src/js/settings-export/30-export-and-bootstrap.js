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
