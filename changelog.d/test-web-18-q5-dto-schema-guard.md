### Fixed

- **The API docs describe the JSON bodies the settings saves and probes actually answer.** Six
  settings operations (profile, interface, tracking, reminders, timezone, webhook save) and the two
  withdrawals (webhook removal, calendar-feed revocation) declared the closed `OkResponse` while the
  server answered with the saved values echoed back, so a client validating responses against
  docs/openapi.yaml refused the server's own answer. Each now references its own schema listing the
  keys it carries. `/healthz` and `/readyz` document their fixed `{"status": ...}` bodies, including
  the `503` one; `ExportSummary.date_from`/`date_to` no longer claim `format: date`, since both are
  `""` for an account with no data; `ExportJSONEntry` no longer lists `bbt` as required, since a day
  with no measurement omits it; and the tracking save's request body lists `week_starts_on`. The
  server's behaviour is unchanged. A new contract test drives every JSON success response through
  the real router and compares its keys with the declared schema in both directions, along with
  enums and numeric and length bounds, so an undocumented key, a missing required one or an
  undeclared value fails the suite.
