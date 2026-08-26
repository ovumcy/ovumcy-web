### Fixed

- **The maskable PWA icon is now full-bleed.** `web/static/pwa/icon-512-maskable.png` was declared
  `purpose: maskable` in the web app manifest while still carrying the ordinary icon's pre-rounded
  plate and transparent corners — its alpha channel was pixel-identical to the non-maskable
  `icon-512.png`, so 10 168 of its 262 144 pixels were fully transparent and all four corners were
  empty. Installing the app on a platform that applies its own icon mask (Android and other adaptive
  launchers) could therefore show corner gaps or a doubly-rounded, inset icon. The asset is now
  flattened onto the manifest's own `background_color` (`#fff9f0`) and is opaque across all 262 144
  pixels; the artwork is unchanged and stays well inside the maskable safe zone. Re-installing the
  app picks up the new icon; nothing else about the install changes.
