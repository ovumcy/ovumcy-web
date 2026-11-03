  function clearDataStatusTarget(form) {
    if (!form || !form.getAttribute) {
      return null;
    }

    var selector = String(form.getAttribute("data-clear-data-status-target") || "").trim();
    return selector ? document.querySelector(selector) : null;
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
      // The delete form's method override would turn the pre-check POST into
      // a DELETE of the account route it is meant to precede.
      if (key === "_method") {
        return;
      }
      if (typeof value === "string") {
        params.append(key, value);
      }
    });

    return params.toString();
  }

  // Asks the password pre-check before an irreversible confirm opens, so a
  // wrong password is reported inline and the modal never asks about an action
  // that would be refused. Resolves true only on a 2xx answer.
  function validateSettingsPassword(form, validateAction, statusTarget, messages) {
    clearFormStatus(statusTarget);

    return fetch(validateAction, {
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
            var message = messages.requestFailed;
            // The re-auth budget answers 429 whatever the body says; the
            // status, not the parsed envelope, is what decides it.
            if (response.status === 429) {
              message = messages.tooManyRequests;
            } else if (errorCode === "invalid password") {
              message = messages.invalidPassword;
            }
            if (statusTarget) {
              renderErrorStatus(statusTarget, message);
            }
            return false;
          });
      })
      .catch(function () {
        if (statusTarget) {
          renderErrorStatus(statusTarget, messages.requestFailed);
        }
        return false;
      });
  }

  function passwordPrecheckMessages(form, invalidAttr, failedAttr, tooManyAttr) {
    var requestFailed = String(form.getAttribute(failedAttr) || "Request failed. Please try again.");
    return {
      invalidPassword: String(form.getAttribute(invalidAttr) || "Invalid password."),
      requestFailed: requestFailed,
      tooManyRequests: String(form.getAttribute(tooManyAttr) || requestFailed)
    };
  }

  function passwordPrecheckStatusTarget(form) {
    var selector = String(form.getAttribute("data-password-precheck-status-target") || "").trim();
    return selector ? document.querySelector(selector) : null;
  }

  function initClearDataPasswordConfirmation() {
    document.addEventListener("input", function (event) {
      var field = event.target;
      if (!field || !field.matches || !field.form) {
        return;
      }

      var form = field.form;
      if (field.matches("#settings-clear-data-password") && form.matches("form[data-clear-data-verify-form]")) {
        clearFormStatus(clearDataStatusTarget(form));
        return;
      }
      if (field.matches("input[type=password]") && form.matches("form[data-password-precheck-action]")) {
        clearFormStatus(passwordPrecheckStatusTarget(form));
      }
    });

    document.addEventListener("submit", function (event) {
      var form = event.target;
      var validateAction;

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
      validateSettingsPassword(form, validateAction, clearDataStatusTarget(form), passwordPrecheckMessages(form, "data-clear-data-invalid-password", "data-clear-data-request-failed", "data-clear-data-too-many"))
        .then(function (validated) {
          if (!validated) {
            return;
          }

          return openClearDataConfirm(
            String(form.getAttribute("data-clear-data-confirm-message") || ""),
            String(form.getAttribute("data-clear-data-confirm-accept") || "")
          ).then(function (confirmed) {
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

    // An hx-confirm form that names a password pre-check (delete account)
    // validates the password first; the generic htmx:confirm handler in
    // 22-confirm-modal.js leaves such a form to this one.
    document.body.addEventListener("htmx:confirm", function (event) {
      var source = event && event.detail ? (event.detail.elt || event.target) : null;
      var validateAction;

      if (!source || !source.matches || !source.matches("form[data-password-precheck-action]") || !event.detail.question) {
        return;
      }

      validateAction = String(source.getAttribute("data-password-precheck-action") || "").trim();
      event.preventDefault();
      // One pre-check and one confirm at a time: a second click while either
      // is open would otherwise queue a second DELETE behind the first.
      if (source.dataset.passwordPrecheckPending === "1") {
        return;
      }
      source.dataset.passwordPrecheckPending = "1";
      validateSettingsPassword(source, validateAction, passwordPrecheckStatusTarget(source), passwordPrecheckMessages(source, "data-password-precheck-invalid-password", "data-password-precheck-request-failed", "data-password-precheck-too-many"))
        .then(function (validated) {
          if (!validated) {
            return false;
          }
          return openClearDataConfirm(event.detail.question, source.getAttribute("data-confirm-accept") || "");
        })
        .then(function (confirmed) {
          source.dataset.passwordPrecheckPending = "";
          if (confirmed) {
            event.detail.issueRequest(true);
          }
        });
    });
  }
