### Fixed

- **The statistics page no longer claims a fertile window while cycle predictions
  are paused by a positive pregnancy test.** The phase card there decided whether
  to show estimates from the unpredictable-cycle setting alone, so it was the one
  prediction surface the pregnancy pause never reached: the calendar grid, the
  calendar feed and the outbound reminders all withheld their projections while
  the statistics page kept showing "Fertile window", derived from the cycle
  length set during onboarding rather than from anything recorded. It now reads
  the same paused-or-off decision the dashboard already resolves, and shows the
  logged-history view instead. Nothing changes for an owner without a pause.
