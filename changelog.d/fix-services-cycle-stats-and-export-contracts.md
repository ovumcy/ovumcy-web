### Changed

- **The settings page reads the owner's entries once per render.** The export panel's figures — the
  selectable date range and the default window's totals — were two separate reads of every
  `daily_logs` row, each walked in Go. They now come from one read, so the slowest page in a long
  history costs half of what it did.

### Fixed

- **The Insights cycle stack no longer marks a fertile window it cannot locate.** A completed cycle
  shorter than 19 days has no room for a full luteal phase, so the ovulation day behind its fertile
  shading, its peak band and its ovulation cell was a clamped fallback rather than an estimate from
  the account's own data. Those rows now show what was recorded — their length and their period
  days — and nothing inferred; the marks stay on every cycle whose window fits.
- **A day in the cycle stack can no longer be both a period day and the fertile peak.** On a short
  cycle the two landed together and the row said both at once, leaving the display to decide. The
  peak is now the ovulation cell itself, so one day carries one claim.
- **A cycle drawn past the stack's axis says so.** Two cycles longer than the axis were drawn as two
  identical full-width rows while the numbers beside them differed; such a row is now flagged as
  truncated rather than reading as a complete comparison.
- **A temperature entered in Fahrenheit comes back exactly as it was typed.** Readings are stored in
  Celsius, and the stored value was rounded to two Celsius decimals — a step too coarse to represent
  a two-decimal Fahrenheit entry, so 98.61 °F redisplayed as 98.62 °F after every save. Stored
  readings now keep enough precision for the owner's own unit; nothing already stored changes.
- **A cycle start in the previous December can be entered in settings.** The form accepted only
  dates from 1 January of the current year, so in early January the anchor every prediction is
  measured from could not be recorded truthfully. The accepted window is now the last year, measured
  from today; every date the old bound accepted is still accepted.
- **A custom symptom named "Mood" exports as itself.** It was written into the built-in "Mood
  swings" column and left out of the other-symptoms list, so an export carried the wrong symptom and
  a re-import restored it. Export columns are now keyed on the built-in catalog, one column per
  built-in symptom.

### Internal

- The CSV header row is handed out as a copy instead of as a shared package-level slice, and the
  export panel's date comparison reads the same operands in both of its arms.
