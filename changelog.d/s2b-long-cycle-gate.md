### Fixed

- **A missing period log no longer lets a long-running cycle keep its predictions.** One unlogged
  period merges two real cycles into a single enormous span, and that span was averaged in with the
  ordinary ones: three 28-day cycles beside one 300-day gap average 96 days. The safety rule that
  withholds every projected date once a cycle runs a week past its expected length was measured
  against that average, so an account on day 61 of its current cycle was compared against 103 —
  and kept a next-period date, a fertile window and an ovulation day on the dashboard, the
  calendar, `/stats`, the JSON API, the webhook reminders and the `.ics` feed, all of them
  calculated from the 28-day median the projection actually uses. The rule now measures against the
  very length those dates are calculated from, so no single outlying span can raise the bar, and the
  late-cycle notice and the hero cycle ribbon follow that same length rather than each answering
  separately. An ordinary 28-day pattern and a genuinely long-but-regular one (three real 50-day
  cycles) keep the exact threshold they had; a history whose average sits above its median now stops
  publishing dates on the day its own estimate has run out rather than a few days later. No recorded
  day is changed or removed — the merged span is still there to be corrected by logging the missing
  period.
