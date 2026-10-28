  // The server writes data-period-off-confirm onto a day form's period toggle
  // only when un-ticking it reaches past that day (services.PeriodOffEffect).
  // The un-ticking click is held back before the box changes, so a declined
  // question leaves no input or change event behind and the day forms mark
  // nothing for saving; an accepted one un-ticks the box with both events.
  function confirmPeriodOff(toggle) {
    var question = toggle.getAttribute("data-period-off-confirm") || "";
    if (typeof window.__ovumcyOpenConfirm === "function") {
      return window.__ovumcyOpenConfirm(question, toggle.getAttribute("data-period-off-confirm-accept") || "");
    }
    return Promise.resolve(window.confirm(question));
  }

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
      confirmPeriodOff(toggle).then(function (confirmed) {
        if (!confirmed || !toggle.checked || !document.contains(toggle)) {
          return;
        }
        toggle.checked = false;
        toggle.dispatchEvent(new Event("input", { bubbles: true }));
        toggle.dispatchEvent(new Event("change", { bubbles: true }));
      });
    }, true);
  }
