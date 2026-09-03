### Fixed

- **`docs/openapi.yaml` mischaracterized `ForgotPasswordRequest.recovery_code`.** The schema's
  pattern required the full `OVUM-XXXX-XXXX-XXXX` shape even though `ForgotPassword` treats an
  absent or blank (after trim) `recovery_code` as the step-1 request and answers it successfully —
  never as a refusal. A client that validates its step-1 body (`email` only, `recovery_code: ""`)
  against the published spec before sending it would have the request rejected by its own tooling
  even though the server accepts it. The pattern now has a `|^$` alternative for that one value, and
  both the field's own description and the schema's top-level description now say plainly that
  `recovery_code`'s absent/blank case selects step 1 rather than being refused — unlike `password`,
  whose absent/blank case in step 2 is refused by name.
