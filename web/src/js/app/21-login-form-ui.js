  var authEmailLocalPattern = /^[A-Za-z0-9.!#$%&'*+/=?^_`{|}~-]+$/;
  var authEmailDomainLabelPattern = /^[A-Za-z0-9-]+$/;

  function configureEmailField(input) {
    if (!input || input.type !== "email") {
      return;
    }

    input.removeAttribute("pattern");
    input.setAttribute("autocapitalize", "none");
    input.setAttribute("spellcheck", "false");
  }

  function isAuthEmailValueValid(value) {
    var normalized = String(value || "").trim();
    var atIndex;
    var localPart;
    var domainPart;
    var domainLabels;
    var index;
    var label;

    if (!normalized) {
      return true;
    }

    if (/[^\u0021-\u007E]/.test(normalized)) {
      return false;
    }

    atIndex = normalized.indexOf("@");
    if (atIndex <= 0 || atIndex !== normalized.lastIndexOf("@") || atIndex >= normalized.length - 1) {
      return false;
    }

    localPart = normalized.slice(0, atIndex);
    domainPart = normalized.slice(atIndex + 1);
    if (!authEmailLocalPattern.test(localPart)) {
      return false;
    }
    if (localPart.charAt(0) === "." || localPart.charAt(localPart.length - 1) === "." || localPart.indexOf("..") !== -1) {
      return false;
    }

    domainLabels = domainPart.split(".");
    if (domainLabels.length < 2) {
      return false;
    }

    for (index = 0; index < domainLabels.length; index++) {
      label = domainLabels[index];
      if (!label || !authEmailDomainLabelPattern.test(label)) {
        return false;
      }
      if (label.charAt(0) === "-" || label.charAt(label.length - 1) === "-") {
        return false;
      }
    }

    return true;
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
    if (input.type === "email" && (input.validity.typeMismatch || !isAuthEmailValueValid(input.value))) {
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

  var passwordUpperPattern;
  var passwordLowerPattern;
  var passwordDigitPattern;
  try {
    passwordUpperPattern = new RegExp("\\p{Lu}", "u");
    passwordLowerPattern = new RegExp("\\p{Ll}", "u");
    passwordDigitPattern = new RegExp("\\p{Nd}", "u");
  } catch {
    passwordUpperPattern = /[A-Z]/;
    passwordLowerPattern = /[a-z]/;
    passwordDigitPattern = /\d/;
  }

  function passwordStrengthState(password) {
    var value = String(password || "");
    return {
      length: Array.from(value).length >= 8,
      upper: passwordUpperPattern.test(value),
      lower: passwordLowerPattern.test(value),
      digit: passwordDigitPattern.test(value)
    };
  }

  function isPasswordStrengthValid(password) {
    var state = passwordStrengthState(password);
    return state.length && state.upper && state.lower && state.digit;
  }

  function updatePasswordGuidance(guidanceRoot, password) {
    if (!guidanceRoot || !guidanceRoot.querySelectorAll) {
      return;
    }

    var state = passwordStrengthState(password);
    var items = guidanceRoot.querySelectorAll("[data-password-rule-item]");
    for (var index = 0; index < items.length; index++) {
      var item = items[index];
      var rule = String(item.getAttribute("data-password-rule-item") || "");
      var met = !!state[rule];
      item.setAttribute("data-met", met ? "true" : "false");
      item.classList.toggle("password-requirements-item-met", met);
      item.classList.toggle("password-requirements-item-pending", !met);
      var icon = item.querySelector("[data-password-rule-icon]");
      if (icon) {
        icon.textContent = met ? "✓" : "•";
      }
    }
  }

  function stopInvalidSubmit(event) {
    if (!event) {
      return;
    }

    event.preventDefault();
    if (typeof event.stopImmediatePropagation === "function") {
      event.stopImmediatePropagation();
      return;
    }
    if (typeof event.stopPropagation === "function") {
      event.stopPropagation();
    }
  }

  function passwordStrengthErrorMessage(passwordField, weakMessage) {
    var password = String(passwordField && passwordField.value || "");
    if (!password || isPasswordStrengthValid(password)) {
      return "";
    }
    return weakMessage;
  }

  function passwordMismatchErrorMessage(passwordField, confirmField, mismatchMessage) {
    var password = String(passwordField && passwordField.value || "");
    var confirm = String(confirmField && confirmField.value || "");
    if (!password || !confirm || password === confirm) {
      return "";
    }
    return mismatchMessage;
  }

  function bindPasswordFormValidation(options) {
    var form = options && options.form;
    var passwordField = options && options.passwordField;
    var confirmField = options && options.confirmField;
    if (!form || !passwordField || !confirmField) {
      return;
    }

    var requiredMessage = options.requiredMessage || "Please fill out this field.";
    var emailMessage = options.emailMessage || "Please enter a valid email address.";
    var mismatchMessage = options.mismatchMessage || "Passwords do not match.";
    var weakMessage = options.weakMessage || "Use a stronger password.";
    var statusTarget = options.statusTarget || null;
    var guidanceRoot = options.guidanceRoot || null;

    bindRequiredFieldValidation(form, requiredMessage, emailMessage);

    function clearValidationStatus() {
      clearFormStatus(statusTarget);
      clearAuthServerError(form);
    }

    function syncPasswordState() {
      updatePasswordGuidance(guidanceRoot, passwordField.value);
    }

    syncPasswordState();

    passwordField.addEventListener("input", function () {
      clearValidationStatus();
      syncPasswordState();
    });
    confirmField.addEventListener("input", clearValidationStatus);
    form.addEventListener("input", function () {
      clearValidationStatus();
    });

    form.addEventListener("submit", function (event) {
      var invalidField;
      var weakPasswordError;
      var mismatchError;

      clearValidationStatus();
      syncPasswordState();

      invalidField = firstInvalidRequiredField(form, requiredMessage, emailMessage);
      if (invalidField) {
        stopInvalidSubmit(event);
        moveFormStatusTarget(statusTarget, invalidField);
        renderFormStatusError(statusTarget, invalidField.validationMessage || requiredMessage);
        invalidField.focus();
        return;
      }

      weakPasswordError = passwordStrengthErrorMessage(passwordField, weakMessage);
      if (weakPasswordError) {
        stopInvalidSubmit(event);
        moveFormStatusTarget(statusTarget, passwordField);
        renderFormStatusError(statusTarget, weakPasswordError);
        focusLoginPasswordField(passwordField);
        return;
      }

      mismatchError = passwordMismatchErrorMessage(passwordField, confirmField, mismatchMessage);
      if (!mismatchError) {
        return;
      }

      stopInvalidSubmit(event);
      moveFormStatusTarget(statusTarget, confirmField);
      renderFormStatusError(statusTarget, mismatchError);
      focusLoginPasswordField(confirmField);
    }, true);
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
    var weakMessage = form.getAttribute("data-weak-password-message") || "Use a stronger password.";

    var passwordField = document.getElementById("register-password");
    var confirmField = document.getElementById("register-confirm-password");
    if (!passwordField || !confirmField) {
      return;
    }

    var statusTarget = document.getElementById("register-client-status");
    bindPasswordFormValidation({
      form: form,
      passwordField: passwordField,
      confirmField: confirmField,
      statusTarget: statusTarget,
      guidanceRoot: form.querySelector("[data-password-guidance]"),
      requiredMessage: requiredMessage,
      emailMessage: emailMessage,
      mismatchMessage: mismatchMessage,
      weakMessage: weakMessage
    });
  }

  function initSettingsPasswordValidation() {
    var form = document.getElementById("settings-change-password-form");
    if (!form) {
      return;
    }

    var passwordField = document.getElementById("settings-new-password");
    var confirmField = document.getElementById("settings-confirm-password");
    if (!passwordField || !confirmField) {
      return;
    }

    bindPasswordFormValidation({
      form: form,
      passwordField: passwordField,
      confirmField: confirmField,
      statusTarget: document.getElementById("settings-change-password-status"),
      guidanceRoot: form.querySelector("[data-password-guidance]"),
      requiredMessage: form.getAttribute("data-required-message") || "Please fill out this field.",
      mismatchMessage: form.getAttribute("data-password-mismatch-message") || "Passwords do not match.",
      weakMessage: form.getAttribute("data-weak-password-message") || "Use a stronger password."
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
