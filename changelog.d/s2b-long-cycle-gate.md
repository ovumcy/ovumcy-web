### Fixed

- **A missing period log no longer lets a long-running cycle keep its predictions.** One unlogged
  period merges two real cycles into a single enormous span, and that span was averaged in with the
  ordinary ones: three 28-day cycles beside one 300-day gap average 96 days. The safety rule that
  withholds every projected date once a cycle runs a week past its expected length was measured
  against that average, so an account on day 61 of its current cycle was compared against 103 —
  and kept a next-period date, a fertile window and an ovulation day on the dashboard, the
  calendar, `/stats`, the JSON API, the webhook reminders and the `.ics` feed, all of them
  calculated from the 28-day median the projection actually uses. The rule now measures against the
  shorter of the average and the length those dates are calculated from, so no single outlying
  span can raise the bar and no history withholds later than it did before; the late-cycle notice
  follows that same length rather than answering separately, and the cycle ribbon on the
  dashboard, which is drawn entirely from the projection, is hidden while the estimate is paused
  rather than drawn around a date that is being withheld. The same rule now also covers the
  message shown after saving a day, which could still call a day fertile from a withheld window,
  and the implantation-bleeding hint offered when a new cycle start is logged — which is now also
  withheld in unpredictable-cycle mode and during a pregnancy pause, as every other projection
  already was. An ordinary 28-day
  pattern and a genuinely long-but-regular one (three real 50-day cycles) keep the exact threshold
  they had; a history whose average sits above its median now stops publishing dates on the day
  its own estimate has run out rather than a few days later. No recorded day is changed or
  removed — the merged span is still there to be corrected by logging the missing period.
- **An ovulation day confirmed by your temperatures stays visible when a long cycle pauses the
  estimate.** It used to disappear from the dashboard, the calendar and the JSON API together with
  the projected dates. It is a day read from your own recorded readings, not a projection, so it is
  now still shown — as an estimate, beside the usual disclaimer — while the fertile window and the
  fertility status stay withheld until the next cycle starts.
