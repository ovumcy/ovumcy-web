### Fixed

- **The API reference now explains the `id: 0` day and what `HEAD` on a day answers.**
  `GET /api/v1/days/{date}` answers `200` for a day with no stored record, with a placeholder built
  at read time and never stored. `docs/openapi.yaml` now documents that placeholder field by field:
  `id` is `0`, the timestamps are the zero time, and `bbt` is absent. It also says that a stored
  record always has a non-zero `id`. `HEAD` on the same path was described as checking for "at
  least one persisted field". It actually answers whether the day holds data, so the spec now lists
  the values that count. A stored record that carries none of them, such as one created by a `PUT`
  with no values, gets `404`. The `DailyLog` schema no longer calls every day record persisted, and
  no longer says `date` is shifted into the caller's timezone at read time: it is returned as the
  stored calendar day. Guard tests drive the real routes for these claims. The server's behaviour
  is unchanged.
