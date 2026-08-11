### Fixed

- **The active language pill is readable.** The pre-auth switcher marked the current language
  with near-white text on a light warm gradient that measured **1.98:1** at its light stop
  (**3.35:1** in the dark theme) — WCAG AA asks 4.5:1 for a label this size. The pill now fills
  with a deeper tone of the same hue: 5.25:1 in the light theme, 5.60:1 in the dark one, worst
  stop of each gradient.
- **Decorative emoji are no longer read out as content.** 48 of the 58 distinct glyphs this UI
  renders reached the accessibility tree, so a screen reader announced "shield", "file cabinet",
  "hourglass", "see-no-evil monkey", "crystal ball" and "electric plug" ahead of the headings,
  toggles and section links they decorate. Every decorative glyph is now `aria-hidden`, and the
  mood chips — whose only content was an emoji — name themselves instead.
- **The cycle-length and period-duration sliders can be hit.** Both offered a pointer target of
  1038×9 px, under the 24 px WCAG 2.2 AA minimum. The rail keeps its slim look while the control
  now claims a 24 px-tall target.
- **Navigation landmarks are told apart.** An authenticated page renders four to five `<nav>`
  elements and most carried no name, so a landmark list showed several identical "navigation"
  entries. The header, mobile menu, bottom bar and footer navigations now carry distinct
  localized labels in all six languages.
- **Selected choice tiles keep a margin over the contrast bar.** The label on a selected goal,
  pregnancy-test, BBT-unit, week-start, language, theme or mood tile measured 4.64:1 against the
  darker end of its fill — a pass with nothing to spare; it now measures 5.60:1.

### Internal

- Browser checks pin the calendar phase cells, the selected tiles, the language pill, the slider
  target size and the closed confirm dialog; template checks pin the decorative-glyph rule, the
  named navigation landmarks, and the two audit findings that needed no change — the
  password-manager username fields and the closed confirm dialog are already outside the
  accessibility tree.
