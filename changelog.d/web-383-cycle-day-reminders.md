### Added

- **Cycle-day reminders (#1071).** Settings → Outbound data now lets an owner add up to 10
  webhook reminders on a fixed cycle day (1–90), each with its own time of day and an optional
  private label — for example a medication on cycle days 15 and 25. A reminder is sent once per
  cycle, on its day, from the chosen owner-local hour; it is held while no cycle start is logged,
  during pregnancy mode, for day 1 or 2 counted from an unconfirmed single bleeding day, and once
  the cycle runs past its usual length, when one neutral `cycle-day-held` notice is sent instead.
  The label stays on the server unless the owner opts in per reminder. Payloads add the `cycle-day`
  and `cycle-day-held` types and a `cycle_day` field; the built-in scheduler gains an hourly
  cycle-day pass, and an operator running `ovumcy notify` from cron should run it hourly.
  Migration 045 adds the `cycle_day_reminders` table and one `users` column; clear-data and account
  deletion remove the reminders.
