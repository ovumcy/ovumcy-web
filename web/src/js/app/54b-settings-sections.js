  // Settings sections collapse on a phone, and only on a phone.
  //
  // Measured at 390px: the page ran 8856 px before the copy pass and 8499 px
  // after it. The length is structural — ten cards open at once — so trimming
  // inside them cannot reach it. Collapsed, the same page is its ten headings.
  //
  // The disclosure is a native <details> rendered OPEN by the server, and this
  // module only closes them, and only while the phone query matches. Three
  // things follow from that shape, and they are the reason for it:
  //   * Without JS, and on every wider screen, the page is what it always was.
  //     Collapsing is an enhancement; it is never a gate in front of a setting.
  //   * The section index above the cards keeps working for free. A link into a
  //     closed card is opened by openDisclosuresAbove, which already walks the
  //     ancestors of a hash target for exactly this — no second mechanism, and
  //     no chance of the index quietly ceasing to navigate.
  //   * A section holding unsaved edits is never closed by this module. Which
  //     forms are dirty is read from dirtySettingsDraftForms rather than
  //     tracked again here.

  var SETTINGS_SECTION_MEDIA = "(max-width: 640px)";

  function settingsSectionDisclosures() {
    var root = document.querySelector("[data-settings-sections]");
    if (!root) {
      return [];
    }
    var found = [];
    // Direct children only: the account card holds a nested section of its own,
    // and folding that one would hide a control inside a control.
    for (var index = 0; index < root.children.length; index++) {
      var node = root.children[index];
      if (node.tagName === "DETAILS" && node.hasAttribute("data-settings-section")) {
        found.push(node);
      }
    }
    return found;
  }

  function settingsSectionHoldsDirtyForm(section) {
    var dirty = typeof dirtySettingsDraftForms === "function" ? dirtySettingsDraftForms() : [];
    for (var index = 0; index < dirty.length; index++) {
      if (section.contains(dirty[index])) {
        return true;
      }
    }
    return false;
  }

  function settingsSectionHoldsHashTarget(section) {
    var id = String(window.location.hash || "").replace(/^#/, "");
    if (!id) {
      return false;
    }
    var target = document.getElementById(id);
    return !!target && (target === section || section.contains(target));
  }

  function applySettingsSectionMode(collapse) {
    var sections = settingsSectionDisclosures();
    for (var index = 0; index < sections.length; index++) {
      var section = sections[index];
      if (!collapse) {
        section.open = true;
        continue;
      }
      if (settingsSectionHoldsDirtyForm(section) || settingsSectionHoldsHashTarget(section)) {
        section.open = true;
        continue;
      }
      section.open = false;
    }
  }

  // The summary is pointer-inert above 640px, but it stays focusable, so Enter
  // on it would still close a section on a screen meant to have none closed.
  // `toggle` does NOT bubble, so this cannot be delegated to the root and is
  // attached per node — idempotently, because the symptoms card replaces
  // itself through htmx and arrives as a different element each time.
  function bindSettingsSectionToggleGuard(section, query) {
    if (section.dataset.settingsSectionBound === "true") {
      return;
    }
    section.dataset.settingsSectionBound = "true";
    section.addEventListener("toggle", function (event) {
      if (!query.matches && !event.target.open) {
        event.target.open = true;
      }
    });
  }

  function bindSettingsSectionDisclosures() {
    var sections = settingsSectionDisclosures();
    if (!sections.length || !window.matchMedia) {
      return;
    }

    var query = window.matchMedia(SETTINGS_SECTION_MEDIA);
    for (var index = 0; index < sections.length; index++) {
      bindSettingsSectionToggleGuard(sections[index], query);
    }

    // Re-entered after every htmx swap (initCSPFriendlyComponents is the swap
    // hook), and collapsing is a LOAD-time decision: re-applying it here would
    // shut every section the owner had opened, each time a symptom is added.
    if (document.body.dataset.settingsSectionsBound === "true") {
      return;
    }
    document.body.dataset.settingsSectionsBound = "true";

    applySettingsSectionMode(query.matches);

    var onChange = function (event) {
      applySettingsSectionMode(event.matches);
    };
    if (typeof query.addEventListener === "function") {
      query.addEventListener("change", onChange);
    } else if (typeof query.addListener === "function") {
      query.addListener(onChange);
    }
  }
