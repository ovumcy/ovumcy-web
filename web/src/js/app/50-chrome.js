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

