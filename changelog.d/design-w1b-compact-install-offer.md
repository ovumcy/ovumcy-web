### Changed

- **The install offer is one row on a phone, and a permanent entry in settings.** The "Install
  Ovumcy" block on mobile carried a kicker, an icon, a heading, a per-platform paragraph and two
  stacked full-width buttons, which took roughly half of the first screen. It is now a single row —
  the install action plus a dismiss control — shown only while the browser has a native prompt
  pending, and the settings interface section carries a quiet "Install Ovumcy" entry that describes
  the path for this device (native prompt, iOS share sheet, browser menu, or already installed) and
  starts the install where the browser supports it. Dismissing the row therefore no longer hides
  the offer for good, and the dismissal is remembered in the browser, not on the server. Nothing
  about the progressive-web-app surface itself changed: still a manifest and the install prompt,
  with no service worker and no offline cache.
