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
      var tooManyRequestsMessage;
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
      tooManyRequestsMessage = String(form.getAttribute("data-clear-data-too-many") || requestFailedMessage);
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
              var message = requestFailedMessage;
              // The re-auth budget answers 429 whatever the body says; the
              // status, not the parsed envelope, is what decides it.
              if (response.status === 429) {
                message = tooManyRequestsMessage;
              } else if (errorCode === "invalid password") {
                message = invalidPasswordMessage;
              }
              if (statusTarget) {
                renderErrorStatus(statusTarget, message);
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
