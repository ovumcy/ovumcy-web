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

  // An anchor jump into a card scrolls its top edge to the top of the viewport,
  // under the sticky header and, from 640px up, the sticky section index. The
  // cards' scroll-margin-top clears both, but the index wraps to as many rows
  // as the locale and the width need, so its height is measured here and
  // published on the stack as --settings-section-nav-height; the stylesheet's
  // two-row figure is only the default for a page without this script. A page
  // with no index has nothing to clear but the header.
  //
  // The sticky header wraps the same way (text zoom, locale), so its height is
  // measured too and published as --sticky-header-clearance: the header plus
  // the sliver the stylesheet's default carries. Both figures feed the cards'
  // scroll-margin-top and the index's sticky offset, and one observer watches
  // both elements.
  var SETTINGS_STICKY_HEADER_SLIVER_PX = 4;

  function publishSettingsStickyChrome(root, nav, header) {
    if (header && header.offsetHeight > 0) {
      root.style.setProperty(
        "--sticky-header-clearance",
        header.offsetHeight + SETTINGS_STICKY_HEADER_SLIVER_PX + "px"
      );
    }
    // A header that is absent or not laid out (yet) keeps whatever was last
    // published, or the stylesheet default.
    var height = nav ? nav.offsetHeight : 0;
    if (nav && !(height > 0)) {
      // Not laid out (yet): keep whatever was last published, or the default.
      return;
    }
    root.style.setProperty("--settings-section-nav-height", height + "px");
  }

  function bindSettingsSectionNavOffset() {
    var root = document.querySelector("[data-settings-sections]");
    if (!root) {
      return;
    }
    var nav = root.querySelector("[data-settings-section-nav]");
    var header = document.querySelector(".paper-header");
    publishSettingsStickyChrome(root, nav, header);
    // Re-entered after every htmx swap; neither the index nor the header is
    // ever swapped, so one observer per stack node is the whole binding.
    if (root.__ovumcySettingsChromeObserved || (!nav && !header)) {
      return;
    }
    root.__ovumcySettingsChromeObserved = true;
    var republish = function () {
      publishSettingsStickyChrome(root, nav, header);
    };
    if (typeof window.ResizeObserver === "function") {
      var observer = new window.ResizeObserver(republish);
      if (nav) {
        observer.observe(nav);
      }
      if (header) {
        observer.observe(header);
      }
      return;
    }
    window.addEventListener("resize", republish);
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

  // A card that replaces itself (hx-swap="outerHTML" onto its own id) takes the
  // focused control down with it: focus drops to BODY, and a swap that
  // re-sorts the card's rows can leave the row the owner just saved far out of
  // view, with its confirmation. The answer names where the owner should be
  // put back — an element carrying data-focus-after-swap — and this moves
  // focus there and scrolls its row clear of the sticky chrome. The marker is
  // the whole contract, so the egress and account cards can opt in by markup.
  //
  // Only a swap that took the owner's focus is repaired: when focus sat in the
  // swapped region and is gone afterwards, it lands on the marker, else on the
  // card's own summary, so a swap whose target vanished still leaves the
  // owner at the card rather than at the top of the document.
  var FOCUS_AFTER_SWAP_MARKER = "[data-focus-after-swap]";

  function focusAfterSwapRoot(event) {
    var node = event && event.target && event.target.nodeType === 1 ? event.target : null;
    if (node && node.isConnected) {
      return node;
    }
    var swapped = event && event.detail ? event.detail.target : null;
    return swapped && swapped.id ? document.getElementById(swapped.id) : null;
  }

  function scrollIntoViewBelowStickyChrome(node) {
    var anchor = node.closest("[data-focus-scroll-anchor]") || node;
    // The same clearance the cards' scroll-margin-top uses, read from the
    // figures publishSettingsStickyChrome keeps current.
    anchor.style.scrollMarginTop =
      "calc(var(--sticky-header-clearance, 4.5rem) + var(--settings-section-nav-height, 0px) + 0.75rem)";
    if (typeof anchor.scrollIntoView === "function") {
      anchor.scrollIntoView({ block: "nearest" });
    }
  }

  function bindFocusAfterSwap() {
    if (document.body.dataset.focusAfterSwapBound === "true") {
      return;
    }
    document.body.dataset.focusAfterSwapBound = "true";

    var focusWasInside = "";

    document.body.addEventListener("htmx:beforeSwap", function (event) {
      var swapped = event && event.detail ? event.detail.target : null;
      var active = document.activeElement;
      focusWasInside =
        swapped && swapped.id && active && active !== document.body && swapped.contains(active) ? swapped.id : "";
    });

    document.body.addEventListener("htmx:afterSwap", function (event) {
      var inside = focusWasInside;
      focusWasInside = "";
      var root = focusAfterSwapRoot(event);
      if (!root || !root.querySelector) {
        return;
      }
      var marked = root.matches && root.matches(FOCUS_AFTER_SWAP_MARKER) ? root : root.querySelector(FOCUS_AFTER_SWAP_MARKER);
      var focusLost = !document.activeElement || document.activeElement === document.body;
      if (!marked && !(inside && focusLost && root.id === inside)) {
        return;
      }
      var destination = marked || root.querySelector("summary") || root;
      if (!destination.hasAttribute("tabindex") && !destination.matches("a[href], button, input, select, textarea, summary")) {
        destination.setAttribute("tabindex", "-1");
      }
      // preventScroll: the scroll below knows about the sticky chrome, the
      // browser's own focus scroll does not.
      destination.focus({ preventScroll: true });
      scrollIntoViewBelowStickyChrome(destination);
    });
  }
