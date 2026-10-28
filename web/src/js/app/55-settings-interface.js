  function readCheckedRadioValue(root, name) {
    if (!root || !root.querySelector) {
      return "";
    }

    var input = root.querySelector('input[name="' + name + '"]:checked');
    if (!input) {
      return "";
    }
    return String(input.value || "").trim();
  }

  function setRadioGroupValue(root, name, value) {
    if (!root || !root.querySelectorAll) {
      return false;
    }

    var normalized = String(value || "").trim();
    var inputs = root.querySelectorAll('input[name="' + name + '"]');
    var matched = false;
    for (var index = 0; index < inputs.length; index++) {
      var input = inputs[index];
      var selected = String(input.value || "").trim() === normalized;
      input.checked = selected;
      matched = matched || selected;
    }
    return matched;
  }

  function syncSettingsInterfaceOptionSelections(root) {
    if (!root || !root.querySelectorAll) {
      return;
    }

    var language = readCheckedRadioValue(root, "language");
    var theme = normalizeThemePreference(readCheckedRadioValue(root, "theme"));
    var languageOptions = root.querySelectorAll("[data-settings-interface-language-option]");
    var themeOptions = root.querySelectorAll("[data-settings-interface-theme-option]");

    for (var languageIndex = 0; languageIndex < languageOptions.length; languageIndex++) {
      var languageOption = languageOptions[languageIndex];
      languageOption.dataset.selected = String(languageOption.getAttribute("data-settings-interface-language-option") || "") === language
        ? "true"
        : "false";
    }

    for (var themeIndex = 0; themeIndex < themeOptions.length; themeIndex++) {
      var themeOption = themeOptions[themeIndex];
      themeOption.dataset.selected = normalizeThemePreference(themeOption.getAttribute("data-settings-interface-theme-option")) === theme
        ? "true"
        : "false";
    }
  }

  function currentSettingsInterfaceSelection(root) {
    return {
      language: readCheckedRadioValue(root, "language"),
      theme: normalizeThemePreference(readCheckedRadioValue(root, "theme"))
    };
  }

  function sameSettingsInterfaceSelection(left, right) {
    if (!left || !right) {
      return false;
    }
    return left.language === right.language && left.theme === right.theme;
  }

  function syncSettingsInterfaceForm(root) {
    var state = root ? root.__ovumcySettingsInterfaceState : null;
    var selection;
    var dirty;
    if (!root || !state) {
      return;
    }

    selection = currentSettingsInterfaceSelection(root);
    if (!selection.language && state.initial.language) {
      selection.language = state.initial.language;
      setRadioGroupValue(root, "language", selection.language);
    }
    if (!selection.theme && state.initial.theme) {
      selection.theme = state.initial.theme;
      setRadioGroupValue(root, "theme", selection.theme);
    }

    if (selection.theme) {
      applyTheme(selection.theme);
    }

    syncSettingsInterfaceOptionSelections(root);
    dirty = !sameSettingsInterfaceSelection(selection, state.initial);
    root.dataset.settingsDraftDirty = dirty ? "true" : "false";
    // An account with no stored language can save the one already rendered;
    // that is not a draft, so neither Discard nor the leave guard react to it.
    syncSettingsDraftButton(state.saveButton, dirty || state.languageUnsaved);
    syncSettingsDraftButton(state.discardButton, dirty);
    if (!dirty) {
      setSettingsDraftTransition(root, false);
    }
  }

  function resetSettingsInterfaceForm(root) {
    var state = root ? root.__ovumcySettingsInterfaceState : null;
    if (!root || !state) {
      return;
    }

    setRadioGroupValue(root, "language", state.initial.language);
    setRadioGroupValue(root, "theme", state.initial.theme);
    applyTheme(state.initial.theme);
    syncSettingsInterfaceForm(root);
  }

  // The confirmation of a language change arrives with the reloaded page, in the
  // card's live region. Focus it once so focus does not fall to BODY; a page
  // without that confirmation has no such node and nothing is focused.
  function focusSettingsInterfaceFlash() {
    var flash = document.querySelector("[data-settings-flash-focus]");
    if (!flash || flash.dataset.settingsFlashFocused === "1") {
      return;
    }
    flash.dataset.settingsFlashFocused = "1";
    if (typeof flash.focus === "function") {
      flash.focus({ preventScroll: true });
    }
  }

  function bindSettingsInterfaceForms() {
    var roots = document.querySelectorAll("[data-settings-interface-form]");
    if (!roots.length) {
      return;
    }
    focusSettingsInterfaceFlash();

    bindSettingsDraftLeaveGuard();

    for (var index = 0; index < roots.length; index++) {
      var root = roots[index];
      var initialLanguage = readCheckedRadioValue(root, "language") || String(document.documentElement.getAttribute("lang") || "").trim();
      // The stored preference, not the rendered theme: an owner on "system"
      // must find the system tile selected, not whichever of light/dark the
      // system happens to be resolving to right now. With nothing stored the
      // rendered theme stays the initial selection, as before.
      var initialTheme = currentThemePreference();

      if (!root.__ovumcySettingsInterfaceState) {
        root.__ovumcySettingsInterfaceState = {
          initial: {
            language: initialLanguage,
            theme: initialTheme
          },
          saveButton: root.querySelector("[data-settings-interface-save]"),
          discardButton: root.querySelector("[data-settings-interface-discard]"),
          languageUnsaved: root.hasAttribute("data-settings-interface-language-unsaved")
        };
      } else {
        root.__ovumcySettingsInterfaceState.initial.language = initialLanguage;
        root.__ovumcySettingsInterfaceState.initial.theme = initialTheme;
        root.__ovumcySettingsInterfaceState.languageUnsaved = root.hasAttribute("data-settings-interface-language-unsaved");
      }

      setRadioGroupValue(root, "language", root.__ovumcySettingsInterfaceState.initial.language);
      setRadioGroupValue(root, "theme", root.__ovumcySettingsInterfaceState.initial.theme);

      if (root.dataset.settingsInterfaceBound !== "1") {
        root.dataset.settingsInterfaceBound = "1";
        root.__ovumcySettingsDraftReset = function () {
          resetSettingsInterfaceForm(this);
        };

        root.addEventListener("change", function (event) {
          if (!event.target || !event.target.matches) {
            return;
          }
          if (event.target.matches('input[name="language"], input[name="theme"]')) {
            syncSettingsInterfaceForm(this);
          }
        });

        root.addEventListener("submit", function (event) {
          var selection = currentSettingsInterfaceSelection(this);
          if (!selection.language || !selection.theme) {
            event.preventDefault();
            setSettingsDraftTransition(this, false);
            return;
          }

          writeStoredTheme(selection.theme);
          setSettingsDraftTransition(this, true);
        });

        // An in-place save (same language) leaves the page as it is, so the
        // saved selection becomes the new baseline: the form is clean again and
        // the leave guard does not prompt for what was just saved.
        root.addEventListener("htmx:afterRequest", function (event) {
          var source = event && event.detail && event.detail.elt ? event.detail.elt : event.target;
          if (!source || source !== this) {
            return;
          }
          if (event.detail && event.detail.successful) {
            this.__ovumcySettingsInterfaceState.initial = currentSettingsInterfaceSelection(this);
            this.__ovumcySettingsInterfaceState.languageUnsaved = false;
            this.removeAttribute("data-settings-interface-language-unsaved");
            var unsavedHint = this.querySelector("[data-settings-interface-language-unsaved-hint]");
            if (unsavedHint) {
              unsavedHint.hidden = true;
            }
            syncSettingsInterfaceForm(this);
          } else {
            setSettingsDraftTransition(this, false);
          }
        });

        if (root.__ovumcySettingsInterfaceState.discardButton) {
          root.__ovumcySettingsInterfaceState.discardButton.addEventListener("click", function () {
            resetSettingsInterfaceForm(this.form);
          });
        }
      }

      syncSettingsInterfaceForm(root);
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

