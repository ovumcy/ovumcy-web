### Fixed

- **`GET /api/v1/stats/overview` now names the ovulation day the temperatures confirm, not the day
  the cycle model projected before any of them arrived.** When basal body temperature is tracked and
  a sustained thermal shift is detected in the current cycle, that shift confirms an ovulation that
  has already happened. The calendar's solid marker, the dashboard's ovulation line, the stats chart
  marker and the two outbound surfaces (the `.ics` feed and the webhook reminder pass) already
  resolve this through the shared detector; the JSON API kept publishing the model's superseded
  projection instead. `ovulation_date` now carries the confirmed day when one exists, and
  `ovulation_exact` reports `true` for it — a measurement, not an approximate estimate. Suppression
  is unaffected: a confirmed observation changes which day is named, never whether one may be named
  at all.
