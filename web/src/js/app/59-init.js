  function initCSPFriendlyComponents() {
    bindMobileMenu();
    bindPWAInstallOffer();
    bindPWAInstallSettingsRow();
    if (typeof window.__ovumcyBindLocalizedDateFields === "function") {
      window.__ovumcyBindLocalizedDateFields(document);
    }
    bindBinaryToggles(document);
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
