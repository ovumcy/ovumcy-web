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

  window.appShellState = function () {
    return {
      mobileMenu: false,
      theme: currentTheme(),
      themeIcon: "\u{1F319}",
      themeToggleLabel: "",
      themeShortLabel: "",
      init: function () {
        this.syncThemeState(this.theme);
      },
      toggleMobileMenu: function () {
        this.mobileMenu = !this.mobileMenu;
      },
      toggleTheme: function () {
        var nextTheme = toggleThemePreference();
        this.syncThemeState(nextTheme);
      },
      syncThemeState: function (rawTheme) {
        this.theme = normalizeTheme(rawTheme) || currentTheme();
        var messages = themeMessagesFromDataset();
        var isDark = this.theme === THEME_DARK;

        this.themeIcon = isDark ? "\u2600" : "\u{1F319}";
        this.themeShortLabel = isDark ? messages.modeLight : messages.modeDark;
        this.themeToggleLabel = isDark ? messages.toggleToLight : messages.toggleToDark;
      }
    };
  };

  window.settingsCycleForm = function (config) {
    var safeConfig = config || {};
    return {
      cycleLength: Number(safeConfig.cycleLength || 28),
      periodLength: Number(safeConfig.periodLength || 5),
      autoPeriodFill: !!safeConfig.autoPeriodFill
    };
  };

  function clearCheckedInputs(root, selector) {
    if (!root || !root.querySelectorAll) {
      return;
    }
    var inputs = root.querySelectorAll(selector);
    for (var index = 0; index < inputs.length; index++) {
      var input = inputs[index];
      input.checked = false;
      if (input.removeAttribute) {
        input.removeAttribute("checked");
      }
    }
  }

  window.dayEditorForm = function (config) {
    var safeConfig = config || {};
    return {
      isPeriod: !!safeConfig.isPeriod,
      clearNonPeriodSelections: function () {
        clearCheckedInputs(this.$root, "input[name='symptom_ids']");
      },
      init: function () {
        this.$watch("isPeriod", function (value) {
          if (!value) {
            this.clearNonPeriodSelections();
          }
        }.bind(this));
      }
    };
  };

  window.dashboardTodayEditor = function (config) {
    var safeConfig = config || {};

    return {
      isPeriod: !!safeConfig.isPeriod,
      activeSymptoms: [],
      notesPreview: "",
      syncSymptoms: function () {
        this.activeSymptoms = collectCheckedSymptomLabels(this.$root);
      },
      hasActiveSymptoms: function () {
        return this.activeSymptoms.length > 0;
      },
      hasNotesPreview: function () {
        return String(this.notesPreview || "").trim().length > 0;
      },
      clearNonPeriodSelections: function () {
        clearCheckedInputs(this.$root, "input[name='symptom_ids']");
        this.syncSymptoms();
      },
      init: function () {
        var notesField = this.$root ? this.$root.querySelector("#today-notes") : null;
        if (notesField) {
          this.notesPreview = String(notesField.defaultValue || notesField.value || "");
        }
        this.syncSymptoms();
        this.$watch("isPeriod", function (value) {
          if (!value) {
            this.clearNonPeriodSelections();
          }
        }.bind(this));
      }
    };
  };

  window.calendarView = function (config) {
    var safeConfig = config || {};
    return {
      selectedDate: safeConfig.selectedDate || "",
      isSelectedDay: function (value) {
        return this.selectedDate === String(value || "");
      },
      selectDayFromEvent: function (event) {
        var target = event && event.currentTarget ? event.currentTarget : null;
        if (!target || typeof target.getAttribute !== "function") {
          return;
        }
        this.selectDay(target.getAttribute("data-day"));
      },
      selectDay: function (value) {
        this.selectedDate = value || "";
        if (!this.selectedDate || !window.history || typeof window.history.replaceState !== "function") {
          return;
        }

        try {
          var currentURL = new URL(window.location.href);
          currentURL.searchParams.set("day", this.selectedDate);
          var nextPath = currentURL.pathname + currentURL.search + currentURL.hash;
          window.history.replaceState({}, "", nextPath);
        } catch {
          // Ignore malformed URLs and keep current location unchanged.
        }
      }
    };
  };

  window.onboardingFlow = function (config) {
    var safeConfig = config || {};
    var lang = safeConfig.lang || "en";
    var periodExceedsCycleMessage = String(
      safeConfig.periodExceedsCycleMessage || "Period length must not exceed cycle length."
    );
    function normalizeOnboardingStep(rawStep) {
      var step = Number(rawStep);
      if (!Number.isFinite(step)) {
        step = 0;
      }
      step = Math.round(step);
      if (step < 0) {
        return 0;
      }
      if (step > 3) {
        return 3;
      }
      return step;
    }

    return {
      step: normalizeOnboardingStep(safeConfig.initialStep),
      minDate: safeConfig.minDate || "",
      maxDate: safeConfig.maxDate || "",
      selectedDate: safeConfig.lastPeriodStart || "",
      cycleLength: Number(safeConfig.cycleLength || 28),
      periodLength: Number(safeConfig.periodLength || 5),
      autoPeriodFill: safeConfig.autoPeriodFill !== false,
      dayOptions: [],
      periodExceedsCycleMessage: periodExceedsCycleMessage,
      clearStepStatuses: function () {
        var statusIDs = ["onboarding-step1-status", "onboarding-step2-status", "onboarding-step3-status"];
        for (var index = 0; index < statusIDs.length; index++) {
          var node = document.getElementById(statusIDs[index]);
          if (node) {
            node.textContent = "";
          }
        }
      },
      clearStepStatus: function (statusID) {
        var node = document.getElementById(statusID);
        if (!node) {
          return;
        }
        node.textContent = "";
      },
      syncStepInURL: function () {
        if (!window.history || typeof window.history.replaceState !== "function") {
          return;
        }
        try {
          var currentURL = new URL(window.location.href);
          if (this.step > 0) {
            currentURL.searchParams.set("step", String(this.step));
          } else {
            currentURL.searchParams.delete("step");
          }
          var nextPath = currentURL.pathname + currentURL.search + currentURL.hash;
          var currentPath = window.location.pathname + window.location.search + window.location.hash;
          if (nextPath !== currentPath) {
            window.history.replaceState({}, "", nextPath);
          }
        } catch {
          // Ignore malformed URLs and keep current location unchanged.
        }
      },
      renderStepStatus: function (statusID, kind, message) {
        var node = document.getElementById(statusID);
        if (!node) {
          return;
        }
        node.textContent = "";
        if (!message) {
          return;
        }

        var status = document.createElement("div");
        status.className = kind;
        status.textContent = String(message);
        node.appendChild(status);
      },
      normalizeStepTwoValues: function () {
        var cycle = Number(this.cycleLength);
        if (!Number.isFinite(cycle)) {
          cycle = 28;
        }
        cycle = Math.max(15, Math.min(90, Math.round(cycle)));
        this.cycleLength = cycle;

        var period = Number(this.periodLength);
        if (!Number.isFinite(period)) {
          period = 5;
        }
        period = Math.max(1, Math.min(14, Math.round(period)));
        this.periodLength = period;
      },
      onCycleLengthChanged: function () {
        this.normalizeStepTwoValues();
        this.clearStepStatus("onboarding-step2-status");
      },
      onPeriodLengthChanged: function () {
        this.normalizeStepTwoValues();
        this.clearStepStatus("onboarding-step2-status");
      },
      validateStepTwoBeforeSubmit: function (event) {
        this.normalizeStepTwoValues();
        if ((this.cycleLength - this.periodLength) < 8) {
          if (event && typeof event.preventDefault === "function") {
            event.preventDefault();
          }
          this.renderStepStatus("onboarding-step2-status", "status-error", this.periodExceedsCycleMessage);
          return false;
        }
        this.clearStepStatus("onboarding-step2-status");
        return true;
      },
      normalizeStartDateWithinBounds: function () {
        var selected = parseDateValue(this.selectedDate);
        if (!selected) {
          this.selectedDate = "";
          return;
        }

        var min = parseDateValue(this.minDate);
        var max = parseDateValue(this.maxDate);

        if (min && selected < min) {
          this.selectedDate = formatDateValue(min);
          return;
        }
        if (max && selected > max) {
          this.selectedDate = formatDateValue(max);
          return;
        }

        this.selectedDate = formatDateValue(selected);
      },
      init: function () {
        this.step = normalizeOnboardingStep(this.step);
        this.syncStepInURL();
        this.dayOptions = buildDayOptions(this.minDate, this.maxDate, lang);
        this.normalizeStartDateWithinBounds();
        this.onStartDateChanged();
      },
      goToStep: function (value) {
        var nextStep = normalizeOnboardingStep(value);
        this.clearStepStatuses();
        this.step = nextStep;
        this.syncStepInURL();
      },
      begin: function () {
        this.goToStep(1);
      },
      onStepOneSaved: function (event) {
        this.advanceAfterSuccessfulRequest(event, 2);
      },
      onStepTwoSaved: function (event) {
        this.advanceAfterSuccessfulRequest(event, 3);
      },
      advanceAfterSuccessfulRequest: function (event, targetStep) {
        if (!event || !event.detail || !event.detail.successful) {
          return;
        }
        this.goToStep(targetStep);
      },
      setStartDate: function (value) {
        this.selectedDate = value || "";
        this.onStartDateChanged();
        this.clearStepStatus("onboarding-step1-status");
      },
      onStartDateChanged: function () {
        this.normalizeStartDateWithinBounds();
        this.clearStepStatus("onboarding-step1-status");
      }
    };
  };

  window.recoveryCodeTools = function () {
    return {
      copied: false,
      copyFailed: false,
      downloaded: false,
      downloadFailed: false,
      recoveryMessage: function (key, fallback) {
        var root = this.$root;
        if (root && root.dataset && root.dataset[key]) {
          return String(root.dataset[key] || "");
        }
        return String(fallback || "");
      },
      notify: function (key, fallback, kind) {
        var message = this.recoveryMessage(key, fallback);
        if (!message || typeof window.showToast !== "function") {
          return;
        }
        window.showToast(message, kind);
      },
      copyCode: function () {
        var code = getRecoveryCodeText(this.$refs);
        if (!code) {
          return;
        }

        var self = this;
        writeTextToClipboard(code).then(function () {
          self.copied = true;
          self.copyFailed = false;
          self.downloaded = false;
          self.downloadFailed = false;
          self.notify("copySuccessMessage", "Recovery code copied.", "ok");
          setTimedFlag(self, "copied", STATUS_CLEAR_MS);
        }).catch(function () {
          self.copied = false;
          self.copyFailed = true;
          self.downloaded = false;
          self.downloadFailed = false;
          self.notify("copyFailedMessage", "Failed to copy recovery code.", "error");
          setTimedFlag(self, "copyFailed", STATUS_CLEAR_MS);
        });
      },
      downloadCode: function () {
        var code = getRecoveryCodeText(this.$refs);
        if (!code) {
          return;
        }

        var self = this;
        this.copied = false;
        this.copyFailed = false;
        this.downloaded = false;
        this.downloadFailed = false;

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

          self.notify("downloadSuccessMessage", "Recovery code downloaded.", "ok");
          setTimedFlag(self, "downloaded", STATUS_CLEAR_MS);
        } catch {
          self.notify("downloadFailedMessage", "Failed to download recovery code.", "error");
          setTimedFlag(self, "downloadFailed", STATUS_CLEAR_MS);
        }
      }
    };
  };


