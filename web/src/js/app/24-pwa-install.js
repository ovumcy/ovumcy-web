  var PWA_INSTALL_DISMISS_STORAGE_KEY = "ovumcy_pwa_install_hidden_v1";
  var PWA_INSTALL_FALLBACK_DELAY_MS = 1200;

  var pwaInstallDeferredEvent = null;
  var pwaInstallFallbackTimer = 0;
  var pwaInstallSubscribers = [];
  // `dismissed` hides the one-time mobile offer only. Availability survives it so
  // the settings entry can still install after the offer has been dismissed.
  var pwaInstallState = {
    available: false,
    busy: false,
    dismissed: false,
    installed: false,
    mode: ""
  };

  function readLocalStorageValue(key) {
    if (!key) {
      return "";
    }
    try {
      return String(window.localStorage.getItem(key) || "");
    } catch {
      return "";
    }
  }

  function writeLocalStorageValue(key, value) {
    if (!key) {
      return;
    }
    try {
      window.localStorage.setItem(key, String(value || ""));
    } catch {
      // Ignore storage quota and privacy mode errors.
    }
  }

  function removeLocalStorageValue(key) {
    if (!key) {
      return;
    }
    try {
      window.localStorage.removeItem(key);
    } catch {
      // Ignore storage cleanup failures.
    }
  }

  function wasPWAInstallDismissed() {
    return readLocalStorageValue(PWA_INSTALL_DISMISS_STORAGE_KEY) === "1";
  }

  function storePWAInstallDismissed() {
    writeLocalStorageValue(PWA_INSTALL_DISMISS_STORAGE_KEY, "1");
  }

  function clearPWAInstallDismissed() {
    removeLocalStorageValue(PWA_INSTALL_DISMISS_STORAGE_KEY);
  }

  function isStandalonePWA() {
    if (window.matchMedia && window.matchMedia("(display-mode: standalone)").matches) {
      return true;
    }
    return window.navigator && window.navigator.standalone === true;
  }

  function pwaUserAgent() {
    if (!window.navigator) {
      return "";
    }
    return String(window.navigator.userAgent || window.navigator.vendor || "").toLowerCase();
  }

  function isIOSDevice() {
    var ua = pwaUserAgent();
    if (/iphone|ipad|ipod/.test(ua)) {
      return true;
    }
    return !!(window.navigator && window.navigator.platform === "MacIntel" && window.navigator.maxTouchPoints > 1);
  }

  function isLikelyMobileClient() {
    if (window.matchMedia) {
      if (window.matchMedia("(max-width: 640px)").matches) {
        return true;
      }
      if (window.matchMedia("(pointer: coarse)").matches && window.matchMedia("(max-width: 900px)").matches) {
        return true;
      }
    }

    return /android|iphone|ipad|ipod|mobile/.test(pwaUserAgent());
  }

  function clonePWAInstallState() {
    return {
      available: !!pwaInstallState.available,
      busy: !!pwaInstallState.busy,
      dismissed: !!pwaInstallState.dismissed,
      installed: !!pwaInstallState.installed,
      mode: String(pwaInstallState.mode || "")
    };
  }

  function emitPWAInstallState() {
    var snapshot = clonePWAInstallState();
    for (var index = 0; index < pwaInstallSubscribers.length; index++) {
      pwaInstallSubscribers[index](snapshot);
    }
  }

  function setPWAInstallState(nextState) {
    var safeState = nextState || {};
    pwaInstallState.available = !!safeState.available;
    pwaInstallState.busy = !!safeState.busy;
    pwaInstallState.dismissed = !!safeState.dismissed;
    pwaInstallState.installed = !!safeState.installed;
    pwaInstallState.mode = String(safeState.mode || "");
    emitPWAInstallState();
  }

  function clearPWAInstallFallbackTimer() {
    if (!pwaInstallFallbackTimer) {
      return;
    }
    window.clearTimeout(pwaInstallFallbackTimer);
    pwaInstallFallbackTimer = 0;
  }

  // A dismissed offer no longer suppresses the fallback or the deferred prompt:
  // both keep resolving so the settings entry can describe (and, where the browser
  // supports it, run) the install. Only the mobile offer reads `dismissed`.
  function schedulePWAInstallFallback() {
    if (isStandalonePWA()) {
      return;
    }

    clearPWAInstallFallbackTimer();
    pwaInstallFallbackTimer = window.setTimeout(function () {
      if (pwaInstallDeferredEvent || isStandalonePWA()) {
        return;
      }

      if (isIOSDevice()) {
        setPWAInstallState({
          available: true,
          busy: false,
          dismissed: wasPWAInstallDismissed(),
          installed: false,
          mode: "ios"
        });
        return;
      }

      if (isLikelyMobileClient()) {
        setPWAInstallState({
          available: true,
          busy: false,
          dismissed: wasPWAInstallDismissed(),
          installed: false,
          mode: "menu"
        });
      }
    }, PWA_INSTALL_FALLBACK_DELAY_MS);
  }

  function dismissPWAInstallOffer() {
    clearPWAInstallFallbackTimer();
    storePWAInstallDismissed();
    setPWAInstallState({
      available: pwaInstallState.available,
      busy: false,
      dismissed: true,
      installed: pwaInstallState.installed,
      mode: pwaInstallState.mode
    });
  }

  function markPWAInstalled() {
    pwaInstallDeferredEvent = null;
    clearPWAInstallFallbackTimer();
    clearPWAInstallDismissed();
    setPWAInstallState({
      available: false,
      busy: false,
      dismissed: false,
      installed: true,
      mode: ""
    });
  }

  function handleBeforeInstallPrompt(event) {
    if (!event) {
      return;
    }
    if (isStandalonePWA()) {
      return;
    }

    if (typeof event.preventDefault === "function") {
      event.preventDefault();
    }
    pwaInstallDeferredEvent = event;
    clearPWAInstallFallbackTimer();
    setPWAInstallState({
      available: true,
      busy: false,
      dismissed: wasPWAInstallDismissed(),
      installed: false,
      mode: "prompt"
    });
  }

  function initPWAInstallPrompt() {
    if (window.__ovumcyPWAInstallInitialized) {
      return;
    }
    window.__ovumcyPWAInstallInitialized = true;

    window.addEventListener("beforeinstallprompt", handleBeforeInstallPrompt);
    window.addEventListener("appinstalled", markPWAInstalled);

    setPWAInstallState({
      available: false,
      busy: false,
      dismissed: wasPWAInstallDismissed(),
      installed: isStandalonePWA(),
      mode: ""
    });

    schedulePWAInstallFallback();
  }

  function requestPWAInstallation() {
    if (!pwaInstallDeferredEvent || typeof pwaInstallDeferredEvent.prompt !== "function") {
      return Promise.resolve(false);
    }

    var installEvent = pwaInstallDeferredEvent;
    setPWAInstallState({
      available: true,
      busy: true,
      dismissed: pwaInstallState.dismissed,
      installed: false,
      mode: "prompt"
    });

    return Promise.resolve(installEvent.prompt())
      .catch(function () {
        return null;
      })
      .then(function () {
        return installEvent.userChoice;
      })
      .catch(function () {
        return { outcome: "dismissed" };
      })
      .then(function (choice) {
        var outcome = choice && choice.outcome ? String(choice.outcome) : "dismissed";
        pwaInstallDeferredEvent = null;

        if (outcome === "accepted") {
          markPWAInstalled();
          return true;
        }

        // The browser consumed the deferred event, so nothing can be prompted
        // again on this page load; a reload re-arms `beforeinstallprompt` and the
        // settings entry with it.
        storePWAInstallDismissed();
        setPWAInstallState({
          available: false,
          busy: false,
          dismissed: true,
          installed: false,
          mode: ""
        });
        return false;
      });
  }

  function subscribePWAInstallState(listener) {
    if (typeof listener !== "function") {
      return function () {};
    }

    pwaInstallSubscribers.push(listener);
    listener(clonePWAInstallState());

    return function () {
      pwaInstallSubscribers = pwaInstallSubscribers.filter(function (candidate) {
        return candidate !== listener;
      });
    };
  }
