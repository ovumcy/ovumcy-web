(function () {
  "use strict";

  var THEME_STORAGE_KEY = "ovumcy_theme";
  var THEME_LIGHT = "light";
  var THEME_DARK = "dark";
  var THEME_SYSTEM = "system";
  var THEME_COLOR_LIGHT = "#fff9f0";
  var THEME_COLOR_DARK = "#18141f";

  // This file runs before the stylesheet so the first paint already carries the
  // resolved theme. "system" and a never-chosen preference both resolve here,
  // and `data-theme` only ever gets light or dark.
  function normalizeTheme(value) {
    var theme = String(value || "").trim().toLowerCase();
    if (theme === THEME_DARK || theme === THEME_LIGHT) {
      return theme;
    }
    return "";
  }

  function readStoredTheme() {
    try {
      var stored = String(window.localStorage.getItem(THEME_STORAGE_KEY) || "").trim().toLowerCase();
      if (stored === THEME_SYSTEM) {
        return "";
      }
      return normalizeTheme(stored);
    } catch {
      return "";
    }
  }

  function systemPreferredTheme() {
    if (typeof window.matchMedia !== "function") {
      return THEME_LIGHT;
    }
    return window.matchMedia("(prefers-color-scheme: dark)").matches ? THEME_DARK : THEME_LIGHT;
  }

  function resolveTheme() {
    return readStoredTheme() || systemPreferredTheme();
  }

  function updateThemeColorMeta(theme) {
    var meta = document.getElementById("theme-color-meta");
    if (!meta) {
      return;
    }
    meta.setAttribute("content", theme === THEME_DARK ? THEME_COLOR_DARK : THEME_COLOR_LIGHT);
  }

  var theme = resolveTheme();
  document.documentElement.setAttribute("data-theme", theme);
  window.__ovumcyTheme = theme;
  updateThemeColorMeta(theme);
})();
