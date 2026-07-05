  var TIMEZONE_SYNC_URL = "/api/v1/users/current/timezone";
  var TIMEZONE_SYNC_STORAGE_KEY = "ovumcy_tz_synced";

  function csrfTokenFromMeta() {
    var meta = document.querySelector('meta[name="csrf-token"]');
    if (!meta) {
      return "";
    }
    return String(meta.getAttribute("content") || "").trim();
  }

  // isAuthenticatedOwnerPage reports whether the current page belongs to a
  // signed-in owner. The base layout renders the data-persisted-timezone
  // attribute ONLY when a CurrentUser is present, so its mere presence is the
  // authenticated-owner marker. Auth/anonymous pages (login, register,
  // forgot-password) omit it entirely — even though they still render the
  // csrf-token meta — so the sync must never fire there. This also keeps the
  // sync POST off /api/v1/users/current/timezone on the register page, where an
  // e2e guard asserts zero requests to /api/v1/users.
  function isAuthenticatedOwnerPage() {
    var body = document.body;
    return !!(body && typeof body.hasAttribute === "function" && body.hasAttribute("data-persisted-timezone"));
  }

  function persistedTimezone() {
    var body = document.body;
    if (!body || typeof body.getAttribute !== "function") {
      return "";
    }
    return String(body.getAttribute("data-persisted-timezone") || "").trim();
  }

  function markTimezoneSynced(value) {
    try {
      window.sessionStorage.setItem(TIMEZONE_SYNC_STORAGE_KEY, value);
    } catch {
      // Ignore storage quota / privacy-mode errors: a failed mark only means we
      // may retry the (idempotent, no-op-on-unchanged) sync later this session.
    }
  }

  function alreadySyncedThisSession(value) {
    try {
      return window.sessionStorage.getItem(TIMEZONE_SYNC_STORAGE_KEY) === value;
    } catch {
      return false;
    }
  }

  // syncClientTimezone POSTs the browser-detected IANA timezone to the dedicated
  // owner endpoint, but only when it is safe, differs from the value the server
  // already persisted, and has not already been synced this session. It attaches
  // the CSRF token via the X-CSRF-Token header (the ovumcy_csrf cookie is
  // HttpOnly and unreadable from JS; the token is mirrored into the csrf-token
  // meta tag — the same source the htmx CSRF hook uses). It fails silently: a
  // background preference sync must never surface an error to the owner.
  function syncClientTimezone() {
    if (typeof window.fetch !== "function") {
      return;
    }

    // Only ever run for a signed-in owner on a normal app page. On auth pages
    // (login/register/forgot-password) there is no owner to sync for, and firing
    // a POST to /api/v1/users/current/timezone there would both 401 and be
    // miscounted by the register-flow e2e guard (it watches /api/v1/users).
    if (!isAuthenticatedOwnerPage()) {
      return;
    }

    var detected = currentClientTimezone();
    if (!detected || !isSafeClientTimezone(detected)) {
      return;
    }

    // Defense in depth: the csrf token is required to POST anyway.
    var token = csrfTokenFromMeta();
    if (!token) {
      return;
    }

    if (detected === persistedTimezone() || alreadySyncedThisSession(detected)) {
      return;
    }

    // Mark before the request so a slow round-trip cannot double-fire from a
    // rapid second navigation; the endpoint is idempotent regardless.
    markTimezoneSynced(detected);

    var body = new URLSearchParams();
    body.set("timezone", detected);

    window.fetch(TIMEZONE_SYNC_URL, {
      method: "POST",
      credentials: "same-origin",
      headers: {
        "Content-Type": "application/x-www-form-urlencoded",
        "X-CSRF-Token": token,
        "X-Requested-With": "XMLHttpRequest"
      },
      body: body.toString()
    }).then(function (response) {
      if (response && response.ok) {
        return;
      }
      // Non-2xx: drop the session mark so a later navigation can retry.
      try {
        window.sessionStorage.removeItem(TIMEZONE_SYNC_STORAGE_KEY);
      } catch {
        // Ignore storage errors.
      }
    }).catch(function () {
      // Network failure: fail silently, allow a later retry.
      try {
        window.sessionStorage.removeItem(TIMEZONE_SYNC_STORAGE_KEY);
      } catch {
        // Ignore storage errors.
      }
    });
  }
