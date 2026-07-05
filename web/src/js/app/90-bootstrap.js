  function configureHTMXForCSP() {
    if (!window.htmx || !window.htmx.config) {
      return;
    }

    window.htmx.config.allowEval = false;
    window.htmx.config.includeIndicatorStyles = false;
    // htmx 2.0 moved DELETE parameters into the URL query string by default
    // (methodsThatUseUrlParams gained "delete"). Our handlers read request
    // bodies via Fiber BodyParser, so restore the htmx 1.x behaviour and keep
    // DELETE inputs (e.g. the delete-account / 2FA-disable password) in the body.
    window.htmx.config.methodsThatUseUrlParams = ["get"];
  }

  configureHTMXForCSP();
  initClientTimezone();
  initPWAInstallPrompt();

  onDocumentReady(function () {
    initThemePreference();
    initAuthPanelTransitions();
    initPasswordToggles();
    initLoginValidation();
    initForgotPasswordValidation();
    initRegisterValidation();
    initSettingsPasswordValidation();
    initResetPasswordValidation();
    initLoginErrorFocus();
    initConfirmModal();
    initClearDataPasswordConfirmation();
    bindCycleStartConfirmForms();
    initToastAPI();
    initHTMXHooks();
    initCSPFriendlyComponents();
    syncClientTimezone();

    document.body.addEventListener("htmx:afterSwap", function () {
      initCSPFriendlyComponents();
    });
  });
})();
