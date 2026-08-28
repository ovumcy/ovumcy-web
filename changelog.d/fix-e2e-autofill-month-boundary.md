### Internal

- **Two e2e specs no longer fail in the month-end window.** Both calendar auto-fill tests
  anchored on `today−30` and asserted the four following days in the anchor month's calendar view;
  near month end (the 28th onward in a 31-day month, as early as the 25th in February) those
  neighbors spill into the next month, whose day cells the anchor month's grid does not always
  render (2026-08-28: anchor Jul 29, neighbor Aug 2 — July's Sunday-start grid ends Aug 1, so all
  retries failed identically). The anchor is now the 5th of the month holding `today−30`, which
  keeps the whole auto-fill window mid-month; the window's last day stays at least ~19 days from
  `lastPeriodStart = today−3` and well clear of the predicted next period. The same class lived in
  the baseline-period test in `bugs.spec.ts`, whose `preFertileDay = today+1` crosses into the
  next month at month end and is unrendered whenever the old month's last day closes the grid's
  final week (2026-02-28 was such a Saturday; the next is 2026-10-31) — that assertion now runs in
  `preFertileDay`'s own month view, where the cell always exists and the per-day state is
  unchanged. Test-only
  change.
