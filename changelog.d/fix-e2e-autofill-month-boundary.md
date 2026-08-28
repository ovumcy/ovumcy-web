### Internal

- **The calendar auto-fill e2e spec no longer fails at the end of every month.** Both tests
  anchored on `today−30` and asserted the four following days in the anchor month's calendar view;
  from the 28th of a month onward those neighbors spill into the next month, whose day cells the
  anchor month's grid does not always render (2026-08-28: anchor Jul 29, neighbor Aug 2 — July's
  Sunday-start grid ends Aug 1, so the neighbor's button does not exist and all retries fail
  identically). The anchor is now the 5th of the month holding `today−30`, which keeps the whole
  auto-fill window mid-month — still at least ~3 weeks from `lastPeriodStart = today−3` and from
  the predicted next period — so the spec is date-independent. Test-only change.
