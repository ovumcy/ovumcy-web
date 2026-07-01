(function () {
  "use strict";

  var IMPORT_ENDPOINT = "/api/v1/imports/json";

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

  function messageForErrorKey(context, key) {
    if (key === "invalid import file") {
      return context.invalidFileMessage;
    }
    if (key === "import file too large") {
      return context.tooLargeMessage;
    }
    return context.failedMessage;
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
        var response = await fetch(IMPORT_ENDPOINT, {
          method: "POST",
          credentials: "same-origin",
          headers: {
            "Content-Type": "application/json",
            Accept: "application/json",
            "X-CSRF-Token": context.token
          },
          body: fileText
        });

        var payload = null;
        try {
          payload = await response.json();
        } catch {
          payload = null;
        }

        if (!response.ok) {
          var errorKey = payload && typeof payload.key === "string" ? payload.key : "";
          if (typeof window.showToast === "function") {
            window.showToast(messageForErrorKey(context, errorKey), "error");
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
    successTemplate: readTextAttribute(section, "data-import-success-template", "Restored %d days (%d already present, %d ignored)."),
    emptyMessage: readTextAttribute(section, "data-import-empty", "Choose a file to restore."),
    failedMessage: readTextAttribute(section, "data-import-failed", "Restore failed. Please try again."),
    invalidFileMessage: readTextAttribute(section, "data-import-invalid-file", "This file isn't a valid Ovumcy export."),
    tooLargeMessage: readTextAttribute(section, "data-import-too-large", "That file is too large to import.")
  };

  form.addEventListener("submit", createImportHandler(context));
})();
