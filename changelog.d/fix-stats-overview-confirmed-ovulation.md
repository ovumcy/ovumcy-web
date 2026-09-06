### Fixed

- **`GET /api/v1/stats/overview` now names the ovulation day the temperatures confirm, not the day
  the cycle model projected before any of them arrived.** When basal body temperature is tracked and
  a sustained thermal shift is detected in the current cycle, that shift confirms an ovulation that
  has already happened. The calendar's solid marker, the dashboard's ovulation line, the stats chart
  marker and the two outbound surfaces (the `.ics` feed and the webhook reminder pass) already
  resolve this through the shared detector; the JSON API kept publishing the model's superseded
  projection instead. `ovulation_date` now carries the confirmed day when one exists.

### Added

- **`GET /api/v1/stats/overview` gains `ovulation_confirmed`.** It names the substitution above
  directly, mirroring the dashboard's own confirmed/exact distinction: `ovulation_exact` keeps its
  original meaning (an exact per-owner luteal-phase fit vs. the clamped 14-day fallback) and
  `ovulation_confirmed` reports separately whether `ovulation_date` is a day inferred from the
  owner's own temperature shift — not a measurement of the ovulation itself — rather than a model
  projection. The two can differ, and a fallback-luteal account's confirmed cycle is exactly
  that case. Suppression is unaffected: a confirmed observation changes which day is named, never
  whether one may be named at all.
