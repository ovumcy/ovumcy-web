### Fixed

- **The API reference now says that `GET /api/v1/days` requires both `from` and `to`.**
  `docs/openapi.yaml` declared both query parameters optional, although the server refuses a
  request that leaves either one out. Both are now marked required. The `400` now lists the three
  keys the server answers with: `invalid from date` and `invalid to date` for a bound that is
  missing, blank, malformed, not a real calendar date or a day the request's timezone never had,
  and `invalid range` when `to` is before `from`. `from` is checked first, so a request with both
  bounds wrong names only `from`. An HTMX request gets the same refusal as an HTML fragment. A
  guard test sends each case to the real route. The server's behaviour is unchanged.
