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

  // The month is one Tab stop: the selected day, else today, else the first
  // open day takes tabindex 0 and the arrow keys reach the rest.
  function calendarTabStop(buttons, selectedDate) {
    var today = null;
    var first = null;
    for (var index = 0; index < buttons.length; index++) {
      var button = buttons[index];
      if (button.disabled) {
        continue;
      }
      if (button.getAttribute("data-day") === selectedDate) {
        return button;
      }
      if (!today && button.getAttribute("aria-current") === "date") {
        today = button;
      }
      if (!first) {
        first = button;
      }
    }
    return today || first;
  }

  function syncCalendarSelection(root) {
    var selectedDate = String(root.getAttribute("data-selected-date") || "");
    var buttons = root.querySelectorAll("button[data-day]");
    var tabStop = calendarTabStop(buttons, selectedDate);

    for (var index = 0; index < buttons.length; index++) {
      var button = buttons[index];
      var selected = button.getAttribute("data-day") === selectedDate;
      button.classList.toggle("selected", selected);
      if (!button.disabled) {
        button.setAttribute("aria-pressed", selected ? "true" : "false");
      }
      button.setAttribute("tabindex", button === tabStop ? "0" : "-1");
    }
  }

  var CALENDAR_ARROW_STEPS = { ArrowLeft: -1, ArrowRight: 1, ArrowUp: -7, ArrowDown: 7 };

  function moveCalendarFocus(root, from, step) {
    var buttons = root.querySelectorAll("button[data-day]");
    var index = Array.prototype.indexOf.call(buttons, from);
    for (index += step; index >= 0 && index < buttons.length; index += step) {
      if (!buttons[index].disabled) {
        for (var other = 0; other < buttons.length; other++) {
          buttons[other].setAttribute("tabindex", other === index ? "0" : "-1");
        }
        buttons[index].focus();
        return;
      }
    }
  }

  function selectedCalendarButton(root) {
    var selectedDate = String(root.getAttribute("data-selected-date") || "");
    var buttons = root.querySelectorAll("button[data-day]");
    for (var index = 0; index < buttons.length; index++) {
      if (buttons[index].getAttribute("data-day") === selectedDate) {
        return buttons[index];
      }
    }
    return null;
  }

  function isInViewport(element) {
    if (typeof element.getBoundingClientRect !== "function") {
      return true;
    }
    var rect = element.getBoundingClientRect();
    var height = window.innerHeight || document.documentElement.clientHeight;
    return rect.top >= 0 && rect.bottom <= height;
  }

  // A day opened from the grid, or re-opened by Edit, Cancel or Delete,
  // replaces the control that had focus. Focus goes to the panel heading so it
  // is announced and never falls to the body; the panel is scrolled in only
  // when it is off screen, which on a phone is below the month. The panel a
  // ?day= URL loads on its own leaves focus where the page put it.
  function focusOpenedDayPanel(event) {
    var detail = event.detail || {};
    var target = detail.target || event.target;
    if (!target || target.id !== "day-editor") {
      return;
    }
    var source = (detail.requestConfig && detail.requestConfig.elt) || detail.elt;
    if (source && source.hasAttribute && source.hasAttribute("data-day-editor-autoload")) {
      return;
    }
    var heading = target.querySelector("[data-day-editor-heading]");
    if (!heading) {
      return;
    }
    if (!isInViewport(heading) && typeof target.scrollIntoView === "function") {
      target.scrollIntoView(scrollIntoViewOptions("start"));
    }
    heading.focus({ preventScroll: true });
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

        root.addEventListener("keydown", function (event) {
          if (event.defaultPrevented || event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) {
            return;
          }
          var cell = closestFromEvent(event, "button[data-day]");
          if (cell && this.contains(cell) && CALENDAR_ARROW_STEPS[event.key]) {
            event.preventDefault();
            moveCalendarFocus(this, cell, CALENDAR_ARROW_STEPS[event.key]);
            return;
          }
          if (event.key === "Escape" && closestFromEvent(event, "#day-editor")) {
            var selected = selectedCalendarButton(this);
            if (selected && !selected.disabled) {
              event.preventDefault();
              selected.focus();
            }
          }
        });

        root.addEventListener("htmx:afterSwap", focusOpenedDayPanel);
      }

      syncCalendarSelection(root);
    }
  }

