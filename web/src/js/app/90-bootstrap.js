  function configureHTMXForCSP() {
    if (!window.htmx || !window.htmx.config) {
      return;
    }

    window.htmx.config.allowEval = false;
    window.htmx.config.includeIndicatorStyles = false;
  }

  configureHTMXForCSP();
  initClientTimezone();
  initPWAInstallPrompt();

  onDocumentReady(function () {
    initThemePreference();
    initAuthPanelTransitions();
    initLanguageSwitcher();
    initPasswordToggles();
    initLoginValidation();
    initForgotPasswordValidation();
    initRegisterValidation();
    initSettingsPasswordValidation();
    initResetPasswordValidation();
    initLoginPasswordPersistence();
    initConfirmModal();
    initClearDataPasswordConfirmation();
    bindCycleStartConfirmForms();
    initToastAPI();
    initHTMXHooks();
    initCSPFriendlyComponents();

    document.body.addEventListener("htmx:afterSwap", function () {
      initCSPFriendlyComponents();
    });
  });
})();
