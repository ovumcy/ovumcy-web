### Changed

- **Breaking (API shape): `POST /api/v1/password-resets` requires the account password alongside the
  recovery code, and now says so to a client that omits it.** The password operand shipped earlier as
  a security fix — a recovery code stands in for the second factor, never for the first, so a single
  captured secret could no longer take over an account — but the stable `/api/v1` surface gained a
  required member without a major version, and a caller still sending the v1.9.2 body of
  `(email, recovery_code)` was answered "invalid recovery code". The code was not invalid; the
  contract had moved, and the answer sent integrators looking for a fault in the wrong place. A JSON
  body that carries no `password` member at all is now refused with its own key,
  `recovery reset requires the account password`, decided before any account is read. A wrong or
  empty password is untouched: it stays inside the deliberately uniform credential refusal, which
  reveals nothing about whether the address, the code or the password was the wrong one. Form
  submissions keep that uniform refusal too — in form encoding an omitted field and an empty one are
  the same thing on the wire, so the question cannot honestly be asked there.

- **Breaking (API shape): onboarding step 2 no longer accepts `age_group`, and refuses a body that
  still sends it.** The step stopped asking for the age bracket; the column is written by
  `PATCH /api/v1/users/current/cycle` alone. Until now a client submitting the v1.9.2 body received
  `200` with the field quietly dropped, which is indistinguishable from a save that worked —
  the one answer a removed field must never give. `POST /api/v1/onboarding/steps/2` now answers
  `400` with the key `onboarding does not accept an age group`, on both the JSON and the form
  transport, and writes nothing. Clients that stopped sending the field are unaffected; those that
  still send it should drop it and set the bracket from the cycle-settings endpoint or Settings.
