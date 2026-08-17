### Fixed

- **The reminder scheduler no longer spins through a full notify pass every few
  milliseconds on a daylight-saving night.** With the daily run hour set to `0`
  and a server timezone whose clock jump lands on midnight (America/Santiago,
  America/Havana), the next scheduled run resolved to 23:00 of the *current*
  day — an instant already in the past. The wait until it was negative, so the
  pass fired immediately, re-listed every account and re-decided its reminders,
  and then computed the same past instant again, repeating from local 23:00
  until the transition roughly an hour later. Outbound webhook delivery was
  attempted on each turn; the per-reminder already-sent watermark kept duplicate
  notifications from going out, but not the work. On such a date the daily pass
  now runs at the first moment the date actually has — the transition itself,
  the same resolution the rest of the app already uses for a calendar day with
  no midnight — so that day's reminders are still delivered rather than skipped.
  Every other timezone and run hour is unaffected.
