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
    var maxPeriodLength = Math.max(1, Math.min(14, cycleLength - 10));
    var safePeriodLength = Math.min(periodLength, maxPeriodLength);
    return {
      invalid: false,
      warning: false,
      adjusted: safePeriodLength !== periodLength,
      periodLength: safePeriodLength,
      periodLong: safePeriodLength > 8,
      cycleShort: cycleLength < 24
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
    var buttons = document.querySelectorAll("[data-theme-option]");
    var theme = currentTheme();
    var messages = themeMessagesFromDataset();

    for (var index = 0; index < buttons.length; index++) {
      var button = buttons[index];
      var optionTheme = normalizeTheme(button.getAttribute("data-theme-option"));
      var selected = optionTheme !== "" && optionTheme === theme;
      var toggleLabel = optionTheme === THEME_DARK ? messages.toggleToDark : messages.toggleToLight;
      var currentLabel = optionTheme === THEME_DARK ? messages.modeDark : messages.modeLight;

      button.dataset.selected = selected ? "true" : "false";
      button.setAttribute("aria-pressed", selected ? "true" : "false");
      button.setAttribute("aria-label", selected ? currentLabel : toggleLabel);
      button.setAttribute("title", selected ? currentLabel : toggleLabel);
    }
  }

  function bindThemeToggleButtons() {
    var buttons = document.querySelectorAll("[data-theme-option]");
    for (var index = 0; index < buttons.length; index++) {
      var button = buttons[index];
      if (button.dataset.themeToggleBound === "1") {
        continue;
      }

      button.dataset.themeToggleBound = "1";
      button.addEventListener("click", function () {
        var nextTheme = normalizeTheme(this.getAttribute("data-theme-option"));
        if (!nextTheme) {
          return;
        }
        setThemePreference(nextTheme);
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

  function syncBinaryToggleState(toggle) {
    if (!toggle || !toggle.querySelector) {
      return;
    }

    var input = toggle.querySelector("[data-binary-toggle-input]");
    var state = toggle.querySelector("[data-binary-toggle-state]");
    var active = !!(input && input.checked);

    toggle.setAttribute("data-active", active ? "true" : "false");
    if (!state) {
      return;
    }

    state.textContent = active
      ? String(state.getAttribute("data-state-on") || "")
      : String(state.getAttribute("data-state-off") || "");
  }

  function bindBinaryToggles(root) {
    var scope = root && root.querySelectorAll ? root : document;
    var toggles = scope.querySelectorAll("[data-binary-toggle]");

    for (var index = 0; index < toggles.length; index++) {
      var toggle = toggles[index];
      var input = toggle.querySelector("[data-binary-toggle-input]");
      if (!input) {
        continue;
      }

      if (toggle.dataset.binaryToggleBound !== "1") {
        toggle.dataset.binaryToggleBound = "1";
        (function (currentToggle, currentInput) {
          currentInput.addEventListener("change", function () {
            syncBinaryToggleState(currentToggle);
          });
        })(toggle, input);
      }

      syncBinaryToggleState(toggle);
    }
  }

  function symptomNameLength(value) {
    return Array.from(String(value || "")).length;
  }

  function syncSymptomNameCounter(field) {
    if (!field || !field.querySelector) {
      return;
    }

    var input = field.querySelector("[data-symptom-name-input]");
    var counter = field.querySelector("[data-symptom-name-count]");
    if (!input || !counter) {
      return;
    }

    var maxLength = parseInt(input.getAttribute("maxlength") || "", 10);
    var currentLength = symptomNameLength(input.value);
    if (maxLength > 0) {
      counter.textContent = String(currentLength) + "/" + String(maxLength);
      return;
    }

    counter.textContent = String(currentLength);
  }

  function bindSymptomNameCounters(root) {
    var scope = root && root.querySelectorAll ? root : document;
    var fields = scope.querySelectorAll("[data-symptom-name-count]");

    for (var index = 0; index < fields.length; index++) {
      var counter = fields[index];
      var field = typeof counter.closest === "function" ? counter.closest(".settings-symptom-name-field") : null;
      if (!field) {
        continue;
      }

      var input = field.querySelector("[data-symptom-name-input]");
      if (!input) {
        continue;
      }

      if (input.dataset.symptomNameCounterBound !== "1") {
        input.dataset.symptomNameCounterBound = "1";
        input.addEventListener("input", function () {
          var ownerField = typeof this.closest === "function" ? this.closest(".settings-symptom-name-field") : null;
          syncSymptomNameCounter(ownerField);
        });
      }

      syncSymptomNameCounter(field);
    }
  }

  function temperatureInputMaxLength(input) {
    var maxText = String(input.getAttribute("data-temperature-max") || "").trim();
    return Math.max(maxText.length, 5);
  }

  function normalizeTemperatureInputText(raw, maxLength) {
    var source = String(raw || "").replace(",", ".");
    var normalized = "";
    var dotSeen = false;

    for (var index = 0; index < source.length; index++) {
      var char = source.charAt(index);
      if (char >= "0" && char <= "9") {
        normalized += char;
        continue;
      }
      if (char === "." && !dotSeen) {
        if (!normalized) {
          normalized = "0";
        }
        normalized += ".";
        dotSeen = true;
      }
    }

    if (dotSeen) {
      var parts = normalized.split(".");
      normalized = parts[0] + "." + String(parts[1] || "").slice(0, 2);
    }

    if (isFinite(maxLength) && maxLength > 0 && normalized.length > maxLength) {
      normalized = normalized.slice(0, maxLength);
    }

    return normalized;
  }

  function parseTemperatureNumber(raw) {
    var value = Number(raw);
    return isFinite(value) ? value : NaN;
  }

  function syncTemperatureInput(input, finalize) {
    if (!input) {
      return true;
    }

    var maxLength = temperatureInputMaxLength(input);
    var raw = String(input.value || "");
    var sanitized = normalizeTemperatureInputText(raw, maxLength);
    var minValue = Number(input.getAttribute("data-temperature-min"));
    var maxValue = Number(input.getAttribute("data-temperature-max"));
    var errorMessage = String(input.getAttribute("data-temperature-range-error") || "");
    var numeric = parseTemperatureNumber(sanitized);

    if (sanitized !== raw) {
      input.value = sanitized;
    }

    if (!sanitized) {
      input.dataset.temperatureLastValid = "";
      input.setCustomValidity("");
      input.removeAttribute("aria-invalid");
      return true;
    }

    if (isFinite(numeric) && (!isFinite(maxValue) || numeric <= maxValue)) {
      input.dataset.temperatureLastValid = sanitized;
      input.setAttribute("aria-invalid", "false");
    } else if (sanitized) {
      input.removeAttribute("aria-invalid");
    }

    if (!finalize) {
      input.setCustomValidity("");
      input.removeAttribute("aria-invalid");
      return true;
    }

    if (!isFinite(numeric) || (isFinite(minValue) && numeric < minValue) || (isFinite(maxValue) && numeric > maxValue)) {
      input.setCustomValidity(errorMessage);
      input.setAttribute("aria-invalid", "true");
      return false;
    }

    input.value = numeric.toFixed(2);
    input.dataset.temperatureLastValid = input.value;
    input.setCustomValidity("");
    input.setAttribute("aria-invalid", "false");
    return true;
  }

  function finalizeTemperatureInput(input, reveal) {
    var valid = syncTemperatureInput(input, true);
    if (!valid && reveal && typeof input.reportValidity === "function") {
      input.reportValidity();
    }
    return valid;
  }

  function validateTemperatureInputs(form, reveal) {
    if (!form || !form.querySelectorAll) {
      return true;
    }

    var inputs = form.querySelectorAll("[data-temperature-input]");
    var firstInvalid = null;
    var shouldReveal = reveal !== false;

    for (var index = 0; index < inputs.length; index++) {
      var input = inputs[index];
      if (!syncTemperatureInput(input, true) && !firstInvalid) {
        firstInvalid = input;
      }
    }

    if (!firstInvalid) {
      return true;
    }

    if (shouldReveal && typeof firstInvalid.reportValidity === "function") {
      firstInvalid.reportValidity();
    }
    return false;
  }

  function bindTemperatureInputs(root) {
    var scope = root && root.querySelectorAll ? root : document;
    var inputs = scope.querySelectorAll("[data-temperature-input]");

    for (var index = 0; index < inputs.length; index++) {
      var input = inputs[index];
      var form = input.form;

      if (!input.getAttribute("maxlength")) {
        input.setAttribute("maxlength", String(temperatureInputMaxLength(input)));
      }

      if (input.dataset.temperatureInputBound !== "1") {
        input.dataset.temperatureInputBound = "1";

        input.addEventListener("input", function () {
          syncTemperatureInput(this, false);
        });

        input.addEventListener("blur", function () {
          finalizeTemperatureInput(this, true);
        });

        input.addEventListener("change", function () {
          finalizeTemperatureInput(this, true);
        });
      }

      if (form && form.dataset.temperatureInputsBound !== "1") {
        form.dataset.temperatureInputsBound = "1";
        form.addEventListener("submit", function (event) {
          if (!validateTemperatureInputs(this, true)) {
            event.preventDefault();
          }
        });
      }

      syncTemperatureInput(input, false);
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
    syncPeriodToggleLabels(root, isPeriod);

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

  function syncPeriodToggleLabels(root, isPeriod) {
    if (!root || !root.querySelectorAll) {
      return;
    }

    var labels = root.querySelectorAll("[data-period-toggle-label]");
    for (var index = 0; index < labels.length; index++) {
      var label = labels[index];
      var onText = String(label.getAttribute("data-period-label-on") || "");
      var offText = String(label.getAttribute("data-period-label-off") || "");
      var prefix = label.textContent && label.textContent.indexOf("🩸") === 0 ? "🩸 " : "";
      label.textContent = prefix + (isPeriod ? onText : offText);
    }
  }

  function syncNoteDisclosure(root) {
    if (!root || !root.querySelectorAll) {
      return;
    }

    var disclosures = root.querySelectorAll("[data-note-disclosure]");
    for (var index = 0; index < disclosures.length; index++) {
      var disclosure = disclosures[index];
      var label = disclosure.querySelector("[data-note-disclosure-label]");
      var summary = disclosure.querySelector("summary");
      var notesField = disclosure.querySelector("[data-dashboard-notes]");
      var openText = String(disclosure.getAttribute("data-note-open-text") || "");
      var emptyText = String(disclosure.getAttribute("data-note-empty-text") || "");
      var filledText = String(disclosure.getAttribute("data-note-filled-text") || "");
      var hasNotes = !!(notesField && String(notesField.value || "").trim());
      var isOpen = disclosure.hasAttribute("open");
      if (summary) {
        summary.setAttribute("aria-expanded", isOpen ? "true" : "false");
      }
      if (!label) {
        continue;
      }
      label.textContent = isOpen
        ? openText
        : (hasNotes ? filledText : emptyText);
    }
  }

  function bindNoteDisclosures(root) {
    if (!root || !root.querySelectorAll) {
      return;
    }

    var disclosures = root.querySelectorAll("[data-note-disclosure]");
    for (var index = 0; index < disclosures.length; index++) {
      var disclosure = disclosures[index];
      var summary = disclosure.querySelector("summary");
      if (disclosure.dataset.noteDisclosureBound === "1") {
        continue;
      }
      disclosure.dataset.noteDisclosureBound = "1";
      if (summary) {
        (function (currentDisclosure) {
          summary.addEventListener("click", function (event) {
            event.preventDefault();
            currentDisclosure.open = !currentDisclosure.open;
            syncNoteDisclosure(root);
          });
        })(disclosure);
      }
      disclosure.addEventListener("toggle", function () {
        syncNoteDisclosure(root);
      });
    }
  }

  function safeLocalStorageGet(key) {
    try {
      return window.localStorage.getItem(key);
    } catch {
      return "";
    }
  }

  function safeLocalStorageSet(key, value) {
    try {
      window.localStorage.setItem(key, value);
    } catch {
      // Ignore privacy mode and quota failures.
    }
  }

  function revealOnceTips(root) {
    if (!root || !root.querySelectorAll) {
      return;
    }

    var tips = root.querySelectorAll("[data-once-tip]");
    for (var index = 0; index < tips.length; index++) {
      var tip = tips[index];
      var key = String(tip.getAttribute("data-once-tip") || "").trim();
      if (!key) {
        continue;
      }

      if (safeLocalStorageGet("ovumcy_once_tip:" + key) === "1") {
        setNodeHidden(tip, true);
        continue;
      }

      setNodeHidden(tip, false);
      safeLocalStorageSet("ovumcy_once_tip:" + key, "1");
    }
  }

  function autosizeNoteField(field) {
    if (!field || !field.style) {
      return;
    }
    field.style.height = "auto";
    field.style.height = Math.min(field.scrollHeight, 320) + "px";
  }

  function bindAutosizeNoteFields(root) {
    var scope = root && root.querySelectorAll ? root : document;
    var fields = scope.querySelectorAll(".dashboard-notes-field");
    for (var index = 0; index < fields.length; index++) {
      var field = fields[index];
      if (field.dataset.autosizeBound !== "1") {
        field.dataset.autosizeBound = "1";
        field.addEventListener("input", function () {
          autosizeNoteField(this);
        });
      }
      autosizeNoteField(field);
    }
  }

  function periodTipPending() {
    return !!document.body && document.body.getAttribute("data-period-tip-pending") === "true";
  }

  function setPeriodTipAcknowledged(scope) {
    if (!scope || !scope.querySelectorAll) {
      return;
    }

    var inputs = scope.querySelectorAll("[data-period-tip-ack]");
    for (var index = 0; index < inputs.length; index++) {
      inputs[index].value = "true";
    }
    if (document.body) {
      document.body.setAttribute("data-period-tip-pending", "false");
    }
  }

  function revealPeriodTip(scope) {
    var message = document.body ? String(document.body.getAttribute("data-period-tip-message") || "").trim() : "";
    var copy = scope && scope.querySelector ? scope.querySelector("[data-period-tip-copy]") : null;

    if (copy) {
      setNodeHidden(copy, false);
    }
    if (!copy && message && typeof window.showToast === "function") {
      window.showToast(message, "ok");
    }
  }

  function maybeAcknowledgePeriodTip(scope) {
    if (!periodTipPending()) {
      return;
    }
    setPeriodTipAcknowledged(scope);
    revealPeriodTip(scope);
  }

  window.__ovumcyMaybeAcknowledgePeriodTip = maybeAcknowledgePeriodTip;

  function showQuickFocus(section) {
    if (!section || !section.classList) {
      return;
    }

    section.classList.add("dashboard-section-quick-focus");
    if (section.__ovumcyQuickFocusTimer) {
      window.clearTimeout(section.__ovumcyQuickFocusTimer);
    }
    section.__ovumcyQuickFocusTimer = window.setTimeout(function () {
      section.classList.remove("dashboard-section-quick-focus");
      section.__ovumcyQuickFocusTimer = 0;
    }, 1800);
  }

  function focusSectionControl(section, selector) {
    if (!section || !section.querySelector) {
      return;
    }

    var target = section.querySelector(selector);
    if (!target) {
      return;
    }

    if (target.closest && target.closest("details")) {
      target.closest("details").open = true;
    }
    if (typeof target.focus === "function") {
      target.focus();
    }
    if (typeof section.scrollIntoView === "function") {
      section.scrollIntoView({ block: "center", behavior: "smooth" });
    }
    showQuickFocus(section);
  }

  function dashboardAutosaveIndicator(form) {
    if (!form || !form.querySelector) {
      return null;
    }
    return form.querySelector("[data-dashboard-autosave-indicator]");
  }

  function dashboardAutosaveMessage(form, key, fallback) {
    if (!form || !form.getAttribute) {
      return fallback || "";
    }
    return String(form.getAttribute("data-autosave-" + key) || fallback || "");
  }

  function setDashboardAutosaveIndicator(form, key) {
    var indicator = dashboardAutosaveIndicator(form);
    if (!indicator) {
      return;
    }
    indicator.textContent = dashboardAutosaveMessage(form, key, indicator.textContent);
    indicator.setAttribute("data-autosave-state", key);
  }

  function clearDashboardAutosaveTimers(form) {
    if (!form) {
      return;
    }
    if (form.__ovumcyAutosaveTimer) {
      window.clearTimeout(form.__ovumcyAutosaveTimer);
      form.__ovumcyAutosaveTimer = 0;
    }
    if (form.__ovumcyAutosaveResetTimer) {
      window.clearTimeout(form.__ovumcyAutosaveResetTimer);
      form.__ovumcyAutosaveResetTimer = 0;
    }
  }

  function scheduleDashboardAutosaveIdleReset(form) {
    if (!form) {
      return;
    }
    if (form.__ovumcyAutosaveResetTimer) {
      window.clearTimeout(form.__ovumcyAutosaveResetTimer);
    }
    form.__ovumcyAutosaveResetTimer = window.setTimeout(function () {
      setDashboardAutosaveIndicator(form, "idle");
      form.__ovumcyAutosaveResetTimer = 0;
    }, 2200);
  }

  function notifyAutosaveNotice(response) {
    var notice;
    if (!response || typeof response.headers.get !== "function" || typeof window.showToast !== "function") {
      return;
    }
    notice = typeof window.__ovumcyDecodeResponseNoticeHeader === "function"
      ? window.__ovumcyDecodeResponseNoticeHeader(response.headers.get("X-Ovumcy-Notice"))
      : String(response.headers.get("X-Ovumcy-Notice") || "").trim();
    if (!notice) {
      return;
    }
    window.showToast(notice, "error");
  }

  function buildDashboardAutosaveBody(form) {
    return new URLSearchParams(new FormData(form));
  }

  function runDashboardAutosave(form, keepalive) {
    var requestVersion;
    var url;
    var headers;
    var body;
    var timezone;

    if (!form || form.dataset.autosaveDirty !== "true") {
      return Promise.resolve(true);
    }
    if (form.__ovumcyAutosaveInFlight) {
      return form.__ovumcyAutosaveInFlight;
    }

    clearDashboardAutosaveTimers(form);
    if (!validateTemperatureInputs(form, false)) {
      setDashboardAutosaveIndicator(form, "invalid");
      scheduleDashboardAutosaveIdleReset(form);
      return Promise.resolve(false);
    }
    setDashboardAutosaveIndicator(form, "saving");

    requestVersion = form.__ovumcyAutosaveVersion || 0;
    url = String(form.getAttribute("hx-post") || form.getAttribute("action") || "").trim();
    headers = {
      "Content-Type": "application/x-www-form-urlencoded;charset=UTF-8",
      "HX-Request": "true"
    };
    body = buildDashboardAutosaveBody(form);
    timezone = currentClientTimezone();

    if (document.querySelector('meta[name="csrf-token"]')) {
      headers["X-CSRF-Token"] = document.querySelector('meta[name="csrf-token"]').getAttribute("content") || "";
    }
    if (timezone) {
      headers[TIMEZONE_HEADER_NAME] = timezone;
    }

    form.__ovumcyAutosaveInFlight = window.fetch(url, {
      method: "POST",
      credentials: "same-origin",
      keepalive: !!keepalive,
      headers: headers,
      body: body.toString()
    }).then(function (response) {
      if (!response.ok) {
        throw new Error("autosave_failed");
      }
      notifyAutosaveNotice(response);
      if ((form.__ovumcyAutosaveVersion || 0) === requestVersion) {
        delete form.dataset.autosaveDirty;
      }
      setDashboardAutosaveIndicator(form, "saved");
      scheduleDashboardAutosaveIdleReset(form);
      return true;
    }).catch(function () {
      setDashboardAutosaveIndicator(form, "error");
      scheduleDashboardAutosaveIdleReset(form);
      return false;
    }).finally(function () {
      form.__ovumcyAutosaveInFlight = null;
      if (form.dataset.autosaveDirty === "true") {
        form.__ovumcyAutosaveTimer = window.setTimeout(function () {
          runDashboardAutosave(form, false);
        }, 2000);
      }
    });

    return form.__ovumcyAutosaveInFlight;
  }

  function markDashboardAutosaveDirty(form) {
    if (!form) {
      return;
    }
    form.__ovumcyAutosaveVersion = (form.__ovumcyAutosaveVersion || 0) + 1;
    form.dataset.autosaveDirty = "true";
    if (form.__ovumcyAutosaveInFlight) {
      return;
    }
    if (form.__ovumcyAutosaveTimer) {
      window.clearTimeout(form.__ovumcyAutosaveTimer);
    }
    form.__ovumcyAutosaveTimer = window.setTimeout(function () {
      runDashboardAutosave(form, false);
    }, 2000);
  }

  function handleDashboardQuickAction(root, action) {
    var periodToggle = root.querySelector("[data-period-toggle]");
    var moodSection = root.querySelector("[data-dashboard-section='mood']");
    var symptomSection = root.querySelector("[data-dashboard-section='symptoms']");

    switch (action) {
      case "period":
        if (!periodToggle) {
          return;
        }
        periodToggle.checked = !periodToggle.checked;
        periodToggle.dispatchEvent(new Event("change", { bubbles: true }));
        if (periodToggle.checked) {
          maybeAcknowledgePeriodTip(root);
        }
        break;
      case "mood":
        focusSectionControl(moodSection, "input[name='mood']:checked, input[name='mood']");
        break;
      case "symptom":
        focusSectionControl(symptomSection, "input[name='symptom_ids']:checked, input[name='symptom_ids']");
        break;
    }
  }

  function finalizeDashboardManualSave(form, successful) {
    if (!form) {
      return;
    }
    clearDashboardAutosaveTimers(form);
    delete form.dataset.autosaveDirty;
    if (!successful) {
      setDashboardAutosaveIndicator(form, "idle");
      return;
    }
    setDashboardAutosaveIndicator(form, "idle");
  }

  window.__ovumcyFinalizeDashboardManualSave = finalizeDashboardManualSave;

  function bindDashboardAutosaveBeforeUnload() {
    if (document.body && document.body.dataset.dashboardAutosaveBeforeUnloadBound === "1") {
      return;
    }
    if (document.body) {
      document.body.dataset.dashboardAutosaveBeforeUnloadBound = "1";
    }

    window.addEventListener("beforeunload", function () {
      var forms = document.querySelectorAll("[data-dashboard-save-form]");
      for (var index = 0; index < forms.length; index++) {
        if (forms[index].dataset.autosaveDirty === "true") {
          runDashboardAutosave(forms[index], true);
        }
      }
    });
  }

  function bindDashboardEditors() {
    var roots = document.querySelectorAll("[data-dashboard-editor]");
    for (var index = 0; index < roots.length; index++) {
      var root = roots[index];
      var form = root.querySelector("[data-dashboard-save-form]");
      if (root.dataset.dashboardEditorBound !== "1") {
        root.dataset.dashboardEditorBound = "1";

        root.addEventListener("change", function (event) {
          var currentForm = this.querySelector("[data-dashboard-save-form]");
          var periodToggle = event.target && event.target.matches && event.target.matches("[data-period-toggle]") ? event.target : null;
          if (periodToggle || (event.target && (event.target.name === "symptom_ids" || event.target.name === "mood"))) {
            syncDashboardPreview(this);
          }
          if (periodToggle && periodToggle.checked) {
            maybeAcknowledgePeriodTip(this);
          }
          if (currentForm && event.target && event.target.name !== "csrf_token") {
            markDashboardAutosaveDirty(currentForm);
          }
        });

        root.addEventListener("input", function (event) {
          var currentForm = this.querySelector("[data-dashboard-save-form]");
          if (event.target && event.target.matches && event.target.matches("[data-dashboard-notes]")) {
            syncDashboardPreview(this);
            syncNoteDisclosure(this);
          }
          if (currentForm && event.target && event.target.name !== "csrf_token") {
            markDashboardAutosaveDirty(currentForm);
          }
        });

        root.addEventListener("click", function (event) {
          var actionButton = closestFromEvent(event, "[data-quick-action]");
          var cycleStartButton = closestFromEvent(event, "[data-dashboard-cycle-start-button]");
          if (actionButton && this.contains(actionButton)) {
            event.preventDefault();
            handleDashboardQuickAction(this, actionButton.getAttribute("data-quick-action"));
            return;
          }
          if (cycleStartButton && this.contains(cycleStartButton)) {
            maybeAcknowledgePeriodTip(cycleStartButton.form || this);
          }
        });

        if (form) {
          form.addEventListener("submit", function () {
            clearDashboardAutosaveTimers(this);
          });
        }
      }

      bindNoteDisclosures(root);
      bindAutosizeNoteFields(root);
      revealOnceTips(root);
      syncDashboardPreview(root);
      syncNoteDisclosure(root);
      setDashboardAutosaveIndicator(form, "idle");
    }

    bindDashboardAutosaveBeforeUnload();
  }

  function syncDayEditorForm(form) {
    var periodToggle = form.querySelector("[data-period-toggle]");
    var isPeriod = !!(periodToggle && periodToggle.checked);
    syncPeriodFieldsets(form, isPeriod);
    syncPeriodToggleLabels(form, isPeriod);
    syncNoteDisclosure(form);
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

          if (event.target.checked) {
            maybeAcknowledgePeriodTip(this);
          }
          syncDayEditorForm(this);
        });

        form.addEventListener("click", function (event) {
          var cycleStartButton = closestFromEvent(event, "[data-day-cycle-start-button]");
          if (!cycleStartButton || !this.contains(cycleStartButton)) {
            return;
          }
          maybeAcknowledgePeriodTip(cycleStartButton.form || this);
        });
      }

      bindNoteDisclosures(form);
      bindAutosizeNoteFields(form);
      revealOnceTips(form);
      syncDayEditorForm(form);
    }
  }

  function syncSettingsCycleForm(root) {
    var cycleInput = root.querySelector("[data-settings-cycle-length]");
    var periodInput = root.querySelector("[data-settings-period-length]");
    var cycleValue = root.querySelector("[data-settings-cycle-length-value]");
    var periodValue = root.querySelector("[data-settings-period-length-value]");
    var unpredictableInput = root.querySelector('input[name="unpredictable_cycle"]');
    if (!cycleInput || !periodInput) {
      return;
    }

    var cycleLength = clampInteger(cycleInput.value, 28, 15, 90);
    var periodLength = clampInteger(periodInput.value, 5, 1, 14);
    var guidance = cycleGuidanceState(cycleLength, periodLength);
    var showShortCycleWarning = guidance.cycleShort && !(unpredictableInput && unpredictableInput.checked);
    periodLength = guidance.periodLength;

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
    setNodeHidden(root.querySelector("[data-settings-cycle-message='adjusted']"), !guidance.adjusted);
    setNodeHidden(root.querySelector("[data-settings-cycle-message='period-long']"), !guidance.periodLong);
    setNodeHidden(root.querySelector("[data-settings-cycle-message='cycle-short']"), !showShortCycleWarning);
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
          if (event.target.matches("[data-settings-cycle-length], [data-settings-period-length], input[name='unpredictable_cycle']")) {
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
          todayLabel: String(root.getAttribute("data-today-label") || "Today"),
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
        state.dayOptions = buildDayOptions(state.minDate, state.maxDate, state.lang, state.todayLabel);
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

  function syncRecoveryCodeConfirmForm(form) {
    if (!form || !form.querySelector) {
      return;
    }

    var checkbox = form.querySelector("#recovery-code-saved");
    var submit = form.querySelector("[data-recovery-code-submit]");
    var enabled = !!(checkbox && checkbox.checked);
    if (!submit) {
      return;
    }

    submit.disabled = !enabled;
    submit.classList.toggle("btn--disabled", !enabled);
  }

  function bindRecoveryCodeConfirmForms() {
    var forms = document.querySelectorAll("[data-recovery-code-confirm]");
    for (var index = 0; index < forms.length; index++) {
      var form = forms[index];
      if (form.dataset.recoveryConfirmBound !== "1") {
        form.dataset.recoveryConfirmBound = "1";
        form.addEventListener("change", function (event) {
          if (event.target && event.target.id === "recovery-code-saved") {
            syncRecoveryCodeConfirmForm(this);
          }
        });
      }
      syncRecoveryCodeConfirmForm(form);
    }
  }

  function initCSPFriendlyComponents() {
    bindThemeToggleButtons();
    bindMobileMenu();
    bindPWAInstallBanner();
    if (typeof window.__ovumcyBindLocalizedDateFields === "function") {
      window.__ovumcyBindLocalizedDateFields(document);
    }
    bindBinaryToggles(document);
    bindSymptomNameCounters(document);
    bindTemperatureInputs(document);
    bindSettingsCycleForms();
    bindIconControls();
    bindDashboardEditors();
    bindDayEditorForms();
    bindCalendarViews();
    bindOnboardingFlows();
    bindRecoveryCodeTools();
    bindRecoveryCodeConfirmForms();
  }
