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
