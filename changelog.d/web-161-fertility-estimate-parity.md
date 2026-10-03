### Fixed

- **The irregular-cycle ovulation range on the dashboard is no longer a day late.** The range
  was the next-period range shifted back by the luteal phase, which puts ovulation one day after
  where the cycle model — and therefore the calendar, the `.ics` feed and the reminders — places it.
  With a 24-day shortest and a 45-day longest cycle from 1 March the dashboard showed 11 March to
  1 April while the model gives 10 to 31 March, so the earliest possible ovulation was named a
  day late. Both ends now come from the same predictor as every other surface. When the shortest
  observed cycle is too short for the model to place an ovulation at all (under 15 days), the old
  shift put the start of the range before the cycle even began (with a 12-day shortest cycle,
  luteal phase 14 and a period starting on 1 March, on 27 February); the range now starts at the
  earliest day the model ever names (cycle day 5).
- **Webhook reminders are decided from the same two years of history as the app.** The reminder
  pass read the owner's whole stored history while the dashboard, calendar, stats page and `.ics`
  feed read the last two years. With old cycles outside that window, the app could pause its
  next-period estimate while a reminder was still sent to the webhook endpoint (or the other way
  round). Both now cut the history through one shared helper.
- **The "saved" message names the same fertile days as the dashboard, and says it is an
  estimate.** After saving a day the message derived its own cycle statistics with the default
  14-day luteal phase and no confirmed temperature shift, so an owner whose luteal phase had been
  inferred as 10 days heard "fertile" on days the dashboard showed nothing and nothing on days it
  shaded. It now goes through the same history window, personal baseline, confirmed-shift step and
  fertility gate as the dashboard. The wording in all six languages now reads as an estimate
  ("this day is likely inside your estimated fertile window") instead of stating a certainty.
  Like the dashboard, which prints "unknown" for the fertility status once the running cycle has
  outrun its reference length, the message no longer calls a day fertile on an out-of-date cycle.
