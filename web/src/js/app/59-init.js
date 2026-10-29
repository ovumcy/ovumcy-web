  // Controls marked data-nojs-only stand in for a JS feature (hx-confirm); with
  // scripting running they are dropped, since a required one would block htmx.
  function dropNoJSOnlyControls(root) {
    root.querySelectorAll("[data-nojs-only]").forEach(function (node) {
      node.remove();
    });
  }

  function initCSPFriendlyComponents() {
    dropNoJSOnlyControls(document);
    bindMobileMenu();
    bindPWAInstallOffer();
    bindPWAInstallSettingsRow();
    if (typeof window.__ovumcyBindLocalizedDateFields === "function") {
      window.__ovumcyBindLocalizedDateFields(document);
    }
    bindBinaryToggles(document);
    bindUsageGoalWarnings(document);
    bindSymptomNameCounters(document);
    bindTemperatureInputs(document);
    bindPregnancyTestFields(document);
    // Before the hash reveal: its load-time scroll reads the cards'
    // scroll-margin-top, which depends on the measured section-index height.
    bindSettingsSectionNavOffset();
    bindHashDisclosureReveals();
    bindGoalChipEscape();
    bindDashboardNotesCounters(document);
    bindSettingsCycleForms();
    bindSettingsTrackingForms();
    bindSettingsInterfaceForms();
    bindSettingsSectionDisclosures();
    bindFocusAfterSwap();
    bindIconControls();
    bindDashboardEditors();
    bindDayEditorForms();
    bindCalendarViews();
    bindOnboardingFlows();
    bindRecoveryCodeTools();
    bindRecoveryCodeConfirmForms();
    consumeFocusAfterReload();
  }
