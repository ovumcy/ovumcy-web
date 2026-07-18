  function syncCalendarURL(selectedDate) {
    if (!window.history || typeof window.history.replaceState !== "function") {
      return;
    }

    try {
      var currentURL = new URL(window.location.href);
      if (selectedDate) {
        currentURL.searchParams.set("day", selectedDate);
      } else {
        currentURL.searchParams.delete("day");
      }
      var nextPath = currentURL.pathname + currentURL.search + currentURL.hash;
      window.history.replaceState({}, "", nextPath);
    } catch {
      // Ignore malformed URLs and keep current location unchanged.
    }
  }

  function syncCalendarSelection(root) {
    var selectedDate = String(root.getAttribute("data-selected-date") || "");
    var buttons = root.querySelectorAll("button[data-day]");

    for (var index = 0; index < buttons.length; index++) {
      buttons[index].classList.toggle("selected", buttons[index].getAttribute("data-day") === selectedDate);
    }
  }

  function bindCalendarViews() {
    var roots = document.querySelectorAll("[data-calendar-view]");
    for (var index = 0; index < roots.length; index++) {
      var root = roots[index];
      if (root.dataset.calendarViewBound !== "1") {
        root.dataset.calendarViewBound = "1";

        root.addEventListener("click", function (event) {
          var button = closestFromEvent(event, "button[data-day]");
          if (!button || !this.contains(button)) {
            return;
          }

          var selectedDate = String(button.getAttribute("data-day") || "");
          this.setAttribute("data-selected-date", selectedDate);
          syncCalendarSelection(this);
          syncCalendarURL(selectedDate);
        });
      }

      syncCalendarSelection(root);
    }
  }

