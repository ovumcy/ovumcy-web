### Changed

- **One rule now decides where a cycle starts, on every surface.** A cycle opens on two or more
  consecutive non-spotting period days, on a non-spotting day marked as a cycle start, or on a lone
  non-spotting period day dated today or yesterday (the period may still be running). Spotting never
  opens a cycle, even when marked, and the onboarding start is a boundary of its own. The completed-cycle
  count, cycle lengths, last period start, dashboard anchor, calendar "recorded" day, stats insights and
  the luteal-phase estimate (day save, restore and the boot recompute alike) all read it, so a single
  bleeding day after a gap no longer counts as a new cycle in the stats while the anchor and the
  next-period estimate stay on the old one.
- **A history that logs each period as one unmarked day loses those cycles.** A lone unmarked
  bleeding day older than yesterday no longer starts a cycle. If you recorded each period as a single
  day without marking it, those periods no longer count as cycles: the completed-cycle count, the cycle
  lengths and the predictions built on them shrink. To restore them, open each such day and mark it as
  a cycle start (or log the second day of that period).
- **Un-ticking the period on the onboarding day withdraws it.** Turning a period day into a
  non-period day on the onboarding start date, or deleting that day, clears that start, so it no
  longer opens a cycle and the calendar no longer paints it. When the start day has no entry (onboarding
  without auto-fill), the day editor and the dashboard's Today form show its period ticked, and
  un-ticking it in either form withdraws the start the same way. Saving a mood with the box still
  ticked, adding only a mood or a symptom to a non-period entry on that date, or a JSON save of that
  date without the period leaves the start in place. A partial save (`PATCH`) that leaves the period
  out never withdraws the start; one that states no period over the start's period day withdraws it
  like a full save.
- **Moving the last period start in Settings takes the auto-filled days along.** Onboarding with
  auto-fill writes the first days of the period; under the new rule they form a cycle start of their
  own, so moving the start left a phantom short cycle behind and kept the dashboard on the old date.
  Saving a new start now writes the new start's days the way onboarding would under your auto-fill
  setting and period length, stopping at today like onboarding's own fill, and marks the new start
  day as a period day if it was logged without one.
  It removes old days only while auto-fill is on in your stored settings, and only when the save
  corrects the old date: the old start still opens your newest cycle, and the new one is earlier or
  less than a shortest cycle (15 days) later. Even then it removes only a fill written as one cohort
  of at least two days: the run onboarding's fill wrote in one go, from the old start up to the first
  day that is missing, that you edited, or that you ticked yourself. A period day you ticked by hand is
  never deleted, whatever the auto-fill setting or period length was when you ticked it, and a period
  length of 1 is never cleared. Installs onboarded before this release keep their old days on a move,
  as they did before. Otherwise the old days stay recorded. Clearing the start moves nothing.
- **The pregnancy pause lifts on a cycle start the rule counts.** A positive test pauses predictions
  until a cycle starts after it. "A cycle starts" is now the same rule as everywhere else, without
  the today-or-yesterday allowance: a marked start or an unmarked two-day bleed after the test lifts
  the pause, while a single bleeding day (even today's), a spotting day or an uncertain mark does not.
  A start on the day of the positive test still keeps the pause.
- **The JSON export carries the onboarding start, and the CSV marks it.** The JSON export gains an
  optional top-level `last_period_start` (`YYYY-MM-DD`), absent when the account has none and also
  absent when the date lies outside a requested export range; the CSV marks `Cycle start` on that date,
  adding a bare row when no day was logged on it, under the same range rule. Restore reads the field
  and sets it only on an account that holds none; an export without it still imports. The field is
  additive: `required` in the schema is unchanged.
