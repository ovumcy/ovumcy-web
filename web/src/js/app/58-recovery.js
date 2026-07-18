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
    var statusTarget = form.querySelector("[data-recovery-code-status]");
    var enabled = !!(checkbox && checkbox.checked);
    if (checkbox) {
      if (enabled && typeof checkbox.setCustomValidity === "function") {
        checkbox.setCustomValidity("");
      }
      if (enabled) {
        checkbox.removeAttribute("aria-invalid");
      }
    }
    if (enabled && statusTarget && typeof clearFormStatus === "function") {
      clearFormStatus(statusTarget);
    }
    if (!submit) {
      return;
    }

    submit.setAttribute("aria-disabled", enabled ? "false" : "true");
    submit.dataset.recoveryCodeReady = enabled ? "true" : "false";
  }

  function recoveryCodeRequiredMessage(form) {
    if (!form || !form.dataset) {
      return "Check this box to continue.";
    }

    return String(form.dataset.recoveryRequiredMessage || "Check this box to continue.");
  }

  function recoveryCodeContinuePath(form) {
    if (!form || typeof form.getAttribute !== "function") {
      return "/dashboard";
    }
    switch (String(form.getAttribute("data-recovery-continue-target") || "").trim()) {
      case "onboarding":
        return "/onboarding";
      case "settings":
        return "/settings";
      default:
        return "/dashboard";
    }
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
          var statusTarget = this.querySelector("[data-recovery-code-status]");
          var requiredMessage = recoveryCodeRequiredMessage(this);
          if (!checkbox) {
            syncRecoveryCodeConfirmForm(this);
            return;
          }
          if (checkbox.checked) {
            event.preventDefault();
            syncRecoveryCodeConfirmForm(this);
            window.location.assign(recoveryCodeContinuePath(this));
            return;
          }

          event.preventDefault();
          if (typeof checkbox.setCustomValidity === "function") {
            checkbox.setCustomValidity(requiredMessage);
          }
          checkbox.setAttribute("aria-invalid", "true");
          if (statusTarget && typeof moveFormStatusTarget === "function") {
            moveFormStatusTarget(statusTarget, checkbox);
          }
          if (statusTarget && typeof renderFormStatusError === "function") {
            renderFormStatusError(statusTarget, requiredMessage);
          }
          if (typeof checkbox.focus === "function") {
            checkbox.focus();
          }
        });
      }
      syncRecoveryCodeConfirmForm(form);
    }
  }

