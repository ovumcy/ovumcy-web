(function () {
  "use strict";

  var PASSWORD_HIDE_ICON = '<svg viewBox="0 0 24 24" class="password-toggle-svg" focusable="false" aria-hidden="true"><path d="M3 3.8 21 20.2" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="1.8"></path><path d="M9.9 9.9A3 3 0 0 0 12 15a3 3 0 0 0 2.1-.9" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="1.8"></path><path d="M5.5 7.7C4.3 8.7 3.3 10 2.6 12c2.1 3.6 5.6 5.8 9.4 5.8 1.7 0 3.4-.5 4.9-1.4" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="1.8"></path><path d="M10.1 6.4c.6-.2 1.2-.2 1.9-.2 3.8 0 7.3 2.2 9.4 5.8-.5.9-1.2 1.8-2 2.6" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="1.8"></path></svg>';
  var PASSWORD_SHOW_ICON = '<svg viewBox="0 0 24 24" class="password-toggle-svg" focusable="false" aria-hidden="true"><path d="M2.6 12c2.1-3.6 5.6-5.8 9.4-5.8s7.3 2.2 9.4 5.8c-2.1 3.6-5.6 5.8-9.4 5.8S4.7 15.6 2.6 12Z" fill="none" stroke="currentColor" stroke-linecap="round" stroke-linejoin="round" stroke-width="1.8"></path><circle cx="12" cy="12" r="2.6" fill="none" stroke="currentColor" stroke-width="1.8"></circle></svg>';
  var TOAST_VISIBLE_MS = 5200;
  var TOAST_EXIT_MS = 220;
  var STATUS_CLEAR_MS = 2000;
  var DOWNLOAD_REVOKE_MS = 500;
  var THEME_STORAGE_KEY = "ovumcy_theme";
  var THEME_LIGHT = "light";
  var THEME_DARK = "dark";
  var THEME_COLOR_LIGHT = "#fff9f0";
  var THEME_COLOR_DARK = "#18141f";
  var TIMEZONE_COOKIE_NAME = "ovumcy_tz";
  var TIMEZONE_HEADER_NAME = "X-Ovumcy-Timezone";
  var TIMEZONE_COOKIE_MAX_AGE_SECONDS = 31536000;

  function getEventTarget(event) {
    return event && event.target ? event.target : null;
  }

  function closestFromEvent(event, selector) {
    var target = getEventTarget(event);
    if (!target || !target.closest) {
      return null;
    }
    return target.closest(selector);
  }

  function isPrimaryClick(event) {
    return !!event && event.button === 0 && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey;
  }

  function onDocumentReady(callback) {
    if (document.readyState === "loading") {
      document.addEventListener("DOMContentLoaded", callback);
      return;
    }
    callback();
  }

  function normalizeTheme(value) {
    var theme = String(value || "").trim().toLowerCase();
    if (theme === THEME_DARK || theme === THEME_LIGHT) {
      return theme;
    }
    return "";
  }

  function supportsMatchMedia() {
    return typeof window.matchMedia === "function";
  }

  function systemPreferredTheme() {
    if (!supportsMatchMedia()) {
      return THEME_LIGHT;
    }
    return window.matchMedia("(prefers-color-scheme: dark)").matches ? THEME_DARK : THEME_LIGHT;
  }

  function resolveTheme(theme) {
    return normalizeTheme(theme) || systemPreferredTheme();
  }

  function readStoredTheme() {
    try {
      return normalizeTheme(window.localStorage.getItem(THEME_STORAGE_KEY));
    } catch {
      return "";
    }
  }

  function writeStoredTheme(theme) {
    var normalized = normalizeTheme(theme);
    if (!normalized) {
      return;
    }

    try {
      window.localStorage.setItem(THEME_STORAGE_KEY, normalized);
    } catch {
      // Ignore storage quota and privacy mode errors.
    }
  }

  function updateThemeColorMeta(theme) {
    var meta = document.getElementById("theme-color-meta");
    if (!meta) {
      return;
    }

    meta.setAttribute("content", theme === THEME_DARK ? THEME_COLOR_DARK : THEME_COLOR_LIGHT);
  }

  function applyTheme(theme) {
    var resolved = resolveTheme(theme);
    document.documentElement.setAttribute("data-theme", resolved);
    updateThemeColorMeta(resolved);
    window.__ovumcyTheme = resolved;
    return resolved;
  }

  function currentTheme() {
    var htmlTheme = normalizeTheme(document.documentElement.getAttribute("data-theme"));
    if (htmlTheme) {
      return htmlTheme;
    }

    var known = normalizeTheme(window.__ovumcyTheme);
    if (known) {
      return known;
    }

    return applyTheme(readStoredTheme());
  }

  function initThemePreference() {
    applyTheme(readStoredTheme());
  }

  function setThemePreference(theme) {
    var normalized = normalizeTheme(theme);
    if (!normalized) {
      return currentTheme();
    }
    writeStoredTheme(normalized);
    return applyTheme(normalized);
  }

  function isSafeClientTimezone(value) {
    if (!value || value.length > 128) {
      return false;
    }
    return /^[A-Za-z0-9_+/-]+$/.test(value);
  }

  function detectClientTimezone() {
    try {
      var formatter = Intl && Intl.DateTimeFormat ? Intl.DateTimeFormat() : null;
      var options = formatter && formatter.resolvedOptions ? formatter.resolvedOptions() : null;
      var timezone = options && options.timeZone ? String(options.timeZone).trim() : "";
      if (!isSafeClientTimezone(timezone)) {
        return "";
      }
      return timezone;
    } catch {
      return "";
    }
  }

  function writeClientCookie(name, value, maxAgeSeconds) {
    if (!name || !value) {
      return;
    }
    var cookie = name + "=" + value +
      "; Path=/" +
      "; SameSite=Lax" +
      "; Max-Age=" + String(maxAgeSeconds || 0);
    if (window.location && window.location.protocol === "https:") {
      cookie += "; Secure";
    }
    document.cookie = cookie;
  }

  function initClientTimezone() {
    var timezone = detectClientTimezone();
    if (!timezone) {
      return;
    }
    window.__ovumcyTimezone = timezone;
    writeClientCookie(TIMEZONE_COOKIE_NAME, timezone, TIMEZONE_COOKIE_MAX_AGE_SECONDS);
  }

  function currentClientTimezone() {
    var known = String(window.__ovumcyTimezone || "").trim();
    if (known && isSafeClientTimezone(known)) {
      return known;
    }

    var detected = detectClientTimezone();
    if (detected) {
      window.__ovumcyTimezone = detected;
    }
    return detected;
  }

  function normalizeLanguageCode(raw) {
    if (!raw) {
      return "";
    }
    var normalized = String(raw).trim().toLowerCase().replace(/_/g, "-");
    if (!normalized) {
      return "";
    }
    if (normalized.indexOf("-") !== -1) {
      normalized = normalized.split("-")[0];
    }
    return normalized;
  }

  function supportedLanguages() {
    var root = document.documentElement;
    var raw = root ? root.getAttribute("data-supported-languages") : "";
    if (!raw) {
      return ["en"];
    }

    try {
      var parsed = JSON.parse(raw);
      if (!Array.isArray(parsed) || !parsed.length) {
        return ["en"];
      }

      var supported = [];
      for (var index = 0; index < parsed.length; index++) {
        var normalized = normalizeLanguageCode(parsed[index]);
        if (normalized && supported.indexOf(normalized) === -1) {
          supported.push(normalized);
        }
      }
      return supported.length ? supported : ["en"];
    } catch {
      return ["en"];
    }
  }

  function parseLanguage(raw) {
    var normalized = normalizeLanguageCode(raw);
    if (!normalized) {
      return "";
    }
    return supportedLanguages().indexOf(normalized) === -1 ? "" : normalized;
  }

  function readCookie(name) {
    var cookies = document.cookie ? document.cookie.split(";") : [];
    for (var index = 0; index < cookies.length; index++) {
      var part = cookies[index].trim();
      if (part.indexOf(name + "=") !== 0) {
        continue;
      }
      return decodeURIComponent(part.substring(name.length + 1));
    }
    return "";
  }

  function languageFromHref(href) {
    if (!href) {
      return "";
    }
    var match = href.match(/\/lang\/([^/?#]+)/i);
    if (!match || !match[1]) {
      return "";
    }
    return match[1];
  }

  function withCurrentNextPath(href) {
    if (!href) {
      return href;
    }
    try {
      var url = new URL(href, window.location.origin);
      var nextPath = window.location.pathname + window.location.search;
      url.searchParams.set("next", nextPath);
      return url.pathname + url.search + url.hash;
    } catch {
      return href;
    }
  }

  function applyHTMLLanguage(raw) {
    var lang = parseLanguage(raw);
    if (!lang) {
      return;
    }
    document.documentElement.setAttribute("lang", lang);
  }

  function initLanguageSwitcher() {
    applyHTMLLanguage(readCookie("ovumcy_lang") || document.documentElement.getAttribute("lang"));

    var links = document.querySelectorAll("a.lang-link");
    for (var index = 0; index < links.length; index++) {
      var link = links[index];
      var updatedHref = withCurrentNextPath(link.getAttribute("href"));
      if (updatedHref) {
        link.setAttribute("href", updatedHref);
      }
    }

    document.addEventListener("click", function (event) {
      var link = closestFromEvent(event, "a.lang-link");
      if (!link) {
        return;
      }

      var updatedHref = withCurrentNextPath(link.getAttribute("href"));
      if (updatedHref) {
        link.setAttribute("href", updatedHref);
      }
      applyHTMLLanguage(languageFromHref(updatedHref || link.getAttribute("href")));
    });
  }

  function initAuthPanelTransitions() {
    var panel = document.querySelector("[data-auth-panel]");
    if (!panel) {
      return;
    }

    var prefersReducedMotion = window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    if (!prefersReducedMotion) {
      panel.classList.add("auth-panel-transition");
      panel.classList.add("auth-panel-enter");
      window.requestAnimationFrame(function () {
        panel.classList.remove("auth-panel-enter");
      });
    }

    document.addEventListener("click", function (event) {
      var link = closestFromEvent(event, "a[data-auth-switch]");
      if (!link) {
        return;
      }

      if (event.defaultPrevented || !isPrimaryClick(event)) {
        return;
      }
      if (link.getAttribute("target") === "_blank") {
        return;
      }

      var href = (link.getAttribute("href") || "").trim();
      if (!href || prefersReducedMotion) {
        return;
      }

      event.preventDefault();
      panel.classList.add("auth-panel-transition");
      panel.classList.add("auth-panel-exit");
      window.setTimeout(function () {
        window.location.href = href;
      }, 140);
    });
  }

  function passwordToggleIconNode(button) {
    if (!button || !button.querySelector) {
      return null;
    }
    return button.querySelector("[data-password-toggle-icon]");
  }

  function updatePasswordToggleLabel(button, isVisible) {
    var showLabel = button.getAttribute("data-show-label") || "Show password";
    var hideLabel = button.getAttribute("data-hide-label") || "Hide password";
    var iconNode = passwordToggleIconNode(button);
    button.setAttribute("aria-label", isVisible ? hideLabel : showLabel);
    if (iconNode) {
      iconNode.innerHTML = isVisible ? PASSWORD_HIDE_ICON : PASSWORD_SHOW_ICON;
    }
  }

  function attachPasswordToggles(root) {
    var scope = root && root.querySelectorAll ? root : document;
    var buttons = scope.querySelectorAll("[data-password-toggle]");

    for (var index = 0; index < buttons.length; index++) {
      var button = buttons[index];
      if (button.dataset.passwordToggleBound === "1") {
        continue;
      }

      var field = button.parentElement ? button.parentElement.querySelector("input[type='password'], input[type='text']") : null;
      if (!field) {
        continue;
      }

      button.dataset.passwordToggleBound = "1";
      updatePasswordToggleLabel(button, field.type === "text");

      button.addEventListener("click", (function (input, toggleButton) {
        return function () {
          var reveal = input.type === "password";
          input.type = reveal ? "text" : "password";
          updatePasswordToggleLabel(toggleButton, reveal);
        };
      })(field, button));
    }
  }

  function initPasswordToggles() {
    attachPasswordToggles(document);
    document.body.addEventListener("htmx:afterSwap", function (event) {
      var target = event && event.detail ? event.detail.target : null;
      attachPasswordToggles(target || document);
    });
  }

  var authEmailInputPattern = "^[A-Za-z0-9.!#$%&'*+/=?^_`{|}~-]+@[A-Za-z0-9-]+(?:\\.[A-Za-z0-9-]+)+$";

  function configureEmailField(input) {
    if (!input || input.type !== "email") {
      return;
    }
    input.setAttribute("pattern", authEmailInputPattern);
  }

  function updateFieldValidityMessage(input, requiredMessage, emailMessage) {
    if (!input || typeof input.setCustomValidity !== "function") {
      return;
    }

    input.setCustomValidity("");
    if (!input.validity) {
      return;
    }

    if (input.validity.valueMissing) {
      input.setCustomValidity(requiredMessage);
      return;
    }
    if (input.type === "email" && (input.validity.typeMismatch || input.validity.patternMismatch)) {
      input.setCustomValidity(emailMessage);
    }
  }

  function bindRequiredFieldValidation(form, requiredMessage, emailMessage) {
    if (!form) {
      return;
    }

    var fields = form.querySelectorAll("input[required]");
    for (var index = 0; index < fields.length; index++) {
      configureEmailField(fields[index]);
      fields[index].addEventListener("invalid", function () {
        updateFieldValidityMessage(this, requiredMessage, emailMessage);
      });
      fields[index].addEventListener("input", function () {
        this.setCustomValidity("");
      });
      fields[index].addEventListener("blur", function () {
        updateFieldValidityMessage(this, requiredMessage, emailMessage);
      });
    }
  }

  function bindSimpleRequiredFormValidation(form, statusTarget, requiredMessage, emailMessage) {
    if (!form) {
      return;
    }

    bindRequiredFieldValidation(form, requiredMessage, emailMessage);

    form.addEventListener("input", function () {
      clearFormStatus(statusTarget);
      clearAuthServerError(form);
    });

    form.addEventListener("submit", function (event) {
      var invalidField;
      clearFormStatus(statusTarget);
      clearAuthServerError(form);

      invalidField = firstInvalidRequiredField(form, requiredMessage, emailMessage);
      if (!invalidField) {
        return;
      }

      event.preventDefault();
      moveFormStatusTarget(statusTarget, invalidField);
      renderFormStatusError(statusTarget, invalidField.validationMessage || requiredMessage);
      invalidField.focus();
    });
  }

  function renderFormStatusError(target, text) {
    if (!target) {
      return;
    }

    target.textContent = "";
    var block = document.createElement("div");
    block.className = "status-error";
    block.textContent = text;
    target.appendChild(block);
  }

  function statusAnchorForField(field) {
    if (!field) {
      return null;
    }

    if (typeof field.closest === "function") {
      var passwordField = field.closest(".password-field");
      if (passwordField) {
        return passwordField;
      }
    }

    return field;
  }

  function moveFormStatusTarget(target, field) {
    if (!target || !field) {
      return;
    }

    var anchor = statusAnchorForField(field);
    if (!anchor || !anchor.parentNode || typeof anchor.insertAdjacentElement !== "function") {
      return;
    }

    anchor.insertAdjacentElement("afterend", target);
  }

  function clearFormStatus(target) {
    if (!target) {
      return;
    }
    target.textContent = "";
  }

  function clearAuthServerError(form) {
    if (!form || !form.parentNode) {
      return;
    }

    var serverError = form.parentNode.querySelector("[data-auth-server-error]");
    if (serverError) {
      serverError.remove();
    }
  }

  function firstInvalidRequiredField(form, requiredMessage, emailMessage) {
    if (!form || !form.querySelectorAll) {
      return null;
    }

    var fields = form.querySelectorAll("input[required]");
    for (var index = 0; index < fields.length; index++) {
      var field = fields[index];
      updateFieldValidityMessage(field, requiredMessage, emailMessage);
      if (typeof field.checkValidity === "function" && !field.checkValidity()) {
        return field;
      }
    }
    return null;
  }

  function initLoginValidation() {
    var form = document.getElementById("login-form");
    if (!form) {
      return;
    }

    var requiredMessage = form.getAttribute("data-required-message") || "Please fill out this field.";
    var emailMessage = form.getAttribute("data-email-message") || "Please enter a valid email address.";
    var statusTarget = document.getElementById("login-client-status");
    bindSimpleRequiredFormValidation(form, statusTarget, requiredMessage, emailMessage);
  }

  function initForgotPasswordValidation() {
    var form = document.getElementById("forgot-password-form");
    if (!form) {
      return;
    }

    var requiredMessage = form.getAttribute("data-required-message") || "Please fill out this field.";
    var emailMessage = form.getAttribute("data-email-message") || "Please enter a valid email address.";
    var statusTarget = document.getElementById("forgot-password-client-status");
    bindSimpleRequiredFormValidation(form, statusTarget, requiredMessage, emailMessage);
  }

  function initRegisterValidation() {
    var form = document.getElementById("register-form");
    if (!form) {
      return;
    }

    var requiredMessage = form.getAttribute("data-required-message") || "Please fill out this field.";
    var emailMessage = form.getAttribute("data-email-message") || "Please enter a valid email address.";
    var mismatchMessage = form.getAttribute("data-password-mismatch-message") || "Passwords do not match.";
    bindRequiredFieldValidation(form, requiredMessage, emailMessage);

    var passwordField = document.getElementById("register-password");
    var confirmField = document.getElementById("register-confirm-password");
    if (!passwordField || !confirmField) {
      return;
    }

    var statusTarget = document.getElementById("register-client-status");

    function isPasswordMatchValid() {
      confirmField.setCustomValidity("");

      var password = String(passwordField.value || "");
      var confirm = String(confirmField.value || "");
      if (!password || !confirm || password === confirm) {
        return true;
      }

      confirmField.setCustomValidity(mismatchMessage);
      return false;
    }

    function handlePasswordInput() {
      clearFormStatus(statusTarget);
      clearAuthServerError(form);
      isPasswordMatchValid();
    }

    passwordField.addEventListener("input", handlePasswordInput);
    confirmField.addEventListener("input", handlePasswordInput);
    form.addEventListener("input", function () {
      clearFormStatus(statusTarget);
      clearAuthServerError(form);
    });

    form.addEventListener("submit", function (event) {
      var invalidField;
      clearFormStatus(statusTarget);
      clearAuthServerError(form);

      invalidField = firstInvalidRequiredField(form, requiredMessage, emailMessage);
      if (invalidField) {
        event.preventDefault();
        moveFormStatusTarget(statusTarget, invalidField);
        renderFormStatusError(statusTarget, invalidField.validationMessage || requiredMessage);
        invalidField.focus();
        return;
      }

      if (isPasswordMatchValid()) {
        return;
      }

      event.preventDefault();
      moveFormStatusTarget(statusTarget, confirmField);
      renderFormStatusError(statusTarget, mismatchMessage);
      focusLoginPasswordField(confirmField);
    });
  }

  function loginPasswordDraftStorage() {
    if (!window.sessionStorage) {
      return null;
    }
    return window.sessionStorage;
  }

  function readLoginPasswordDraft(storage, key) {
    if (!storage || !key) {
      return "";
    }
    try {
      return String(storage.getItem(key) || "");
    } catch {
      return "";
    }
  }

  function writeLoginPasswordDraft(storage, key, value) {
    if (!storage || !key) {
      return;
    }
    try {
      storage.setItem(key, String(value || ""));
    } catch {
      // Ignore session storage write failures (privacy mode, quota, etc.).
    }
  }

  function clearLoginPasswordDraft(storage, key) {
    if (!storage || !key) {
      return;
    }
    try {
      storage.removeItem(key);
    } catch {
      // Ignore session storage cleanup failures.
    }
  }

  function isTruthyDataValue(raw) {
    var normalized = String(raw || "").trim().toLowerCase();
    return normalized === "1" || normalized === "true" || normalized === "yes";
  }

  function focusLoginPasswordField(input) {
    if (!input || typeof input.focus !== "function") {
      return;
    }
    input.focus();

    if (typeof input.setSelectionRange !== "function") {
      return;
    }
    var end = String(input.value || "").length;
    input.setSelectionRange(end, end);
  }

  function initLoginPasswordPersistence() {
    var form = document.getElementById("login-form");
    if (!form) {
      return;
    }

    var passwordField = document.getElementById("login-password");
    if (!passwordField) {
      return;
    }

    var storage = loginPasswordDraftStorage();
    var storageKey = form.getAttribute("data-password-draft-key") || "ovumcy_login_password_draft";
    var hasError = isTruthyDataValue(form.getAttribute("data-login-has-error"));

    function persistPasswordDraft() {
      writeLoginPasswordDraft(storage, storageKey, passwordField.value);
    }

    passwordField.addEventListener("input", persistPasswordDraft);
    form.addEventListener("submit", persistPasswordDraft);

    if (!hasError) {
      clearLoginPasswordDraft(storage, storageKey);
      return;
    }

    var draft = readLoginPasswordDraft(storage, storageKey);
    if (draft) {
      passwordField.value = draft;
    }
    focusLoginPasswordField(passwordField);
  }

  function initResetPasswordValidation() {
    var form = document.getElementById("reset-password-form");
    if (!form) {
      return;
    }

    form.addEventListener("input", function () {
      clearAuthServerError(form);
    });
  }

  function initConfirmModal() {
    var modal = document.getElementById("confirm-modal");
    var messageNode = document.getElementById("confirm-modal-message");
    var cancelButton = document.getElementById("confirm-modal-cancel");
    var acceptButton = document.getElementById("confirm-modal-accept");
    if (!modal || !messageNode || !cancelButton || !acceptButton) {
      return;
    }

    var pendingResolve = null;

    function closeConfirm(accepted) {
      if (!pendingResolve) {
        return;
      }
      var resolve = pendingResolve;
      pendingResolve = null;
      modal.classList.add("hidden");
      modal.setAttribute("aria-hidden", "true");
      resolve(accepted);
    }

    function openConfirm(question, acceptLabel) {
      if (pendingResolve) {
        pendingResolve(false);
        pendingResolve = null;
      }

      messageNode.textContent = question || "";
      cancelButton.textContent = document.body.getAttribute("data-confirm-cancel") || "Cancel";
      acceptButton.textContent = acceptLabel || document.body.getAttribute("data-confirm-delete") || "Delete";
      modal.classList.remove("hidden");
      modal.setAttribute("aria-hidden", "false");
      cancelButton.focus();

      return new Promise(function (resolve) {
        pendingResolve = resolve;
      });
    }

    window.__ovumcyOpenConfirm = openConfirm;

    cancelButton.addEventListener("click", function () {
      closeConfirm(false);
    });

    acceptButton.addEventListener("click", function () {
      closeConfirm(true);
    });

    modal.addEventListener("click", function (event) {
      if (event.target === modal) {
        closeConfirm(false);
      }
    });

    document.addEventListener("keydown", function (event) {
      if (event.key === "Escape") {
        closeConfirm(false);
      }
    });

    document.body.addEventListener("htmx:confirm", function (event) {
      if (!event || !event.detail || !event.detail.question) {
        return;
      }

      var source = event.detail.elt || event.target;
      if (!source || !source.getAttribute) {
        return;
      }

      var acceptLabel = source.getAttribute("data-confirm-accept") || "";
      event.preventDefault();
      openConfirm(event.detail.question, acceptLabel).then(function (confirmed) {
        if (confirmed) {
          event.detail.issueRequest(true);
        }
      });
    });

    document.addEventListener("submit", function (event) {
      var form = event.target;
      if (!form || !form.matches || !form.matches("form[data-confirm]")) {
        return;
      }

      if (form.dataset.confirmBypass === "1") {
        form.dataset.confirmBypass = "";
        return;
      }

      event.preventDefault();
      openConfirm(form.getAttribute("data-confirm") || "", form.getAttribute("data-confirm-accept") || "").then(function (confirmed) {
        if (!confirmed) {
          return;
        }
        form.dataset.confirmBypass = "1";
        if (typeof form.requestSubmit === "function") {
          form.requestSubmit();
          return;
        }
        form.submit();
      });
    });
  }

  function clearDataStatusTarget(form) {
    if (!form || !form.querySelector) {
      return null;
    }

    var selector = String(form.getAttribute("data-clear-data-status-target") || "").trim();
    if (selector) {
      return document.querySelector(selector);
    }

    return form.querySelector("[data-clear-data-status]");
  }

  function openClearDataConfirm(question, acceptLabel) {
    if (typeof window.__ovumcyOpenConfirm === "function") {
      return window.__ovumcyOpenConfirm(question, acceptLabel);
    }
    return Promise.resolve(window.confirm(question));
  }

  function encodeFormForRequest(form) {
    var params = new URLSearchParams();
    var formData = new FormData(form);

    formData.forEach(function (value, key) {
      if (typeof value === "string") {
        params.append(key, value);
      }
    });

    return params.toString();
  }

  function initClearDataPasswordConfirmation() {
    document.addEventListener("input", function (event) {
      var field = event.target;
      if (!field || !field.matches || !field.matches("#settings-clear-data-password")) {
        return;
      }

      var form = field.form;
      if (!form || !form.matches || !form.matches("form[data-clear-data-verify-form]")) {
        return;
      }

      clearFormStatus(clearDataStatusTarget(form));
    });

    document.addEventListener("submit", function (event) {
      var form = event.target;
      var validateAction;
      var statusTarget;
      var invalidPasswordMessage;
      var requestFailedMessage;
      var confirmMessage;
      var confirmAcceptLabel;

      if (!form || !form.matches || !form.matches("form[data-clear-data-verify-form]")) {
        return;
      }

      if (form.dataset.clearDataConfirmBypass === "1") {
        form.dataset.clearDataConfirmBypass = "";
        return;
      }

      validateAction = String(form.getAttribute("data-clear-data-validate-action") || "").trim();
      if (!validateAction) {
        return;
      }

      event.preventDefault();
      statusTarget = clearDataStatusTarget(form);
      invalidPasswordMessage = String(form.getAttribute("data-clear-data-invalid-password") || "Invalid password.");
      requestFailedMessage = String(form.getAttribute("data-clear-data-request-failed") || "Request failed. Please try again.");
      confirmMessage = String(form.getAttribute("data-clear-data-confirm-message") || "");
      confirmAcceptLabel = String(form.getAttribute("data-clear-data-confirm-accept") || "");

      clearFormStatus(statusTarget);

      fetch(validateAction, {
        method: "POST",
        credentials: "same-origin",
        headers: {
          Accept: "application/json",
          "Content-Type": "application/x-www-form-urlencoded; charset=UTF-8"
        },
        body: encodeFormForRequest(form)
      })
        .then(function (response) {
          if (response.ok) {
            return true;
          }

          return response.json()
            .catch(function () {
              return null;
            })
            .then(function (payload) {
              var errorCode = payload && payload.error ? String(payload.error) : "";
              if (statusTarget) {
                renderErrorStatus(
                  statusTarget,
                  errorCode === "invalid password" ? invalidPasswordMessage : requestFailedMessage
                );
              }
              return false;
            });
        })
        .catch(function () {
          if (statusTarget) {
            renderErrorStatus(statusTarget, requestFailedMessage);
          }
          return false;
        })
        .then(function (validated) {
          if (!validated) {
            return;
          }

          return openClearDataConfirm(confirmMessage, confirmAcceptLabel).then(function (confirmed) {
            if (!confirmed) {
              return;
            }

            form.dataset.clearDataConfirmBypass = "1";
            if (typeof form.requestSubmit === "function") {
              form.requestSubmit();
              return;
            }
            form.submit();
          });
        });
    });
  }

  function formatCycleStartMessage(template, replacements) {
    var result = String(template || "");
    for (var index = 0; index < replacements.length; index++) {
      result = result.replace(/%[sd]/, String(replacements[index] || ""));
    }
    return result;
  }

  function openCycleStartConfirm(question, acceptLabel) {
    if (typeof window.__ovumcyOpenConfirm === "function") {
      return window.__ovumcyOpenConfirm(question, acceptLabel);
    }
    return Promise.resolve(window.confirm(question));
  }

  function findCycleStartPolicyNode(form) {
    if (!form || !form.parentElement || !form.parentElement.querySelector) {
      return null;
    }
    return form.parentElement.querySelector("[data-cycle-start-policy]");
  }

  function readCycleStartPolicy(form) {
    var policyNode = findCycleStartPolicyNode(form);
    var shortGap;
    if (!policyNode) {
      return null;
    }

    shortGap = parseInt(policyNode.getAttribute("data-cycle-start-short-gap") || "0", 10);
    if (!isFinite(shortGap)) {
      shortGap = 0;
    }

    return {
      hasConflict: policyNode.getAttribute("data-cycle-start-conflict") === "true",
      conflictDate: String(policyNode.getAttribute("data-cycle-start-conflict-date") || ""),
      targetDate: String(policyNode.getAttribute("data-cycle-start-target-date") || ""),
      shortGap: shortGap,
      previousDate: String(policyNode.getAttribute("data-cycle-start-previous-date") || ""),
      replaceMessage: String(policyNode.getAttribute("data-cycle-start-replace-message") || ""),
      replaceAccept: String(policyNode.getAttribute("data-cycle-start-replace-accept") || ""),
      shortGapMessage: String(policyNode.getAttribute("data-cycle-start-short-gap-message") || ""),
      shortGapAccept: String(policyNode.getAttribute("data-cycle-start-short-gap-accept") || "")
    };
  }

  function setCycleStartHiddenValue(form, selector, value) {
    var input = form.querySelector(selector);
    if (!input) {
      return;
    }
    input.value = value ? "true" : "false";
  }

  function submitCycleStartForm(form) {
    if (!form) {
      return;
    }

    form.dataset.cycleStartConfirmBypass = "1";
    if (typeof form.requestSubmit === "function") {
      form.requestSubmit();
      return;
    }
    form.submit();
  }

  function bindCycleStartConfirmForms() {
    document.addEventListener("submit", function (event) {
      var form = event.target;
      var policy;
      if (!form || !form.matches || !form.matches("form[data-cycle-start-confirm-form]")) {
        return;
      }

      if (typeof window.__ovumcyMaybeAcknowledgePeriodTip === "function") {
        window.__ovumcyMaybeAcknowledgePeriodTip(form);
      }

      if (form.dataset.cycleStartConfirmBypass === "1") {
        form.dataset.cycleStartConfirmBypass = "";
        return;
      }

      policy = readCycleStartPolicy(form);
      if (!policy || (!policy.hasConflict && policy.shortGap <= 0)) {
        return;
      }

      event.preventDefault();
      event.stopImmediatePropagation();
      setCycleStartHiddenValue(form, "[data-cycle-start-replace-input]", false);
      setCycleStartHiddenValue(form, "[data-cycle-start-uncertain-input]", false);

      Promise.resolve()
        .then(function () {
          if (!policy.hasConflict) {
            return true;
          }
          return openCycleStartConfirm(
            formatCycleStartMessage(policy.replaceMessage, [policy.conflictDate, policy.targetDate]),
            policy.replaceAccept
          ).then(function (confirmed) {
            if (confirmed) {
              setCycleStartHiddenValue(form, "[data-cycle-start-replace-input]", true);
            }
            return confirmed;
          });
        })
        .then(function (confirmed) {
          if (!confirmed || policy.shortGap <= 0) {
            return confirmed;
          }
          return openCycleStartConfirm(
            formatCycleStartMessage(policy.shortGapMessage, [policy.shortGap, policy.previousDate]),
            policy.shortGapAccept
          ).then(function (shortGapConfirmed) {
            if (shortGapConfirmed) {
              setCycleStartHiddenValue(form, "[data-cycle-start-uncertain-input]", true);
            }
            return shortGapConfirmed;
          });
        })
        .then(function (confirmed) {
          if (!confirmed) {
            return;
          }
          submitCycleStartForm(form);
        });
    }, true);
  }

  var PWA_INSTALL_DISMISS_STORAGE_KEY = "ovumcy_pwa_install_hidden_v1";
  var PWA_INSTALL_FALLBACK_DELAY_MS = 1200;

  var pwaInstallDeferredEvent = null;
  var pwaInstallFallbackTimer = 0;
  var pwaInstallSubscribers = [];
  var pwaInstallState = {
    available: false,
    busy: false,
    installed: false,
    mode: ""
  };

  function readLocalStorageValue(key) {
    if (!key) {
      return "";
    }
    try {
      return String(window.localStorage.getItem(key) || "");
    } catch {
      return "";
    }
  }

  function writeLocalStorageValue(key, value) {
    if (!key) {
      return;
    }
    try {
      window.localStorage.setItem(key, String(value || ""));
    } catch {
      // Ignore storage quota and privacy mode errors.
    }
  }

  function removeLocalStorageValue(key) {
    if (!key) {
      return;
    }
    try {
      window.localStorage.removeItem(key);
    } catch {
      // Ignore storage cleanup failures.
    }
  }

  function wasPWAInstallDismissed() {
    return readLocalStorageValue(PWA_INSTALL_DISMISS_STORAGE_KEY) === "1";
  }

  function storePWAInstallDismissed() {
    writeLocalStorageValue(PWA_INSTALL_DISMISS_STORAGE_KEY, "1");
  }

  function clearPWAInstallDismissed() {
    removeLocalStorageValue(PWA_INSTALL_DISMISS_STORAGE_KEY);
  }

  function isStandalonePWA() {
    if (window.matchMedia && window.matchMedia("(display-mode: standalone)").matches) {
      return true;
    }
    return window.navigator && window.navigator.standalone === true;
  }

  function pwaUserAgent() {
    if (!window.navigator) {
      return "";
    }
    return String(window.navigator.userAgent || window.navigator.vendor || "").toLowerCase();
  }

  function isIOSDevice() {
    var ua = pwaUserAgent();
    if (/iphone|ipad|ipod/.test(ua)) {
      return true;
    }
    return !!(window.navigator && window.navigator.platform === "MacIntel" && window.navigator.maxTouchPoints > 1);
  }

  function isLikelyMobileClient() {
    if (window.matchMedia) {
      if (window.matchMedia("(max-width: 640px)").matches) {
        return true;
      }
      if (window.matchMedia("(pointer: coarse)").matches && window.matchMedia("(max-width: 900px)").matches) {
        return true;
      }
    }

    return /android|iphone|ipad|ipod|mobile/.test(pwaUserAgent());
  }

  function clonePWAInstallState() {
    return {
      available: !!pwaInstallState.available,
      busy: !!pwaInstallState.busy,
      installed: !!pwaInstallState.installed,
      mode: String(pwaInstallState.mode || "")
    };
  }

  function emitPWAInstallState() {
    var snapshot = clonePWAInstallState();
    for (var index = 0; index < pwaInstallSubscribers.length; index++) {
      pwaInstallSubscribers[index](snapshot);
    }
  }

  function setPWAInstallState(nextState) {
    var safeState = nextState || {};
    pwaInstallState.available = !!safeState.available;
    pwaInstallState.busy = !!safeState.busy;
    pwaInstallState.installed = !!safeState.installed;
    pwaInstallState.mode = String(safeState.mode || "");
    emitPWAInstallState();
  }

  function clearPWAInstallFallbackTimer() {
    if (!pwaInstallFallbackTimer) {
      return;
    }
    window.clearTimeout(pwaInstallFallbackTimer);
    pwaInstallFallbackTimer = 0;
  }

  function schedulePWAInstallFallback() {
    if (isStandalonePWA() || wasPWAInstallDismissed()) {
      return;
    }

    clearPWAInstallFallbackTimer();
    pwaInstallFallbackTimer = window.setTimeout(function () {
      if (pwaInstallDeferredEvent || isStandalonePWA()) {
        return;
      }

      if (isIOSDevice()) {
        setPWAInstallState({
          available: true,
          busy: false,
          installed: false,
          mode: "ios"
        });
        return;
      }

      if (isLikelyMobileClient()) {
        setPWAInstallState({
          available: true,
          busy: false,
          installed: false,
          mode: "menu"
        });
      }
    }, PWA_INSTALL_FALLBACK_DELAY_MS);
  }

  function dismissPWAInstallPrompt() {
    pwaInstallDeferredEvent = null;
    clearPWAInstallFallbackTimer();
    storePWAInstallDismissed();
    setPWAInstallState({
      available: false,
      busy: false,
      installed: isStandalonePWA(),
      mode: ""
    });
  }

  function markPWAInstalled() {
    pwaInstallDeferredEvent = null;
    clearPWAInstallFallbackTimer();
    clearPWAInstallDismissed();
    setPWAInstallState({
      available: false,
      busy: false,
      installed: true,
      mode: ""
    });
  }

  function handleBeforeInstallPrompt(event) {
    if (!event) {
      return;
    }
    if (isStandalonePWA() || wasPWAInstallDismissed()) {
      return;
    }

    if (typeof event.preventDefault === "function") {
      event.preventDefault();
    }
    pwaInstallDeferredEvent = event;
    clearPWAInstallFallbackTimer();
    setPWAInstallState({
      available: true,
      busy: false,
      installed: false,
      mode: "prompt"
    });
  }

  function initPWAInstallPrompt() {
    if (window.__ovumcyPWAInstallInitialized) {
      return;
    }
    window.__ovumcyPWAInstallInitialized = true;

    window.addEventListener("beforeinstallprompt", handleBeforeInstallPrompt);
    window.addEventListener("appinstalled", markPWAInstalled);

    setPWAInstallState({
      available: false,
      busy: false,
      installed: isStandalonePWA(),
      mode: ""
    });

    schedulePWAInstallFallback();
  }

  function requestPWAInstallation() {
    if (!pwaInstallDeferredEvent || typeof pwaInstallDeferredEvent.prompt !== "function") {
      return Promise.resolve(false);
    }

    var installEvent = pwaInstallDeferredEvent;
    setPWAInstallState({
      available: true,
      busy: true,
      installed: false,
      mode: "prompt"
    });

    return Promise.resolve(installEvent.prompt())
      .catch(function () {
        return null;
      })
      .then(function () {
        return installEvent.userChoice;
      })
      .catch(function () {
        return { outcome: "dismissed" };
      })
      .then(function (choice) {
        var outcome = choice && choice.outcome ? String(choice.outcome) : "dismissed";
        pwaInstallDeferredEvent = null;

        if (outcome === "accepted") {
          markPWAInstalled();
          return true;
        }

        dismissPWAInstallPrompt();
        return false;
      });
  }

  function subscribePWAInstallState(listener) {
    if (typeof listener !== "function") {
      return function () {};
    }

    pwaInstallSubscribers.push(listener);
    listener(clonePWAInstallState());

    return function () {
      pwaInstallSubscribers = pwaInstallSubscribers.filter(function (candidate) {
        return candidate !== listener;
      });
    };
  }

  function renderErrorStatus(target, text) {
    target.textContent = "";
    var block = document.createElement("div");
    block.className = "status-error";
    block.textContent = text;
    target.appendChild(block);
  }

  function createToastStack() {
    var stack = document.createElement("div");
    stack.className = "toast-stack";
    document.body.appendChild(stack);
    return stack;
  }

  function appendToastMessage(body, message, kind) {
    var messageWrap = document.createElement("span");
    messageWrap.className = "toast-message-wrap";

    var icon = document.createElement("span");
    icon.className = "toast-icon";
    icon.setAttribute("aria-hidden", "true");
    if (kind === "error") {
      icon.classList.add("toast-icon-error");
      icon.textContent = "⚠";
    } else {
      icon.textContent = "✓";
    }
    messageWrap.appendChild(icon);

    var text = document.createElement("span");
    text.className = "toast-message";
    text.textContent = message;
    messageWrap.appendChild(text);

    body.appendChild(messageWrap);
  }

  var successStatusClearTimers = new WeakMap();

  function initToastAPI() {
    var stack = null;

    function getStack() {
      if (stack) {
        return stack;
      }
      stack = createToastStack();
      return stack;
    }

    window.showToast = function (message, kind) {
      if (!message) {
        return;
      }

      var container = getStack();
      var toast = document.createElement("div");
      toast.className = (kind === "error" ? "status-error" : "status-ok") + " reveal";
      var body = document.createElement("div");
      body.className = "toast-body";
      appendToastMessage(body, message, kind === "error" ? "error" : "ok");

      var closeButton = document.createElement("button");
      closeButton.type = "button";
      closeButton.className = "toast-close";
      closeButton.setAttribute("aria-label", document.body.getAttribute("data-toast-close") || "Close");
      closeButton.textContent = "×";
      closeButton.addEventListener("click", function () {
        toast.remove();
      });
      body.appendChild(closeButton);

      toast.appendChild(body);
      container.appendChild(toast);

      window.setTimeout(function () {
        if (!toast.parentNode) {
          return;
        }
        toast.classList.add("toast-exit");
        window.setTimeout(function () {
          toast.remove();
        }, TOAST_EXIT_MS);
      }, TOAST_VISIBLE_MS);
    };
  }

  function getSaveFeedbackFormFromEvent(event) {
    var target = getEventTarget(event);
    if (!target || !target.closest) {
      return null;
    }
    return target.closest("form[data-save-feedback]");
  }

  function setSaveButtonState(form, isBusy) {
    if (!form) {
      return;
    }
    var button = form.querySelector("[data-save-button]");
    if (!button) {
      return;
    }

    button.disabled = isBusy;
    if (isBusy) {
      button.setAttribute("aria-busy", "true");
      button.classList.add("btn-loading");
      return;
    }
    button.removeAttribute("aria-busy");
    button.classList.remove("btn-loading");
  }

  function clearStatusTargetIfEmpty(target) {
    if (!target || target.querySelector(".status-ok") || target.querySelector(".status-error")) {
      return;
    }
    target.textContent = "";
  }

  function closeLabelText() {
    return document.body.getAttribute("data-toast-close") || "Close";
  }

  function ensureDismissibleSuccessStatus(target) {
    if (!target || !target.querySelector) {
      return null;
    }

    var successNode = target.querySelector(".status-ok");
    if (!successNode) {
      return null;
    }

    if (successNode.querySelector(".toast-close")) {
      return successNode;
    }

    var message = String(successNode.textContent || "").trim();
    successNode.textContent = "";

    var body = document.createElement("div");
    body.className = "toast-body";
    appendToastMessage(body, message, "ok");

    var closeButton = document.createElement("button");
    closeButton.type = "button";
    closeButton.className = "toast-close";
    closeButton.setAttribute("aria-label", closeLabelText());
    closeButton.setAttribute("data-dismiss-status", "true");
    closeButton.textContent = "×";
    body.appendChild(closeButton);

    successNode.appendChild(body);
    return successNode;
  }

  function scheduleClearSuccessStatus(target) {
    var successNode = ensureDismissibleSuccessStatus(target);
    if (!successNode) {
      return;
    }

    var existingTimer = successStatusClearTimers.get(successNode);
    if (existingTimer) {
      window.clearTimeout(existingTimer);
      successStatusClearTimers.delete(successNode);
    }

    var timer = window.setTimeout(function () {
      if (!target.contains(successNode)) {
        successStatusClearTimers.delete(successNode);
        clearStatusTargetIfEmpty(target);
        return;
      }

      successNode.classList.add("toast-exit");
      window.setTimeout(function () {
        if (target.contains(successNode)) {
          successNode.remove();
        }
        successStatusClearTimers.delete(successNode);
        clearStatusTargetIfEmpty(target);
      }, TOAST_EXIT_MS);
    }, TOAST_VISIBLE_MS);
    successStatusClearTimers.set(successNode, timer);
  }

  function maybeShowSuccessToast(target) {
    var successNode;
    var message;
    if (!target || target.getAttribute("data-success-toast") !== "true" || typeof window.showToast !== "function") {
      return;
    }

    successNode = target.querySelector(".status-ok");
    if (!successNode) {
      return;
    }

    message = String(successNode.textContent || "").trim();
    if (!message || target.dataset.toastShown === message) {
      return;
    }

    target.dataset.toastShown = message;
    window.showToast(message, "ok");
  }

  function showResponseNotice(xhr) {
    var message;
    if (!xhr || typeof xhr.getResponseHeader !== "function" || typeof window.showToast !== "function") {
      return;
    }

    message = typeof window.__ovumcyDecodeResponseNoticeHeader === "function"
      ? window.__ovumcyDecodeResponseNoticeHeader(xhr.getResponseHeader("X-Ovumcy-Notice"))
      : String(xhr.getResponseHeader("X-Ovumcy-Notice") || "").trim();
    if (!message) {
      return;
    }
    window.showToast(message, "error");
  }

  function maybeRefreshDayEditor(target) {
    var dayEditor = document.getElementById("day-editor");
    var form = target.closest("form[data-save-feedback]");
    if (!dayEditor || !form || !form.closest("#day-editor")) {
      return;
    }

    if (window.htmx && typeof window.htmx.trigger === "function") {
      window.htmx.trigger(document.body, "calendar-day-updated");
    }

    var postPath = form.getAttribute("hx-post") || "";
    var match = postPath.match(/\/api\/days\/(\d{4}-\d{2}-\d{2})$/);
    if (match && window.htmx && typeof window.htmx.ajax === "function") {
      window.htmx.ajax("GET", "/calendar/day/" + match[1], { target: "#day-editor", swap: "innerHTML" });
    }
  }

  function initHTMXHooks() {
    document.body.addEventListener("htmx:configRequest", function (event) {
      var tokenMeta = document.querySelector('meta[name="csrf-token"]');
      if (!tokenMeta || !event || !event.detail) {
        return;
      }

      var token = tokenMeta.getAttribute("content");
      if (!token) {
        return;
      }

      event.detail.parameters = event.detail.parameters || {};
      event.detail.parameters.csrf_token = token;
      event.detail.headers = event.detail.headers || {};
      event.detail.headers["X-CSRF-Token"] = token;

      var timezone = currentClientTimezone();
      if (timezone) {
        event.detail.headers[TIMEZONE_HEADER_NAME] = timezone;
      }
    });

    document.body.addEventListener("htmx:beforeRequest", function (event) {
      var target = event && event.detail ? event.detail.target : null;
      if (target && target.classList && target.classList.contains("save-status")) {
        delete target.dataset.toastShown;
      }
      setSaveButtonState(getSaveFeedbackFormFromEvent(event), true);
    });

    document.body.addEventListener("htmx:afterRequest", function (event) {
      var form = getSaveFeedbackFormFromEvent(event);
      var xhr = event && event.detail ? event.detail.xhr : null;
      setSaveButtonState(form, false);
      showResponseNotice(xhr);
      if (form && form.matches && form.matches("[data-dashboard-save-form]") && typeof window.__ovumcyFinalizeDashboardManualSave === "function") {
        window.__ovumcyFinalizeDashboardManualSave(form, !!(event && event.detail && event.detail.successful));
      }
    });

    document.body.addEventListener("htmx:afterSwap", function (event) {
      var target = event && event.detail ? event.detail.target : null;
      if (!target || !target.classList || !target.classList.contains("save-status")) {
        return;
      }

      var successNode = target.querySelector(".status-ok");
      if (!successNode) {
        return;
      }

      maybeRefreshDayEditor(target);
      maybeShowSuccessToast(target);
      scheduleClearSuccessStatus(target);
    });

    document.body.addEventListener("htmx:afterSettle", function (event) {
      var target = event && event.detail ? event.detail.target : null;
      if (!target || !target.classList || !target.classList.contains("save-status")) {
        return;
      }
      scheduleClearSuccessStatus(target);
    });

    document.body.addEventListener("click", function (event) {
      var dismissButton = closestFromEvent(event, "button[data-dismiss-status]");
      if (!dismissButton) {
        return;
      }

      var statusNode = dismissButton.closest(".status-ok, .status-error");
      if (!statusNode) {
        return;
      }

      var parent = statusNode.parentElement;
      statusNode.remove();
      clearStatusTargetIfEmpty(parent);
    });

    document.body.addEventListener("htmx:responseError", function (event) {
      var target = event && event.detail ? event.detail.target : null;
      var form = getSaveFeedbackFormFromEvent(event);
      if (!target || !target.classList || !target.classList.contains("save-status")) {
        if (form && form.matches && form.matches("[data-dashboard-save-form]") && typeof window.__ovumcyFinalizeDashboardManualSave === "function") {
          window.__ovumcyFinalizeDashboardManualSave(form, false);
        }
        return;
      }

      var xhr = event.detail.xhr;
      var responseText = xhr && typeof xhr.responseText === "string" ? xhr.responseText : "";
      if (responseText && responseText.indexOf("status-error") !== -1) {
        target.innerHTML = responseText;
        return;
      }

      var fallback = document.body.getAttribute("data-request-failed") || "Request failed. Please try again.";
      renderErrorStatus(target, fallback);
      if (form && form.matches && form.matches("[data-dashboard-save-form]") && typeof window.__ovumcyFinalizeDashboardManualSave === "function") {
        window.__ovumcyFinalizeDashboardManualSave(form, false);
      }
    });
  }

  function copyTextWithExecCommand(text) {
    return new Promise(function (resolve, reject) {
      var textarea = document.createElement("textarea");
      textarea.value = text;
      textarea.setAttribute("readonly", "readonly");
      textarea.className = "clipboard-helper";
      textarea.setAttribute("aria-hidden", "true");
      textarea.tabIndex = -1;
      document.body.appendChild(textarea);
      textarea.select();

      try {
        var copied = document.execCommand("copy");
        document.body.removeChild(textarea);
        if (copied) {
          resolve();
          return;
        }
      } catch {
        document.body.removeChild(textarea);
      }

      reject(new Error("copy_failed"));
    });
  }

  function writeTextToClipboard(text) {
    if (navigator.clipboard && typeof navigator.clipboard.writeText === "function") {
      return navigator.clipboard.writeText(text).catch(function () {
        return copyTextWithExecCommand(text);
      });
    }

    return copyTextWithExecCommand(text);
  }

  function setNodeHidden(node, hidden) {
    if (!node) {
      return;
    }
    if (hidden) {
      node.setAttribute("hidden", "");
      return;
    }
    node.removeAttribute("hidden");
  }

  function parseDateValue(value) {
    var normalized = String(value || "").trim();
    if (!normalized) {
      return null;
    }

    var match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(normalized);
    if (!match) {
      return null;
    }

    var year = Number(match[1]);
    var month = Number(match[2]) - 1;
    var day = Number(match[3]);
    var parsed = new Date(year, month, day);
    if (
      isNaN(parsed.getTime()) ||
      parsed.getFullYear() !== year ||
      parsed.getMonth() !== month ||
      parsed.getDate() !== day
    ) {
      return null;
    }
    return parsed;
  }

  function formatDateValue(value) {
    var year = value.getFullYear();
    var month = String(value.getMonth() + 1).padStart(2, "0");
    var day = String(value.getDate()).padStart(2, "0");
    return year + "-" + month + "-" + day;
  }

  function decodeResponseNoticeHeader(raw) {
    var value = String(raw || "").trim();
    if (!value) {
      return "";
    }

    try {
      return decodeURIComponent(value.replace(/\+/g, "%20")).trim();
    } catch {
      return value;
    }
  }

  function buildDayOptions(minDateRaw, maxDateRaw, locale, todayLabel) {
    var minDate = parseDateValue(minDateRaw);
    var maxDate = parseDateValue(maxDateRaw);
    if (!minDate || !maxDate || minDate > maxDate) {
      return [];
    }

    var result = [];
    var formatter = new Intl.DateTimeFormat(locale || "en", {
      day: "numeric",
      month: "short"
    });

    for (var cursor = new Date(maxDate); cursor >= minDate; cursor.setDate(cursor.getDate() - 1)) {
      var current = new Date(cursor);
      var isToday = formatDateValue(current) === formatDateValue(maxDate);
      result.push({
        value: formatDateValue(current),
        label: isToday && todayLabel ? String(todayLabel) : formatter.format(current),
        secondaryLabel: isToday ? formatter.format(current) : "",
        isToday: isToday
      });
    }
    return result;
  }

  function sanitizeDateFieldDigits(raw, maxDigits) {
    return String(raw || "").replace(/\D/g, "").slice(0, maxDigits);
  }

  function padDateFieldSegment(raw, targetLength) {
    var sanitized = String(raw || "").trim();
    if (!sanitized) {
      return "";
    }
    return sanitized.length >= targetLength ? sanitized : sanitized.padStart(targetLength, "0");
  }

  function findDateFieldRoot(target) {
    if (!target) {
      return null;
    }
    if (target.matches && target.matches("[data-date-field]")) {
      return target;
    }
    return target.closest ? target.closest("[data-date-field]") : null;
  }

  function createLocalizedDateFieldController(root) {
    if (!root || !root.querySelector) {
      return null;
    }
    if (root.__ovumcyDateFieldController) {
      return root.__ovumcyDateFieldController;
    }

    var transportInput = root.querySelector("[data-date-field-value]");
    var dayInput = root.querySelector('[data-date-field-part="day"]');
    var monthInput = root.querySelector('[data-date-field-part="month"]');
    var yearInput = root.querySelector('[data-date-field-part="year"]');
    var openButton = root.querySelector("[data-date-field-open]");
    if (!transportInput || !dayInput || !monthInput || !yearInput) {
      return null;
    }

    var required = transportInput.getAttribute("data-date-field-required") === "true";
    var invalidMessage = String(root.getAttribute("data-date-field-invalid-message") || "Use a valid date.");
    var requiredMessage = String(root.getAttribute("data-date-field-required-message") || "Please enter a date.");
    var outOfRangeMessage = String(root.getAttribute("data-date-field-out-of-range-message") || "Choose a date in the allowed range.");
    var minDate = parseDateValue(transportInput.getAttribute("min") || "");
    var maxDate = parseDateValue(transportInput.getAttribute("max") || "");
    var currentValidationMessage = "";
    var syncingTransport = false;
    var syncingSegments = false;

    function setFieldValidation(message) {
      currentValidationMessage = String(message || "");
      dayInput.setCustomValidity(currentValidationMessage);
      monthInput.setCustomValidity(currentValidationMessage);
      yearInput.setCustomValidity(currentValidationMessage);
    }

    function readSegmentState() {
      var day = sanitizeDateFieldDigits(dayInput.value, 2);
      var month = sanitizeDateFieldDigits(monthInput.value, 2);
      var year = sanitizeDateFieldDigits(yearInput.value, 4);

      if (!day && !month && !year) {
        return {
          empty: true,
          valid: true,
          value: "",
          date: null
        };
      }

      if (day.length !== 2 || month.length !== 2 || year.length !== 4) {
        return {
          empty: false,
          valid: false,
          reason: "incomplete",
          value: ""
        };
      }

      var isoValue = year + "-" + month + "-" + day;
      var parsed = parseDateValue(isoValue);
      if (!parsed) {
        return {
          empty: false,
          valid: false,
          reason: "invalid",
          value: ""
        };
      }

      if ((minDate && parsed < minDate) || (maxDate && parsed > maxDate)) {
        return {
          empty: false,
          valid: false,
          reason: "out_of_range",
          value: isoValue,
          date: parsed
        };
      }

      return {
        empty: false,
        valid: true,
        value: isoValue,
        date: parsed
      };
    }

    function commitTransportValue(value, notify) {
      var nextValue = String(value || "");
      var changed = transportInput.value !== nextValue;
      transportInput.value = nextValue;
      if (notify && changed) {
        transportInput.dispatchEvent(new Event("input", { bubbles: true }));
        transportInput.dispatchEvent(new Event("change", { bubbles: true }));
      }
    }

    function syncSegmentsFromTransport() {
      if (syncingSegments) {
        return;
      }
      syncingTransport = true;
      var parsed = parseDateValue(transportInput.value);
      if (!parsed) {
        dayInput.value = "";
        monthInput.value = "";
        yearInput.value = "";
      } else {
        dayInput.value = String(parsed.getDate()).padStart(2, "0");
        monthInput.value = String(parsed.getMonth() + 1).padStart(2, "0");
        yearInput.value = String(parsed.getFullYear());
      }
      syncingTransport = false;
    }

    function syncTransportFromSegments(notify) {
      if (syncingTransport) {
        return;
      }

      syncingSegments = true;
      var state = readSegmentState();
      if (state.valid || state.reason === "out_of_range") {
        commitTransportValue(state.value, notify);
      } else {
        commitTransportValue("", notify);
      }
      syncingSegments = false;
    }

    function clearValidation() {
      setFieldValidation("");
    }

    function validate(options) {
      var state = readSegmentState();
      var resolvedInvalidMessage = options && options.invalidMessage ? String(options.invalidMessage) : invalidMessage;
      var resolvedRequiredMessage = options && options.requiredMessage ? String(options.requiredMessage) : requiredMessage;
      var resolvedOutOfRangeMessage = options && options.outOfRangeMessage ? String(options.outOfRangeMessage) : outOfRangeMessage;

      if (state.empty) {
        commitTransportValue("", false);
        if (required) {
          setFieldValidation(resolvedRequiredMessage);
          return false;
        }
        clearValidation();
        return true;
      }

      if (!state.valid) {
        if (state.reason === "out_of_range") {
          commitTransportValue(state.value, false);
          setFieldValidation(resolvedOutOfRangeMessage);
          return false;
        }

        commitTransportValue("", false);
        setFieldValidation(resolvedInvalidMessage);
        return false;
      }

      commitTransportValue(state.value, false);
      clearValidation();
      return true;
    }

    function focusFirstEditable() {
      var day = sanitizeDateFieldDigits(dayInput.value, 2);
      var month = sanitizeDateFieldDigits(monthInput.value, 2);
      var year = sanitizeDateFieldDigits(yearInput.value, 4);

      if (day.length < 2) {
        dayInput.focus();
        return dayInput;
      }
      if (month.length < 2) {
        monthInput.focus();
        return monthInput;
      }
      if (year.length < 4) {
        yearInput.focus();
        return yearInput;
      }
      dayInput.focus();
      return dayInput;
    }

    function reportValidity() {
      var target = focusFirstEditable();
      return target && typeof target.reportValidity === "function" ? target.reportValidity() : false;
    }

    function setSegmentValue(input, rawValue, maxDigits) {
      var nextValue = sanitizeDateFieldDigits(rawValue, maxDigits);
      if (input.value !== nextValue) {
        input.value = nextValue;
      }
    }

    function maybeAdvanceFocus(input, maxDigits, nextInput) {
      if (!nextInput) {
        return;
      }
      if (sanitizeDateFieldDigits(input.value, maxDigits).length === maxDigits && document.activeElement === input) {
        nextInput.focus();
        nextInput.select();
      }
    }

    function handleSegmentInput(input, maxDigits, nextInput) {
      return function () {
        setSegmentValue(input, input.value, maxDigits);
        clearValidation();
        syncTransportFromSegments(true);
        maybeAdvanceFocus(input, maxDigits, nextInput);
      };
    }

    function handleSegmentBlur(input, maxDigits) {
      return function () {
        var nextValue = sanitizeDateFieldDigits(input.value, maxDigits);
        if (maxDigits === 2 && nextValue.length === 1) {
          nextValue = padDateFieldSegment(nextValue, 2);
        }
        if (input.value !== nextValue) {
          input.value = nextValue;
        }
        syncTransportFromSegments(true);
      };
    }

    dayInput.addEventListener("input", handleSegmentInput(dayInput, 2, monthInput));
    monthInput.addEventListener("input", handleSegmentInput(monthInput, 2, yearInput));
    yearInput.addEventListener("input", handleSegmentInput(yearInput, 4, null));

    dayInput.addEventListener("blur", handleSegmentBlur(dayInput, 2));
    monthInput.addEventListener("blur", handleSegmentBlur(monthInput, 2));
    yearInput.addEventListener("blur", handleSegmentBlur(yearInput, 4));

    transportInput.addEventListener("input", function () {
      if (!syncingSegments) {
        clearValidation();
        syncSegmentsFromTransport();
      }
    });
    transportInput.addEventListener("change", function () {
      if (!syncingSegments) {
        clearValidation();
        syncSegmentsFromTransport();
      }
    });

    syncSegmentsFromTransport();
    clearValidation();

    root.__ovumcyDateFieldController = {
      root: root,
      input: transportInput,
      dayInput: dayInput,
      monthInput: monthInput,
      yearInput: yearInput,
      openButton: openButton,
      isCustom: true,
      getValue: function () {
        return String(transportInput.value || "");
      },
      setValue: function (value) {
        commitTransportValue(value, false);
        syncSegmentsFromTransport();
        clearValidation();
      },
      clear: function () {
        commitTransportValue("", false);
        syncSegmentsFromTransport();
        clearValidation();
      },
      readState: readSegmentState,
      validate: validate,
      validationMessage: function () {
        return currentValidationMessage;
      },
      setCustomValidity: setFieldValidation,
      reportValidity: reportValidity,
      focus: function () {
        focusFirstEditable();
      },
      setDisabled: function (disabled) {
        var nextDisabled = !!disabled;
        transportInput.disabled = nextDisabled;
        dayInput.disabled = nextDisabled;
        monthInput.disabled = nextDisabled;
        yearInput.disabled = nextDisabled;
        if (openButton) {
          openButton.disabled = nextDisabled;
          openButton.setAttribute("aria-disabled", nextDisabled ? "true" : "false");
        }
      }
    };

    return root.__ovumcyDateFieldController;
  }

  window.__ovumcyDecodeResponseNoticeHeader = decodeResponseNoticeHeader;

  function bindLocalizedDateFields(scope) {
    var root = scope && scope.querySelectorAll ? scope : document;
    var fields = root.querySelectorAll("[data-date-field]");
    for (var index = 0; index < fields.length; index++) {
      createLocalizedDateFieldController(fields[index]);
    }
  }

  function getLocalizedDateFieldController(target) {
    var root = findDateFieldRoot(target);
    if (!root) {
      return null;
    }
    return createLocalizedDateFieldController(root);
  }

  window.__ovumcyBindLocalizedDateFields = bindLocalizedDateFields;
  window.__ovumcyGetDateFieldController = getLocalizedDateFieldController;

  function fieldCharacterLength(value) {
    return Array.from(String(value || "")).length;
  }

  function getRecoveryCodeText(refs) {
    var node = refs && refs.code ? refs.code : null;
    return node ? String(node.textContent || "").trim() : "";
  }

  function collectCheckedSymptomLabels(scope) {
    if (!scope || !scope.querySelectorAll) {
      return [];
    }

    var checked = scope.querySelectorAll("input[name='symptom_ids']:checked");
    var labels = [];
    for (var index = 0; index < checked.length; index++) {
      var label = String(checked[index].dataset.symptomLabel || "").trim();
      if (label) {
        labels.push(label);
      }
    }
    return labels;
  }

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
    var currentLength = fieldCharacterLength(input.value);
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

  function syncDashboardNotesCounter(group) {
    if (!group || !group.querySelector) {
      return;
    }

    var input = group.querySelector("[data-dashboard-notes]");
    var counter = group.querySelector("[data-dashboard-notes-count]");
    if (!input || !counter) {
      return;
    }

    var maxLength = parseInt(input.getAttribute("maxlength") || "", 10);
    var currentLength = fieldCharacterLength(input.value);
    if (maxLength > 0) {
      counter.textContent = String(currentLength) + "/" + String(maxLength);
      return;
    }

    counter.textContent = String(currentLength);
  }

  function bindDashboardNotesCounters(root) {
    var scope = root && root.querySelectorAll ? root : document;
    var counters = scope.querySelectorAll("[data-dashboard-notes-count]");

    for (var index = 0; index < counters.length; index++) {
      var counter = counters[index];
      var group = typeof counter.closest === "function" ? counter.closest("[data-dashboard-notes-field-group]") : null;
      if (!group) {
        continue;
      }

      var input = group.querySelector("[data-dashboard-notes]");
      if (!input) {
        continue;
      }

      if (input.dataset.dashboardNotesCounterBound !== "1") {
        input.dataset.dashboardNotesCounterBound = "1";
        input.addEventListener("input", function () {
          var ownerGroup = typeof this.closest === "function" ? this.closest("[data-dashboard-notes-field-group]") : null;
          syncDashboardNotesCounter(ownerGroup);
        });
      }

      syncDashboardNotesCounter(group);
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

    var checkbox = form.querySelector("[data-recovery-code-checkbox], #recovery-code-saved");
    var submit = form.querySelector("[data-recovery-code-submit]");
    var enabled = !!(checkbox && checkbox.checked);
    if (!submit) {
      return;
    }

    submit.setAttribute("aria-disabled", enabled ? "false" : "true");
    submit.dataset.recoveryCodeReady = enabled ? "true" : "false";
  }

  function bindRecoveryCodeConfirmForms() {
    var forms = document.querySelectorAll("[data-recovery-code-confirm]");
    for (var index = 0; index < forms.length; index++) {
      var form = forms[index];
      if (form.dataset.recoveryConfirmBound !== "1") {
        form.dataset.recoveryConfirmBound = "1";
        form.addEventListener("input", function () {
          syncRecoveryCodeConfirmForm(this);
        });
        form.addEventListener("change", function () {
          syncRecoveryCodeConfirmForm(this);
        });
        form.addEventListener("click", function () {
          var currentForm = this;
          window.setTimeout(function () {
            syncRecoveryCodeConfirmForm(currentForm);
          }, 0);
        });
        form.addEventListener("submit", function (event) {
          var checkbox = this.querySelector("[data-recovery-code-checkbox], #recovery-code-saved");
          if (!checkbox || checkbox.checked) {
            syncRecoveryCodeConfirmForm(this);
            return;
          }

          event.preventDefault();
          syncRecoveryCodeConfirmForm(this);
          if (typeof checkbox.reportValidity === "function") {
            checkbox.reportValidity();
            return;
          }
          if (typeof checkbox.focus === "function") {
            checkbox.focus();
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
    bindDashboardNotesCounters(document);
    bindSettingsCycleForms();
    bindIconControls();
    bindDashboardEditors();
    bindDayEditorForms();
    bindCalendarViews();
    bindOnboardingFlows();
    bindRecoveryCodeTools();
    bindRecoveryCodeConfirmForms();
  }

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
