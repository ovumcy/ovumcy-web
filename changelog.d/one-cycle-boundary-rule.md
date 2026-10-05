### Changed

- **One rule now decides where a cycle starts, on every surface.** A cycle opens on two or more
  consecutive non-spotting period days, on a non-spotting day marked as a cycle start, or on a lone
  non-spotting period day dated today or yesterday (the period may still be running). Spotting never
  opens a cycle, even when marked, and the onboarding start is a boundary of its own. The completed-cycle
  count, cycle lengths, last period start, dashboard anchor, calendar "recorded" day, stats insights and
  the luteal-phase estimate (day save, restore and the boot recompute alike) all read it, so a single
  bleeding day after a gap no longer counts as a new cycle in the stats while the anchor and the
  next-period estimate stay on the old one. A lone unmarked bleeding day older than yesterday no longer
  starts a cycle; mark it as a cycle start to keep it as one.
- **The JSON export carries the onboarding start, and the CSV marks it.** The JSON export gains an
  optional top-level `last_period_start` (`YYYY-MM-DD`), absent when the account has none; the CSV marks
  `Cycle start` on that date, adding a bare row when no day was logged on it. Restore reads the field
  and sets it only on an account that holds none; an export without it still imports. The field is
  additive: `required` in the schema is unchanged.
