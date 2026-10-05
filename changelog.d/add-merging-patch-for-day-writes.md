### Added

- **`PATCH /api/v1/days/{date}` updates only the fields a request names.** `PUT` on the same path
  replaces the whole day, so a client that sent `{"mood": 3}` to a period day also erased the period,
  the flow, the temperature and the notes. Because the day was no longer a period day, it also lost
  its cycle start, and the cycle statistics moved. `PATCH` merges instead: a field the body omits keeps
  its stored value, the cycle start included, and an explicit `null` clears a field. The rules that
  follow from the period flag still apply. Stating `is_period: false` clears the flow and the cycle
  start, exactly as it does under `PUT`. `PATCH` has the same owner, CSRF, rate-limit and validation
  rules as `PUT`. Two concurrent `PATCH` requests on the same day that name different fields both
  keep their field, on PostgreSQL as on SQLite, including when the day had no entry yet. An
  `ack_period_tip` sent with a `PATCH` that leaves `is_period` out counts when the saved day is a
  period day. A field the account hides is never changed through a form body. A form-encoded day
  write, `PUT` or `PATCH`, reads its fields from the request body only, never from the URL query
  string. `PUT` stays a full replace; `docs/openapi.yaml` now warns about this and points clients
  that write only some fields at `PATCH`. The bundled UI still saves with `PUT`. On PostgreSQL, a
  `PUT`, a manual cycle-start mark or the period autofill that runs at the same time as another
  write of the same day no longer reverts that write's fields, a field the account hides included.
