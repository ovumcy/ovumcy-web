### Fixed

- **`POST /api/v1/users` documents the `consent` field it has always required.** `RegisterRequest`
  declared `required: [email, password, confirm_password]` next to `additionalProperties: false`,
  while the handler refuses any body whose `consent` is not truthy — so a client generated from the
  spec was refused for omitting a field the same document forbade it to send. The field is published
  with its accepted spellings (`1`, `true`, `on`, `yes`, case-insensitive, trimmed) and the refusal
  it produces. Found on the load stand, where the harness could not register until it sent `consent`.
  A new contract test reads the required set out of `docs/openapi.yaml` and lets the endpoint judge
  it: a body carrying exactly the declared required fields must not be refused as invalid, so a
  field the server starts demanding without documenting it fails the suite.
