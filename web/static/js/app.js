(function () {
  "use strict";

  var PASSWORD_HIDE_ICON = "\u{1F648}";
  var PASSWORD_SHOW_ICON = "\u{1F441}";
  var TOAST_VISIBLE_MS = 5200;
  var TOAST_EXIT_MS = 220;
  var STATUS_CLEAR_MS = 2000;
  var DOWNLOAD_REVOKE_MS = 500;
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
    var cookie = name + "=" + encodeURIComponent(value) +
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

  function parseLanguage(raw) {
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
    if (normalized !== "en" && normalized !== "ru") {
      return "";
    }
    return normalized;
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
      panel.style.opacity = "0";
      panel.style.transform = "translateY(8px)";
      panel.style.transition = "opacity 180ms ease, transform 180ms ease";
      window.requestAnimationFrame(function () {
        panel.style.opacity = "1";
        panel.style.transform = "translateY(0)";
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
      panel.style.pointerEvents = "none";
      panel.style.opacity = "0";
      panel.style.transform = "translateY(-6px)";
      window.setTimeout(function () {
        window.location.href = href;
      }, 140);
    });
  }

  function updatePasswordToggleLabel(button, isVisible) {
    var showLabel = button.getAttribute("data-show-label") || "Show password";
    var hideLabel = button.getAttribute("data-hide-label") || "Hide password";
    button.setAttribute("aria-label", isVisible ? hideLabel : showLabel);
    button.textContent = isVisible ? PASSWORD_HIDE_ICON : PASSWORD_SHOW_ICON;
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

  function initLoginValidation() {
    var form = document.getElementById("login-form");
    if (!form) {
      return;
    }

    var requiredMessage = form.getAttribute("data-required-message") || "Please fill out this field.";
    var emailMessage = form.getAttribute("data-email-message") || "Please enter a valid email address.";

    function updateValidityMessage(input) {
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

    var fields = form.querySelectorAll("input[required]");
    for (var index = 0; index < fields.length; index++) {
      fields[index].addEventListener("invalid", function () {
        updateValidityMessage(this);
      });
      fields[index].addEventListener("input", function () {
        this.setCustomValidity("");
      });
      fields[index].addEventListener("blur", function () {
        updateValidityMessage(this);
      });
    }
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
      setSaveButtonState(getSaveFeedbackFormFromEvent(event), true);
    });

    document.body.addEventListener("htmx:afterRequest", function (event) {
      setSaveButtonState(getSaveFeedbackFormFromEvent(event), false);
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
      if (!target || !target.classList || !target.classList.contains("save-status")) {
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
    });
  }

  function copyTextWithExecCommand(text) {
    return new Promise(function (resolve, reject) {
      var textarea = document.createElement("textarea");
      textarea.value = text;
      textarea.setAttribute("readonly", "readonly");
      textarea.style.position = "fixed";
      textarea.style.opacity = "0";
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

  function parseDateValue(value) {
    if (!value) {
      return null;
    }
    var parsed = new Date(value + "T00:00:00");
    if (isNaN(parsed.getTime())) {
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

  function buildDayOptions(minDateRaw, maxDateRaw, locale) {
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
      result.push({
        value: formatDateValue(current),
        label: formatter.format(current)
      });
    }
    return result;
  }

  function setTimedFlag(target, key, timeoutMs) {
    target[key] = true;
    window.setTimeout(function () {
      target[key] = false;
    }, timeoutMs);
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

  window.appShellState = function () {
    return {
      mobileMenu: false,
      toggleMobileMenu: function () {
        this.mobileMenu = !this.mobileMenu;
      }
    };
  };

  window.settingsCycleForm = function (config) {
    var safeConfig = config || {};
    return {
      cycleLength: Number(safeConfig.cycleLength || 28),
      periodLength: Number(safeConfig.periodLength || 5),
      autoPeriodFill: !!safeConfig.autoPeriodFill
    };
  };

  function clearCheckedInputs(root, selector) {
    if (!root || !root.querySelectorAll) {
      return;
    }
    var inputs = root.querySelectorAll(selector);
    for (var index = 0; index < inputs.length; index++) {
      var input = inputs[index];
      input.checked = false;
      if (input.removeAttribute) {
        input.removeAttribute("checked");
      }
    }
  }

  window.dayEditorForm = function (config) {
    var safeConfig = config || {};
    return {
      isPeriod: !!safeConfig.isPeriod,
      clearNonPeriodSelections: function () {
        clearCheckedInputs(this.$root, "input[name='symptom_ids']");
      },
      init: function () {
        this.$watch("isPeriod", function (value) {
          if (!value) {
            this.clearNonPeriodSelections();
          }
        }.bind(this));
      }
    };
  };

  window.dashboardTodayEditor = function (config) {
    var safeConfig = config || {};

    return {
      isPeriod: !!safeConfig.isPeriod,
      activeSymptoms: [],
      notesPreview: "",
      syncSymptoms: function () {
        this.activeSymptoms = collectCheckedSymptomLabels(this.$root);
      },
      hasActiveSymptoms: function () {
        return this.activeSymptoms.length > 0;
      },
      hasNotesPreview: function () {
        return String(this.notesPreview || "").trim().length > 0;
      },
      clearNonPeriodSelections: function () {
        clearCheckedInputs(this.$root, "input[name='symptom_ids']");
        this.syncSymptoms();
      },
      init: function () {
        var notesField = this.$root ? this.$root.querySelector("#today-notes") : null;
        this.notesPreview = notesField ? String(notesField.value || "") : "";
        this.syncSymptoms();
        this.$watch("isPeriod", function (value) {
          if (!value) {
            this.clearNonPeriodSelections();
          }
        }.bind(this));
      }
    };
  };

  window.calendarView = function (config) {
    var safeConfig = config || {};
    return {
      selectedDate: safeConfig.selectedDate || "",
      isSelectedDay: function (value) {
        return this.selectedDate === String(value || "");
      },
      selectDayFromEvent: function (event) {
        var target = event && event.currentTarget ? event.currentTarget : null;
        if (!target || typeof target.getAttribute !== "function") {
          return;
        }
        this.selectDay(target.getAttribute("data-day"));
      },
      selectDay: function (value) {
        this.selectedDate = value || "";
        if (!this.selectedDate || !window.history || typeof window.history.replaceState !== "function") {
          return;
        }

        try {
          var currentURL = new URL(window.location.href);
          currentURL.searchParams.set("day", this.selectedDate);
          var nextPath = currentURL.pathname + currentURL.search + currentURL.hash;
          window.history.replaceState({}, "", nextPath);
        } catch {
          // Ignore malformed URLs and keep current location unchanged.
        }
      }
    };
  };

  window.onboardingFlow = function (config) {
    var safeConfig = config || {};
    var lang = safeConfig.lang || "en";
    var periodExceedsCycleMessage = String(
      safeConfig.periodExceedsCycleMessage || "Period length must not exceed cycle length."
    );
    function normalizeOnboardingStep(rawStep) {
      var step = Number(rawStep);
      if (!Number.isFinite(step)) {
        step = 0;
      }
      step = Math.round(step);
      if (step < 0) {
        return 0;
      }
      if (step > 3) {
        return 3;
      }
      return step;
    }

    return {
      step: normalizeOnboardingStep(safeConfig.initialStep),
      minDate: safeConfig.minDate || "",
      maxDate: safeConfig.maxDate || "",
      selectedDate: safeConfig.lastPeriodStart || "",
      cycleLength: Number(safeConfig.cycleLength || 28),
      periodLength: Number(safeConfig.periodLength || 5),
      autoPeriodFill: safeConfig.autoPeriodFill !== false,
      dayOptions: [],
      periodExceedsCycleMessage: periodExceedsCycleMessage,
      clearStepStatuses: function () {
        var statusIDs = ["onboarding-step1-status", "onboarding-step2-status", "onboarding-step3-status"];
        for (var index = 0; index < statusIDs.length; index++) {
          var node = document.getElementById(statusIDs[index]);
          if (node) {
            node.textContent = "";
          }
        }
      },
      clearStepStatus: function (statusID) {
        var node = document.getElementById(statusID);
        if (!node) {
          return;
        }
        node.textContent = "";
      },
      syncStepInURL: function () {
        if (!window.history || typeof window.history.replaceState !== "function") {
          return;
        }
        try {
          var currentURL = new URL(window.location.href);
          if (this.step > 0) {
            currentURL.searchParams.set("step", String(this.step));
          } else {
            currentURL.searchParams.delete("step");
          }
          var nextPath = currentURL.pathname + currentURL.search + currentURL.hash;
          var currentPath = window.location.pathname + window.location.search + window.location.hash;
          if (nextPath !== currentPath) {
            window.history.replaceState({}, "", nextPath);
          }
        } catch {
          // Ignore malformed URLs and keep current location unchanged.
        }
      },
      renderStepStatus: function (statusID, kind, message) {
        var node = document.getElementById(statusID);
        if (!node) {
          return;
        }
        node.textContent = "";
        if (!message) {
          return;
        }

        var status = document.createElement("div");
        status.className = kind;
        status.textContent = String(message);
        node.appendChild(status);
      },
      normalizeStepTwoValues: function () {
        var cycle = Number(this.cycleLength);
        if (!Number.isFinite(cycle)) {
          cycle = 28;
        }
        cycle = Math.max(15, Math.min(90, Math.round(cycle)));
        this.cycleLength = cycle;

        var period = Number(this.periodLength);
        if (!Number.isFinite(period)) {
          period = 5;
        }
        period = Math.max(1, Math.min(14, Math.round(period)));
        this.periodLength = period;
      },
      onCycleLengthChanged: function () {
        this.normalizeStepTwoValues();
        this.clearStepStatus("onboarding-step2-status");
      },
      onPeriodLengthChanged: function () {
        this.normalizeStepTwoValues();
        this.clearStepStatus("onboarding-step2-status");
      },
      validateStepTwoBeforeSubmit: function (event) {
        this.normalizeStepTwoValues();
        if ((this.cycleLength - this.periodLength) < 8) {
          if (event && typeof event.preventDefault === "function") {
            event.preventDefault();
          }
          this.renderStepStatus("onboarding-step2-status", "status-error", this.periodExceedsCycleMessage);
          return false;
        }
        this.clearStepStatus("onboarding-step2-status");
        return true;
      },
      normalizeStartDateWithinBounds: function () {
        var selected = parseDateValue(this.selectedDate);
        if (!selected) {
          this.selectedDate = "";
          return;
        }

        var min = parseDateValue(this.minDate);
        var max = parseDateValue(this.maxDate);

        if (min && selected < min) {
          this.selectedDate = formatDateValue(min);
          return;
        }
        if (max && selected > max) {
          this.selectedDate = formatDateValue(max);
          return;
        }

        this.selectedDate = formatDateValue(selected);
      },
      init: function () {
        this.step = normalizeOnboardingStep(this.step);
        this.syncStepInURL();
        this.dayOptions = buildDayOptions(this.minDate, this.maxDate, lang);
        this.normalizeStartDateWithinBounds();
        this.onStartDateChanged();
      },
      goToStep: function (value) {
        var nextStep = normalizeOnboardingStep(value);
        this.clearStepStatuses();
        this.step = nextStep;
        this.syncStepInURL();
      },
      begin: function () {
        this.goToStep(1);
      },
      onStepOneSaved: function (event) {
        this.advanceAfterSuccessfulRequest(event, 2);
      },
      onStepTwoSaved: function (event) {
        this.advanceAfterSuccessfulRequest(event, 3);
      },
      advanceAfterSuccessfulRequest: function (event, targetStep) {
        if (!event || !event.detail || !event.detail.successful) {
          return;
        }
        this.goToStep(targetStep);
      },
      setStartDate: function (value) {
        this.selectedDate = value || "";
        this.onStartDateChanged();
        this.clearStepStatus("onboarding-step1-status");
      },
      onStartDateChanged: function () {
        this.normalizeStartDateWithinBounds();
        this.clearStepStatus("onboarding-step1-status");
      }
    };
  };

  window.recoveryCodeTools = function () {
    return {
      copied: false,
      copyFailed: false,
      downloaded: false,
      downloadFailed: false,
      recoveryMessage: function (key, fallback) {
        var root = this.$root;
        if (root && root.dataset && root.dataset[key]) {
          return String(root.dataset[key] || "");
        }
        return String(fallback || "");
      },
      notify: function (key, fallback, kind) {
        var message = this.recoveryMessage(key, fallback);
        if (!message || typeof window.showToast !== "function") {
          return;
        }
        window.showToast(message, kind);
      },
      copyCode: function () {
        var code = getRecoveryCodeText(this.$refs);
        if (!code) {
          return;
        }

        var self = this;
        writeTextToClipboard(code).then(function () {
          self.copied = true;
          self.copyFailed = false;
          self.downloaded = false;
          self.downloadFailed = false;
          self.notify("copySuccessMessage", "Recovery code copied.", "ok");
          setTimedFlag(self, "copied", STATUS_CLEAR_MS);
        }).catch(function () {
          self.copied = false;
          self.copyFailed = true;
          self.downloaded = false;
          self.downloadFailed = false;
          self.notify("copyFailedMessage", "Failed to copy recovery code.", "error");
          setTimedFlag(self, "copyFailed", STATUS_CLEAR_MS);
        });
      },
      downloadCode: function () {
        var code = getRecoveryCodeText(this.$refs);
        if (!code) {
          return;
        }

        var self = this;
        this.copied = false;
        this.copyFailed = false;
        this.downloaded = false;
        this.downloadFailed = false;

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

          self.notify("downloadSuccessMessage", "Recovery code downloaded.", "ok");
          setTimedFlag(self, "downloaded", STATUS_CLEAR_MS);
        } catch {
          self.notify("downloadFailedMessage", "Failed to download recovery code.", "error");
          setTimedFlag(self, "downloadFailed", STATUS_CLEAR_MS);
        }
      }
    };
  };

  onDocumentReady(function () {
    initAuthPanelTransitions();
    initLanguageSwitcher();
    initClientTimezone();
    initPasswordToggles();
    initLoginValidation();
    initLoginPasswordPersistence();
    initConfirmModal();
    initToastAPI();
    initHTMXHooks();
  });
})();
