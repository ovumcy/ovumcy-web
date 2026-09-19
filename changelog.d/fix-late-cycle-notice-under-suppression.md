### Fixed

- **The late-cycle notice no longer reassures on a cycle its own suppression gate has already
  flagged.** Once the overdue gate (the shorter of the projection and reference lengths, plus a
  week) fires, the notice used to keep comparing the running cycle day against the account's
  recorded maximum, and an account whose maximum was itself a merged span from an unlogged period
  (three 28-day cycles beside one 300-day gap) read "still inside your recorded range of 28 to 300
  days" beside a dashboard that had withheld every projected date since day 36. The notice now
  states the fact the gate is already acting on instead — the cycle has run past the length
  predictions are based on, and predictions wait for a new logged period — whenever the gate has
  fired, rather than a comparison that can land on the very outlier the gate exists to see past.
  The measured-excess and no-personal-range states are unchanged.
