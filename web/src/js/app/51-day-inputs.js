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

