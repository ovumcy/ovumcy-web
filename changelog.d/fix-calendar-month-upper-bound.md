### Security

- **`GET /calendar` no longer accepts an unbounded month.** A syntactically valid but far-future
  month (`?month=9999-12`) made `BuildCalendarDayStates` walk cycle predictions with no upper
  bound, costing 1.36 s and over 3.1M allocations per request on an otherwise ordinary page.
  Navigation is now clamped to three years ahead of today, mirroring the existing three-year lower
  bound: a request past the bound renders the clamped month instead of the one asked for, and the
  "next" link disables at the upper edge exactly as "prev" already does at the lower one.
  Independently of the clamp, cycle projection itself now stops after a fixed number of cycles
  regardless of how far the grid asked it to go, and the page has its own rate limit
  (`RATE_LIMIT_CALENDAR_MAX` / `RATE_LIMIT_CALENDAR_WINDOW`, default 300 requests per minute),
  keyed the same way as the other authenticated routes.
