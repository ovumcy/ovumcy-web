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
- **Un-marking the onboarding day withdraws it.** A day logged on the onboarding start date that is
  not a period day now withdraws that start as a cycle boundary, and the calendar no longer paints it
  as a period day.
- **Moving the last period start in Settings takes the auto-filled days along.** Onboarding with
  auto-fill writes the first days of the period; under the new rule they form a cycle start of their
  own, so moving the start left a phantom short cycle behind and kept the dashboard on the old date.
  Saving a new start now removes the old start's auto-filled days that you have not edited (a day
  carrying anything you entered stays), writes the new start's days the way onboarding would under your
  auto-fill setting and period length, and marks the new start day as a period day if it was logged
  without one. Clearing the start moves nothing.
- **The pregnancy pause lifts on a cycle start the rule counts.** A positive test pauses predictions
  until a cycle starts after it. "A cycle starts" is now the same rule as everywhere else: an unmarked
  two-day bleed after the test lifts the pause, while a spotting day or an uncertain mark does not. A
  start on the day of the positive test still keeps the pause.
- **The JSON export carries the onboarding start, and the CSV marks it.** The JSON export gains an
  optional top-level `last_period_start` (`YYYY-MM-DD`), absent when the account has none and also
  absent when the date lies outside a requested export range; the CSV marks `Cycle start` on that date,
  adding a bare row when no day was logged on it, under the same range rule. Restore reads the field
  and sets it only on an account that holds none; an export without it still imports. The field is
  additive: `required` in the schema is unchanged.
