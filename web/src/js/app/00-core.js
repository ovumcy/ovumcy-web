(function () {
  "use strict";

  var PASSWORD_HIDE_ICON = "\u{1F648}";
  var PASSWORD_SHOW_ICON = "\u{1F441}";
  var TOAST_VISIBLE_MS = 5200;
  var TOAST_EXIT_MS = 220;
  var STATUS_CLEAR_MS = 2000;
  var DOWNLOAD_REVOKE_MS = 500;
  var THEME_STORAGE_KEY = "ovumcy_theme";
  var THEME_LIGHT = "light";
  var THEME_DARK = "dark";
  var THEME_COLOR_LIGHT = "#fff9f0";
  var THEME_COLOR_DARK = "#18141f";
  var TIMEZONE_COOKIE_NAME = "ovumcy_tz";
  var TIMEZONE_HEADER_NAME = "X-Ovumcy-Timezone";
  var TIMEZONE_COOKIE_MAX_AGE_SECONDS = 31536000;

  function getEventTarget(event) {
    return event && event.target ? event.target : null;
  }

  function closestFromEvent(event, selector) {
    var target = getEventTarget(event);
    if (!target || !target.closest) {
      return null;
    }
    return target.closest(selector);
  }

  function isPrimaryClick(event) {
    return !!event && event.button === 0 && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey;
  }

  function onDocumentReady(callback) {
    if (document.readyState === "loading") {
      document.addEventListener("DOMContentLoaded", callback);
      return;
    }
    callback();
  }

  function normalizeTheme(value) {
    var theme = String(value || "").trim().toLowerCase();
    if (theme === THEME_DARK || theme === THEME_LIGHT) {
      return theme;
    }
    return "";
  }

  function supportsMatchMedia() {
    return typeof window.matchMedia === "function";
  }

  function systemPreferredTheme() {
    if (!supportsMatchMedia()) {
      return THEME_LIGHT;
    }
    return window.matchMedia("(prefers-color-scheme: dark)").matches ? THEME_DARK : THEME_LIGHT;
  }

  function resolveTheme(theme) {
    return normalizeTheme(theme) || systemPreferredTheme();
  }

  function readStoredTheme() {
    try {
      return normalizeTheme(window.localStorage.getItem(THEME_STORAGE_KEY));
    } catch {
      return "";
    }
  }

  function writeStoredTheme(theme) {
    var normalized = normalizeTheme(theme);
    if (!normalized) {
      return;
    }

    try {
      window.localStorage.setItem(THEME_STORAGE_KEY, normalized);
    } catch {
      // Ignore storage quota and privacy mode errors.
    }
  }

  function updateThemeColorMeta(theme) {
    var meta = document.getElementById("theme-color-meta");
    if (!meta) {
      return;
    }

    meta.setAttribute("content", theme === THEME_DARK ? THEME_COLOR_DARK : THEME_COLOR_LIGHT);
  }

  function applyTheme(theme) {
    var resolved = resolveTheme(theme);
    document.documentElement.setAttribute("data-theme", resolved);
    updateThemeColorMeta(resolved);
    window.__ovumcyTheme = resolved;
    return resolved;
  }

  function currentTheme() {
    var htmlTheme = normalizeTheme(document.documentElement.getAttribute("data-theme"));
    if (htmlTheme) {
      return htmlTheme;
    }

    var known = normalizeTheme(window.__ovumcyTheme);
    if (known) {
      return known;
    }

    return applyTheme(readStoredTheme());
  }

  function initThemePreference() {
    applyTheme(readStoredTheme());
  }

  function setThemePreference(theme) {
    var normalized = normalizeTheme(theme);
    if (!normalized) {
      return currentTheme();
    }
    writeStoredTheme(normalized);
    return applyTheme(normalized);
  }

  function isSafeClientTimezone(value) {
    if (!value || value.length > 128) {
      return false;
    }
    return /^[A-Za-z0-9_+/-]+$/.test(value);
  }

  function detectClientTimezone() {
    try {
      var formatter = Intl && Intl.DateTimeFormat ? Intl.DateTimeFormat() : null;
      var options = formatter && formatter.resolvedOptions ? formatter.resolvedOptions() : null;
      var timezone = options && options.timeZone ? String(options.timeZone).trim() : "";
      if (!isSafeClientTimezone(timezone)) {
        return "";
      }
      return timezone;
    } catch {
      return "";
    }
  }

  function writeClientCookie(name, value, maxAgeSeconds) {
    if (!name || !value) {
      return;
    }
    var cookie = name + "=" + value +
      "; Path=/" +
      "; SameSite=Lax" +
      "; Max-Age=" + String(maxAgeSeconds || 0);
    if (window.location && window.location.protocol === "https:") {
      cookie += "; Secure";
    }
    document.cookie = cookie;
  }

  function initClientTimezone() {
    var timezone = detectClientTimezone();
    if (!timezone) {
      return;
    }
    window.__ovumcyTimezone = timezone;
    writeClientCookie(TIMEZONE_COOKIE_NAME, timezone, TIMEZONE_COOKIE_MAX_AGE_SECONDS);
  }

  function currentClientTimezone() {
    var known = String(window.__ovumcyTimezone || "").trim();
    if (known && isSafeClientTimezone(known)) {
      return known;
    }

    var detected = detectClientTimezone();
    if (detected) {
      window.__ovumcyTimezone = detected;
    }
    return detected;
  }
