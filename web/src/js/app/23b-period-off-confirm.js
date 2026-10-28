  // The server writes data-period-off-confirm onto a day form's period toggle
  // only when un-ticking it reaches past that day (services.PeriodOffEffect).
  // The un-ticking click is held back before the box changes, so a declined
  // question leaves no input or change event behind and the day forms mark
  // nothing for saving; an accepted one un-ticks the box with both events.
  function bindPeriodOffConfirm() {
    document.addEventListener("click", function (event) {
      var toggle = event.target;
      if (!toggle || !toggle.matches || !toggle.matches("input[data-period-toggle][data-period-off-confirm]")) {
        return;
      }
      // The click has already flipped the box: still ticked means it was ticking.
      if (toggle.checked) {
        return;
      }
      event.preventDefault();
      openCycleStartConfirm(
        toggle.getAttribute("data-period-off-confirm") || "",
        toggle.getAttribute("data-period-off-confirm-accept") || ""
      ).then(function (confirmed) {
        if (!confirmed || !toggle.checked || !document.contains(toggle)) {
          return;
        }
        toggle.checked = false;
        toggle.dispatchEvent(new Event("input", { bubbles: true }));
        toggle.dispatchEvent(new Event("change", { bubbles: true }));
      });
    }, true);
  }

  // A day save answers with the question the saved day's toggle asks from then
  // on (X-Ovumcy-Period-Off-Confirm, empty when it asks nothing): both day forms
  // stay open after a save, so the question they were rendered with goes stale.
  // An answer without the header leaves that question as it is.
  // X-Ovumcy-Period-Off-Confirm-Key, its stable key, follows the question into
  // data-period-off-confirm-key.
  function applyPeriodOffConfirmHeader(form, raw, key) {
    var toggle = form && form.querySelector ? form.querySelector("input[data-period-toggle]") : null;
    var question;
    if (!toggle || raw === null || raw === undefined) {
      return;
    }
    question = window.__ovumcyDecodeResponseNoticeHeader(raw);
    if (question) {
      toggle.setAttribute("data-period-off-confirm", question);
      if (key) {
        toggle.setAttribute("data-period-off-confirm-key", key);
      } else {
        toggle.removeAttribute("data-period-off-confirm-key");
      }
    } else {
      toggle.removeAttribute("data-period-off-confirm");
      toggle.removeAttribute("data-period-off-confirm-key");
    }
  }

  window.__ovumcyApplyPeriodOffConfirmHeader = applyPeriodOffConfirmHeader;
