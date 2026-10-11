(function () {
  "use strict";

  var IMPORT_ENDPOINT = "/api/v1/imports/json";
  // The binding of the account that rendered this page. It is sent on every
  // restore, empty or not: the server refuses a restore whose binding names
  // another account, and this request (JSON, not HTMX) would otherwise read as
  // a client that renders no page and be let through without one.
  var ACCOUNT_BINDING_HEADER = "X-Ovumcy-Day-Form-Account";

  function readTextAttribute(node, name, fallback) {
    return node.getAttribute(name) || fallback;
  }

  function formatTemplate(template, values) {
    var index = 0;
    return String(template || "").replace(/%[sd]/g, function () {
      var value = index < values.length ? values[index] : "";
      index += 1;
      return String(value);
    });
  }

  function setButtonDisabled(button, disabled) {
    if (!button) {
      return;
    }
    button.disabled = disabled;
    button.setAttribute("aria-disabled", disabled ? "true" : "false");
  }

  function csrfToken(section) {
    var fromSection = String(section.getAttribute("data-import-csrf") || "").trim();
    if (fromSection) {
      return fromSection;
    }
    var meta = document.querySelector('meta[name="csrf-token"]');
    return meta ? String(meta.getAttribute("content") || "").trim() : "";
  }

  function readFileAsText(file) {
    return new Promise(function (resolve, reject) {
      var reader = new FileReader();
      reader.onload = function () {
        resolve(String(reader.result || ""));
      };
      reader.onerror = function () {
        reject(new Error("file_read_failed"));
      };
      reader.readAsText(file);
    });
  }

  // The stable key of a refused restore. Every API refusal answers the shared
  // envelope {error, error_detail: {key, ...}}: the structured key is read
  // first and the top-level legacy string second.
  function errorKeyFromPayload(payload) {
    if (!payload || typeof payload !== "object") {
      return "";
    }
    var detail = payload.error_detail;
    if (detail && typeof detail.key === "string" && detail.key) {
      return detail.key;
    }
    return typeof payload.error === "string" ? payload.error : "";
  }

  // Each refusal with copy of its own. request_too_large is the body-size cap
  // answering before the handler runs; it is the same condition to the person
  // as the entry-count cap, so it shares the message.
  function messageForErrorKey(context, key) {
    switch (key) {
      case "invalid import file":
        return context.invalidFileMessage;
      case "import file too large":
      case "request_too_large":
        return context.tooLargeMessage;
      case "settings form account changed":
        return context.accountChangedMessage;
      default:
        return context.failedMessage;
    }
  }

  function createImportHandler(context) {
    return async function handleImport(event) {
      event.preventDefault();

      var fileInput = context.fileInput;
      var file = fileInput && fileInput.files ? fileInput.files[0] : null;
      if (!file) {
        if (typeof window.showToast === "function") {
          window.showToast(context.emptyMessage, "error");
        }
        return;
      }

      var submitButton = context.submitButton;
      if (submitButton) {
        submitButton.classList.add("btn-loading");
        setButtonDisabled(submitButton, true);
      }

      try {
        var fileText = await readFileAsText(file);
        var headers = {
          "Content-Type": "application/json",
          Accept: "application/json",
          "X-CSRF-Token": context.token
        };
        headers[ACCOUNT_BINDING_HEADER] = context.accountBinding;
        var response = await fetch(IMPORT_ENDPOINT, {
          method: "POST",
          credentials: "same-origin",
          headers: headers,
          body: fileText
        });

        var payload = null;
        try {
          payload = await response.json();
        } catch {
          payload = null;
        }

        if (!response.ok) {
          if (typeof window.showToast === "function") {
            window.showToast(messageForErrorKey(context, errorKeyFromPayload(payload)), "error");
          }
          return;
        }

        var added = payload && Number.isFinite(payload.added) ? payload.added : 0;
        var skipped = payload && Number.isFinite(payload.skipped) ? payload.skipped : 0;
        var rejected = payload && Number.isFinite(payload.rejected) ? payload.rejected : 0;

        if (typeof window.showToast === "function") {
          window.showToast(formatTemplate(context.successTemplate, [added, skipped, rejected]), "ok");
        }

        if (context.form && typeof context.form.reset === "function") {
          context.form.reset();
        }
      } catch {
        if (typeof window.showToast === "function") {
          window.showToast(context.failedMessage, "error");
        }
      } finally {
        if (submitButton) {
          submitButton.classList.remove("btn-loading");
          setButtonDisabled(submitButton, false);
        }
      }
    };
  }

  var section = document.querySelector("[data-import-section]");
  if (!section) {
    return;
  }

  var form = section.querySelector("[data-import-form]");
  var fileInput = section.querySelector("[data-import-file]");
  var submitButton = section.querySelector("[data-import-submit]");
  if (!form || !fileInput || !submitButton) {
    return;
  }

  var context = {
    form: form,
    fileInput: fileInput,
    submitButton: submitButton,
    token: csrfToken(section),
    accountBinding: String(section.getAttribute("data-import-account") || "").trim(),
    successTemplate: readTextAttribute(section, "data-import-success-template", "Restored %d days (%d already present, %d ignored)."),
    emptyMessage: readTextAttribute(section, "data-import-empty", "Choose a file to restore."),
    failedMessage: readTextAttribute(section, "data-import-failed", "Restore failed. Please try again."),
    invalidFileMessage: readTextAttribute(section, "data-import-invalid-file", "This file isn't a valid Ovumcy export."),
    tooLargeMessage: readTextAttribute(section, "data-import-too-large", "That file is too large to import."),
    accountChangedMessage: readTextAttribute(
      section,
      "data-import-account-changed",
      "You're now signed in to a different account. This change was not saved. Reload the page."
    )
  };

  form.addEventListener("submit", createImportHandler(context));
})();
