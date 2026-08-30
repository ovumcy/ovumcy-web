### Fixed

- **The corrected luteal-phase estimate now reaches the accounts that can no longer recompute one
  themselves.** Ovumcy learns a personal luteal phase from logged BBT or cervical-mucus signal, and
  it used to measure that phase one day too long: it counted the calendar span from the observed
  ovulation to the next period start, while the prediction consumes the number of days that
  *follow* ovulation. An ovulation observed on cycle day 15 predicted cycle day 14, moving the
  ovulation date and both edges of the fertile window a day earlier on the dashboard, the calendar,
  the API, the webhook reminders and the calendar feed. The computation was corrected, but the
  result is also cached on the account, and that cache is only refreshed when the owner saves a day
  or restores an export. An account whose logs no longer support an inference at all — fewer than
  three recorded cycle starts, or fewer than two cycles with a usable ovulation signal — refreshes
  nothing and would have kept predicting a day early indefinitely.

  The first start after the upgrade now recomputes the stored estimate for every owner from their
  own logs, before the server accepts a request, and records a marker so it never runs again.
  Nothing an owner typed is touched: the value has no settings field and no onboarding step behind
  it — it is derived from the day logs alone, and this recomputes it from the same logs the
  dashboard reads. An account with no usable signal lands on the 14-day default the product ships
  with, and an account whose stored estimate already matches its logs is left alone.

  This could not be done in SQL. The repair is not an arithmetic correction of the stored number:
  14 is at once the default, the value written whenever the inference declines, and a plausible
  stale estimate that should now be 13; the plausibility filter that rejects implausible cycles was
  applied to the shifted quantity, so cycles it used to discard now count and cycles it used to
  keep no longer do; and an owner left with too few usable cycles falls back to the default rather
  than to a shifted value. Measured on one such history, a stored 14 recomputes to 18 — four days
  up, from a number indistinguishable from the untouched default. Only re-running the inference
  over the logs finds it.

  A storage failure during the pass does not stop the server: the estimate is a cache with a safe
  fallback, so the failure is logged, the marker is left unwritten, and the next start tries again.

### Internal

- **The three writers of the derived luteal-phase cache now share one rule.** A day save, a bulk
  restore and the new boot recompute each decided for themselves what the cached estimate should be
  — run the inference, fall back to the default — and a disagreement between the stored values and
  the inference that produced them is exactly the defect the recompute exists to repair. All three
  now call `DeriveUserLutealPhase`, so no two of them can drift apart again. The boot pass resolves
  each owner's calendar day through the same `resolveOwnerLocation` the webhook pass and the
  calendar feed use, rather than adding a second owner-timezone resolver.
