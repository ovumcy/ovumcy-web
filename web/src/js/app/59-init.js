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
    bindDashboardNotesCounters(document);
    bindSettingsCycleForms();
    bindSettingsTrackingForms();
    bindSettingsInterfaceForms();
    bindIconControls();
    bindDashboardEditors();
    bindDayEditorForms();
    bindCalendarViews();
    bindOnboardingFlows();
    bindRecoveryCodeTools();
    bindRecoveryCodeConfirmForms();
  }
