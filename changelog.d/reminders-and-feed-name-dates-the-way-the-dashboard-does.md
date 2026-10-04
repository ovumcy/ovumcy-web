### Fixed

- **Webhook reminders, the calendar feed, the calendar grid and the stats API no longer name a
  date the dashboard withholds.** With irregular cycle mode on and fewer than three completed
  cycles, the dashboard says more cycles are needed instead of naming a next period or an ovulation
  day, yet the reminders still sent "ovulation around …", the `.ics` feed still carried those days,
  the calendar grid still painted them and `GET /api/v1/stats/overview` still published them. None
  of them does now: the state withholds every projected date on every surface, and the dashboard's
  next-period line shows the "needs more cycles" note on its own, without the date beside it. The
  stats API names it with a new suppression reason, `irregular_needs_more_cycles`.
- **Where the dashboard shows a range, reminders and the feed send that range, not one day.** With
  three or more completed cycles that vary in length the dashboard shows a next-period start window
  (and, in irregular cycle mode, an ovulation range) because a single day would overstate the
  estimate; reminders and the feed kept sending the middle day of that window. A reminder now names
  the first and last day ("Next period estimated to start between … and …"), and the JSON payload
  adds `event_date_end` beside `event_date`, which becomes the window's first day; a single-date
  reminder is unchanged and carries no `event_date_end`. The feed carries the window as one
  multi-day event. Because a window is keyed on its first day, an owner whose reminder for the
  current cycle already went out before this update may receive it once more, as a range.
