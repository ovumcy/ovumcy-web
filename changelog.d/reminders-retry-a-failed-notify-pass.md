### Fixed

- **A reminder pass that failed outright no longer costs the whole day's reminders.** The built-in
  scheduler keeps a once-per-local-day marker so a restart cannot re-fire the day's pass, and it used
  to advance that marker even when the pass had returned an error. The only error a pass raises at
  that level is "the list of owners could not be read" — raised before a single owner is processed —
  so a moment of database trouble at the scheduled hour recorded a day on which nothing was sent as a
  day already done, and a restart later the same day saw the marker, skipped the run, and sent
  nothing either. Marking it was deliberate: it stopped a broken database from making the scheduler
  spin. The pass is now retried instead — a few minutes apart, up to three attempts in the same slot
  — and the day is marked only once one succeeds or the attempts are spent, so the anti-spin property
  is kept while a transient failure costs minutes rather than every owner's reminders. A process
  stopped between two attempts leaves the day unmarked, so the next start catches it up, and the
  per-reminder watermark still guarantees a retry cannot re-send what an earlier attempt delivered.
  A pass that panics is unchanged: it is still contained, still leaves the day unmarked, and is still
  retried by the next scheduled fire rather than immediately.
