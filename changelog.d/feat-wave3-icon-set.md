### Changed

- **Interface glyphs are drawn by a first-party icon set instead of emoji.** Navigation,
  section headings, toggles, quick actions, banners and the phase chip now render inline
  SVG icons drawn in one hand — one stroke weight, round corners, `currentColor`, so they
  follow the text colour and the theme. The sprite is inlined in the page, so nothing is
  fetched and the strict content security policy is unaffected. The icons are decorative
  and stay out of the accessibility tree: a screen reader no longer reads "shield",
  "hourglass" or "crystal ball" as page content, and the icons look the same on every
  platform instead of being whatever the system emoji font draws. Mood faces and symptom
  icons are logged data, not chrome, and are unchanged.
