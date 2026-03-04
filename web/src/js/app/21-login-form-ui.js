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
    if (input.type === "email" && input.validity.typeMismatch) {
      input.setCustomValidity(emailMessage);
    }
  }

  function bindRequiredFieldValidation(form, requiredMessage, emailMessage) {
    if (!form) {
      return;
    }

    var fields = form.querySelectorAll("input[required]");
    for (var index = 0; index < fields.length; index++) {
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

  function initLoginValidation() {
    var form = document.getElementById("login-form");
    if (!form) {
      return;
    }

    var requiredMessage = form.getAttribute("data-required-message") || "Please fill out this field.";
    var emailMessage = form.getAttribute("data-email-message") || "Please enter a valid email address.";
    bindRequiredFieldValidation(form, requiredMessage, emailMessage);
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

    function clearStatus() {
      if (!statusTarget) {
        return;
      }
      statusTarget.textContent = "";
    }

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
      clearStatus();
      isPasswordMatchValid();
    }

    passwordField.addEventListener("input", handlePasswordInput);
    confirmField.addEventListener("input", handlePasswordInput);

    form.addEventListener("submit", function (event) {
      clearStatus();
      if (isPasswordMatchValid()) {
        return;
      }

      event.preventDefault();
      renderFormStatusError(statusTarget, mismatchMessage);
      if (typeof confirmField.reportValidity === "function") {
        confirmField.reportValidity();
      }
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

