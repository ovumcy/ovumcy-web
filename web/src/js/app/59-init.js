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
    bindHashDisclosureReveals();
    bindDashboardNotesCounters(document);
    bindSettingsCycleForms();
    bindSettingsTrackingForms();
    bindSettingsInterfaceForms();
    bindSettingsSectionDisclosures();
    bindIconControls();
    bindDashboardEditors();
    bindDayEditorForms();
    bindCalendarViews();
    bindOnboardingFlows();
    bindRecoveryCodeTools();
    bindRecoveryCodeConfirmForms();
  }
