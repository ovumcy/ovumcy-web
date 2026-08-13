### Changed

- **A button now picks a role, not a colour.** Primary, secondary and destructive are defined
  once as tokens on top of the semantic layer, together with the single inactive state they
  share, and every button reads them. The primary action paints one fill everywhere: the
  dashboard editor no longer re-tints it per cycle phase, so "Update entry" stops turning green
  on a fertile day. The quiet variant keeps its own accent ink, which now clears WCAG AA hovered
  as well as at rest (it measured 4.34:1 hovered before), and a destructive action finally has a
  hover fill in the dark theme.
- **The two remaining blues in the product are gone.** The calendar's logged-entry marker and
  the ring around the day being viewed were the only blue surfaces anywhere; both move to the
  warm accent ink, which carries its own dark value, and stay separable from the amber today
  ring — 5.29:1 light and 8.67:1 dark on a plain day cell, and 3.14:1 / 4.05:1 at worst over
  the phase washes, above the 3:1 floor a non-text indicator answers to.
- **An enabled setting no longer looks like a warning.** The settings toggles painted their
  on-state in the period rose, so "Auto-fill period days" read as an alert when it was simply
  on; the on-state now uses the affirmative warm accent every other selected control uses, on a
  flat fill.

### Internal

- **The button family lost the two utilities that encoded one intent twice.** `btn-warning` and
  `btn-secondary-caution` both described a cautionary secondary action and neither had a call
  site; the destructive role covers what they were for. `btn--disabled`, the only double-dash
  modifier among the sheet's utilities, is now `btn-disabled`.
- **The primary-action contrast gate also holds the role.** It measured each phase tint
  separately, and every tint cleared the bar on its own, so the gate could not see that there
  were four of them; it now reads the painted fill across every phase and fertility value and
  fails on a second one.
