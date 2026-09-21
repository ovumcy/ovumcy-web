### Added

- **The calendar feed carries the temperature-confirmed ovulation day.** Once a basal body
  temperature shift confirms ovulation in the current cycle, the `.ics` feed publishes that day,
  exactly when the dashboard, the calendar and the JSON API show it — including after a cycle has
  run so long that projected dates are paused. It keeps the feed's neutral title and the
  medical-safety disclaimer, and it shares the ovulation event's identifier, so a calendar app that
  already holds a projection for the same day updates it rather than adding a second one. It is
  withheld in irregular (unpredictable) cycle mode, during a pregnancy pause and before the first
  completed cycle, and earlier cycles are never included. Webhook reminders are unchanged: they
  announce only days still ahead.

### Changed

- **The calendar feed is described as carrying estimated days.** The settings card, the subscribe
  page and the privacy notice said the feed carries predicted days; they now say estimated, in all
  six languages, since the feed can also carry a day confirmed by temperature readings.
- **The onboarding cycle-length hint quotes a common range of about 24-38 days**, the FIGO normal
  range for cycle frequency, instead of 21-35 days, in all six languages. The documentation now
  marks which cycle thresholds are clinical (the 24-day short-cycle boundary) and which are
  engineering heuristics. No prediction behaviour changes.

### Fixed

- **The German calendar-feed texts use the formal address.** The feed's settings card, subscribe
  page and status messages addressed the owner as "du" while the rest of the German interface uses
  "Sie"; they now use "Sie" throughout.
