### Fixed

- **Breaking (API shape): a basal body temperature written in Fahrenheit via the JSON API now
  converts, same as the form, so `bbt` on a write is read in the account's own unit.** A client
  that sends Celsius for a Fahrenheit-preference account has to send Fahrenheit instead.
  `PUT /api/v1/days/{date}` accepts `bbt` from both a browser form post and a JSON body; only the
  form path converted the value into the account's preferred unit before storing it. A Fahrenheit-preference
  owner posting JSON (`{"bbt": 98.6}`) had it validated straight against the Celsius physiological
  range and refused — no bad data was ever stored, but every JSON write of BBT failed for her. The
  JSON path now converts through the same unit-aware helper the form path uses. `docs/openapi.yaml`
  also now states that a read of `bbt` is always Celsius, regardless of the account's write-side unit.
