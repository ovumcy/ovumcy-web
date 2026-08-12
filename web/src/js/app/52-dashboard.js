  function syncDashboardPreview(root) {
    var periodToggle = root.querySelector("[data-period-toggle]");
    var notesField = root.querySelector("[data-dashboard-notes]");
    var preview = root.querySelector("[data-dashboard-preview]");
    var isPeriod = !!(periodToggle && periodToggle.checked);
    var notes = notesField ? String(notesField.value || "") : "";
    var trimmedNotes = notes.trim();
    var symptoms = collectCheckedSymptomLabels(root);
    var hasSymptoms = symptoms.length > 0;
    var hasNotes = trimmedNotes.length > 0;
    var showPreview = isPeriod || hasSymptoms || hasNotes;
    var symptomList = root.querySelector("[data-dashboard-symptom-list]");
    var symptomEmpty = root.querySelector("[data-dashboard-symptom-empty]");
    var notesValue = root.querySelector("[data-dashboard-notes-value]");
    var notesEmpty = root.querySelector("[data-dashboard-notes-empty]");

    syncPeriodFieldsets(root, isPeriod);
    syncPeriodToggleLabels(root, isPeriod);

    if (!preview) {
      return;
    }

    setNodeHidden(preview, !showPreview);
    setNodeHidden(root.querySelector("[data-dashboard-preview-heading='period']"), !isPeriod);
    setNodeHidden(root.querySelector("[data-dashboard-preview-heading='other']"), isPeriod);
    setNodeHidden(root.querySelector("[data-dashboard-period-summary]"), !isPeriod);
    setNodeHidden(root.querySelector("[data-dashboard-other-summary]"), isPeriod);

    if (symptomList) {
      symptomList.textContent = "";
      for (var index = 0; index < symptoms.length; index++) {
        var item = document.createElement("li");
        item.textContent = symptoms[index];
        symptomList.appendChild(item);
      }
      setNodeHidden(symptomList, !hasSymptoms);
    }

    setNodeHidden(symptomEmpty, hasSymptoms);
    if (notesValue) {
      notesValue.textContent = notes;
      setNodeHidden(notesValue, !hasNotes);
    }
    setNodeHidden(notesEmpty, hasNotes);
  }

  function syncPeriodToggleLabels(root, isPeriod) {
    if (!root || !root.querySelectorAll) {
      return;
    }

    var labels = root.querySelectorAll("[data-period-toggle-label]");
    for (var index = 0; index < labels.length; index++) {
      var label = labels[index];
      var onText = String(label.getAttribute("data-period-label-on") || "");
      var offText = String(label.getAttribute("data-period-label-off") || "");
      var text = isPeriod ? onText : offText;
      // The glyph is decorative and carries aria-hidden, so it is moved rather
      // than reprinted: rewriting textContent with a literal prefix would drop
      // the wrapper and read the emoji out to assistive technology again.
      var glyph = label.querySelector("[data-period-toggle-glyph]");
      label.textContent = "";
      if (glyph) {
        label.appendChild(glyph);
        label.appendChild(document.createTextNode(" " + text));
      } else {
        label.textContent = text;
      }
    }
  }

  function syncNoteDisclosure(root) {
    if (!root || !root.querySelectorAll) {
      return;
    }

    var disclosures = root.querySelectorAll("[data-note-disclosure]");
    for (var index = 0; index < disclosures.length; index++) {
      var disclosure = disclosures[index];
      var label = disclosure.querySelector("[data-note-disclosure-label]");
      var summary = disclosure.querySelector("summary");
      var notesField = disclosure.querySelector("[data-dashboard-notes]");
      var openText = String(disclosure.getAttribute("data-note-open-text") || "");
      var emptyText = String(disclosure.getAttribute("data-note-empty-text") || "");
      var filledText = String(disclosure.getAttribute("data-note-filled-text") || "");
      var hasNotes = !!(notesField && String(notesField.value || "").trim());
      var isOpen = disclosure.hasAttribute("open");
      if (summary) {
        summary.setAttribute("aria-expanded", isOpen ? "true" : "false");
      }
      if (!label) {
        continue;
      }
      label.textContent = isOpen
        ? openText
        : (hasNotes ? filledText : emptyText);
    }
  }

  function bindNoteDisclosures(root) {
    if (!root || !root.querySelectorAll) {
      return;
    }

    var disclosures = root.querySelectorAll("[data-note-disclosure]");
    for (var index = 0; index < disclosures.length; index++) {
      var disclosure = disclosures[index];
      var summary = disclosure.querySelector("summary");
      if (disclosure.dataset.noteDisclosureBound === "1") {
        continue;
      }
      disclosure.dataset.noteDisclosureBound = "1";
      if (summary) {
        (function (currentDisclosure) {
          summary.addEventListener("click", function (event) {
            event.preventDefault();
            currentDisclosure.open = !currentDisclosure.open;
            syncNoteDisclosure(root);
          });
        })(disclosure);
      }
      disclosure.addEventListener("toggle", function () {
        syncNoteDisclosure(root);
      });
    }
  }

  function safeLocalStorageGet(key) {
    try {
      return window.localStorage.getItem(key);
    } catch {
      return "";
    }
  }

  function safeLocalStorageSet(key, value) {
    try {
      window.localStorage.setItem(key, value);
    } catch {
      // Ignore privacy mode and quota failures.
    }
  }

  function revealOnceTips(root) {
    if (!root || !root.querySelectorAll) {
      return;
    }

    var tips = root.querySelectorAll("[data-once-tip]");
    for (var index = 0; index < tips.length; index++) {
      var tip = tips[index];
      var key = String(tip.getAttribute("data-once-tip") || "").trim();
      if (!key) {
        continue;
      }

      if (safeLocalStorageGet("ovumcy_once_tip:" + key) === "1") {
        setNodeHidden(tip, true);
        continue;
      }

      setNodeHidden(tip, false);
      safeLocalStorageSet("ovumcy_once_tip:" + key, "1");
    }
  }

  function autosizeNoteField(field) {
    if (!field || !field.style) {
      return;
    }
    field.style.height = "auto";
    field.style.height = Math.min(field.scrollHeight, 320) + "px";
  }

  function bindAutosizeNoteFields(root) {
    var scope = root && root.querySelectorAll ? root : document;
    var fields = scope.querySelectorAll(".dashboard-notes-field");
    for (var index = 0; index < fields.length; index++) {
      var field = fields[index];
      if (field.dataset.autosizeBound !== "1") {
        field.dataset.autosizeBound = "1";
        field.addEventListener("input", function () {
          autosizeNoteField(this);
        });
      }
      autosizeNoteField(field);
    }
  }

  function syncDashboardNotesCounter(group) {
    if (!group || !group.querySelector) {
      return;
    }

    var input = group.querySelector("[data-dashboard-notes]");
    var counter = group.querySelector("[data-dashboard-notes-count]");
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

  function bindDashboardNotesCounters(root) {
    var scope = root && root.querySelectorAll ? root : document;
    var counters = scope.querySelectorAll("[data-dashboard-notes-count]");

    for (var index = 0; index < counters.length; index++) {
      var counter = counters[index];
      var group = typeof counter.closest === "function" ? counter.closest("[data-dashboard-notes-field-group]") : null;
      if (!group) {
        continue;
      }

      var input = group.querySelector("[data-dashboard-notes]");
      if (!input) {
        continue;
      }

      if (input.dataset.dashboardNotesCounterBound !== "1") {
        input.dataset.dashboardNotesCounterBound = "1";
        input.addEventListener("input", function () {
          var ownerGroup = typeof this.closest === "function" ? this.closest("[data-dashboard-notes-field-group]") : null;
          syncDashboardNotesCounter(ownerGroup);
        });
      }

      syncDashboardNotesCounter(group);
    }
  }

  function periodTipPending() {
    return !!document.body && document.body.getAttribute("data-period-tip-pending") === "true";
  }

  function setPeriodTipAcknowledged(scope) {
    if (!scope || !scope.querySelectorAll) {
      return;
    }

    var inputs = scope.querySelectorAll("[data-period-tip-ack]");
    for (var index = 0; index < inputs.length; index++) {
      inputs[index].value = "true";
    }
    if (document.body) {
      document.body.setAttribute("data-period-tip-pending", "false");
    }
  }

  function revealPeriodTip(scope) {
    var message = document.body ? String(document.body.getAttribute("data-period-tip-message") || "").trim() : "";
    var copy = scope && scope.querySelector ? scope.querySelector("[data-period-tip-copy]") : null;

    if (copy) {
      setNodeHidden(copy, false);
    }
    if (!copy && message && typeof window.showToast === "function") {
      window.showToast(message, "ok");
    }
  }

  function maybeAcknowledgePeriodTip(scope) {
    if (!periodTipPending()) {
      return;
    }
    setPeriodTipAcknowledged(scope);
    revealPeriodTip(scope);
  }

  window.__ovumcyMaybeAcknowledgePeriodTip = maybeAcknowledgePeriodTip;

  function showQuickFocus(section) {
    if (!section || !section.classList) {
      return;
    }

    section.classList.add("dashboard-section-quick-focus");
    if (section.__ovumcyQuickFocusTimer) {
      window.clearTimeout(section.__ovumcyQuickFocusTimer);
    }
    section.__ovumcyQuickFocusTimer = window.setTimeout(function () {
      section.classList.remove("dashboard-section-quick-focus");
      section.__ovumcyQuickFocusTimer = 0;
    }, 1800);
  }

  // A link into a collapsed disclosure lands on nothing: a target inside a
  // closed <details> is not rendered, so the browser has nothing to scroll to
  // and the jump silently does nothing. The journal's late-cycle actions point
  // straight at fields that now live behind "More", so every same-page jump
  // opens the disclosures above its target first. No inline handler and no
  // markup of its own — the anchors stay plain links, and a browser without
  // this script still submits and saves everything.
  function openDisclosuresAbove(target) {
    var node = target;
    while (node && node !== document.body) {
      if (node.tagName === "DETAILS" && !node.open) {
        node.open = true;
      }
      node = node.parentNode;
    }
  }

  function revealHashTarget(hash) {
    var id = String(hash || "").replace(/^#/, "");
    var target = id ? document.getElementById(id) : null;
    if (!target) {
      return null;
    }
    openDisclosuresAbove(target);
    return target;
  }

  function scrollToHashTarget() {
    var target = revealHashTarget(window.location.hash);
    var reduceMotion = window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    if (!target || typeof target.scrollIntoView !== "function") {
      return;
    }
    target.scrollIntoView(reduceMotion ? { block: "start" } : { block: "start", behavior: "smooth" });
  }

  function bindHashDisclosureReveals() {
    if (!document.body || document.body.dataset.hashDisclosureBound === "1") {
      return;
    }
    document.body.dataset.hashDisclosureBound = "1";

    // Opened synchronously on the click, before the browser acts on the link:
    // by the time it looks for the target, the target is rendered and the
    // default jump scrolls to it.
    document.addEventListener("click", function (event) {
      var link = closestFromEvent(event, "a[href^='#']");
      if (!link) {
        return;
      }
      revealHashTarget(link.getAttribute("href"));
    });

    // Arriving with the anchor already in the URL — a shared link, a reload,
    // or a jump the browser could not make — needs the scroll as well.
    window.addEventListener("hashchange", function () {
      scrollToHashTarget();
    });
    scrollToHashTarget();
  }

  function focusSectionControl(section, selector) {
    if (!section || !section.querySelector) {
      return;
    }

    var target = section.querySelector(selector);
    if (!target) {
      return;
    }

    if (target.closest && target.closest("details")) {
      target.closest("details").open = true;
    }
    if (typeof target.focus === "function") {
      target.focus();
    }
    if (typeof section.scrollIntoView === "function") {
      section.scrollIntoView({ block: "center", behavior: "smooth" });
    }
    showQuickFocus(section);
  }

