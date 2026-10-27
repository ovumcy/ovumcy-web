  // A day opened from the grid opens as the grid opens it, so the edit=1 a
  // dashboard link arrived with is dropped: kept, it would reopen whatever day
  // is current for editing (and bring its panel up) on every reload.
  function calendarDayHref(href, selectedDate) {
    var target = new URL(href, window.location.href);
    if (selectedDate) {
      target.searchParams.set("day", selectedDate);
    } else {
      target.searchParams.delete("day");
    }
    target.searchParams.delete("edit");
    return target.pathname + target.search + target.hash;
  }

  function syncCalendarURL(selectedDate) {
    if (!window.history || typeof window.history.replaceState !== "function") {
      return;
    }

    try {
      window.history.replaceState({}, "", calendarDayHref(window.location.href, selectedDate));
    } catch {
      // Ignore malformed URLs and keep current location unchanged.
    }
  }

  // Prev and Next keep the open day; Today, which names no month, still opens
  // today.
  function syncCalendarMonthNav(selectedDate) {
    var links = document.querySelectorAll("[data-calendar-month-nav] a[href]:not([data-calendar-today])");
    for (var index = 0; index < links.length; index++) {
      try {
        links[index].setAttribute("href", calendarDayHref(links[index].getAttribute("href"), selectedDate));
      } catch {
        // Leave a link with a malformed href as it was rendered.
      }
    }
  }

  function calendarDayButton(root, date) {
    if (!/^\d{4}-\d{2}-\d{2}$/.test(date)) {
      return null;
    }
    var button = root.querySelector('button[data-day="' + date + '"]');
    return button && !button.disabled ? button : null;
  }

  // The month is one Tab stop: the day the arrow keys last reached, else the
  // selected day, else today, else the first open day takes tabindex 0. The
  // arrowed-to day is kept across swaps, which re-run this on every one.
  function calendarTabStop(root, buttons, selectedDate) {
    var stop = calendarDayButton(root, String(root.dataset.calendarFocusDay || "")) ||
      calendarDayButton(root, selectedDate) ||
      root.querySelector('button[data-day][aria-current="date"]:not([disabled])');
    if (stop) {
      return stop;
    }
    for (var index = 0; index < buttons.length; index++) {
      if (!buttons[index].disabled) {
        return buttons[index];
      }
    }
    return null;
  }

  function syncCalendarSelection(root) {
    var selectedDate = String(root.getAttribute("data-selected-date") || "");
    var buttons = root.querySelectorAll("button[data-day]");
    var tabStop = calendarTabStop(root, buttons, selectedDate);

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
        root.dataset.calendarFocusDay = buttons[index].getAttribute("data-day");
        syncCalendarSelection(root);
        buttons[index].focus();
        return;
      }
    }
  }

  // The sticky header covers the top of the viewport, so a panel scrolled to
  // the very top would sit under it.
  function calendarHeaderBottom() {
    var header = document.querySelector(".paper-header");
    return header ? Math.max(0, header.getBoundingClientRect().bottom) : 0;
  }

  // Below the month (the stacked phone and tablet layout) the panel is always
  // brought up: there its heading can sit on the last line of the screen, in
  // view and yet behind the tab bar, while everything it opened stays below the
  // fold. Beside the month it moves only when its heading is out of view.
  function revealDayPanel(panel, heading) {
    var grid = document.getElementById("calendar-grid-panel");
    var panelTop = panel.getBoundingClientRect().top;
    var stacked = Boolean(grid) && panelTop >= grid.getBoundingClientRect().bottom;
    var headerBottom = calendarHeaderBottom();
    var headingRect = heading.getBoundingClientRect();
    var viewportHeight = window.innerHeight || document.documentElement.clientHeight;
    var hidden = headingRect.top < headerBottom || headingRect.bottom > viewportHeight;
    if (!stacked && !hidden) {
      return;
    }
    window.scrollTo({
      top: window.scrollY + panelTop - headerBottom - 12,
      behavior: scrollIntoViewOptions("start").behavior || "auto",
    });
  }

  // A day opened from the grid, or re-opened by Edit, Cancel or Delete,
  // replaces the control that had focus. Focus goes to the panel heading so it
  // is announced and never falls to the body. The panel a ?day= URL loads on
  // its own leaves focus where the page put it; one loaded for editing is
  // still brought into view, since the owner came to type into it.
  function focusOpenedDayPanel(event) {
    var detail = event.detail || {};
    var target = detail.target || event.target;
    if (!target || target.id !== "day-editor") {
      return;
    }
    var heading = target.querySelector("[data-day-editor-heading]");
    if (!heading) {
      return;
    }
    var source = (detail.requestConfig && detail.requestConfig.elt) || detail.elt;
    if (source && source.hasAttribute && source.hasAttribute("data-day-editor-autoload")) {
      if (source.getAttribute("data-day-editor-autoload") === "edit") {
        revealDayPanel(target, heading);
      }
      return;
    }
    revealDayPanel(target, heading);
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
          delete this.dataset.calendarFocusDay;
          syncCalendarSelection(this);
          syncCalendarURL(selectedDate);
          syncCalendarMonthNav(selectedDate);
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
            var selected = calendarDayButton(this, String(this.getAttribute("data-selected-date") || ""));
            if (selected) {
              event.preventDefault();
              delete this.dataset.calendarFocusDay;
              syncCalendarSelection(this);
              selected.focus();
            }
          }
        });

        root.addEventListener("htmx:afterSwap", focusOpenedDayPanel);
      }

      syncCalendarSelection(root);
    }
  }

