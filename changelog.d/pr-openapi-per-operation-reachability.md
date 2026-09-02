### Fixed

- **`docs/openapi.yaml` now declares every status each operation's own handler chain can answer
  with.** Thirty-two declarations were missing across twenty-eight operations, all pre-existing;
  none is a v2.0.0 breaking change, and no server behavior moved. The gap was structural: the
  existing contract test proves `declared ⊆ emittable` over the whole server at once, so a status
  emittable *somewhere* satisfied it even on an operation that never declared it.
  - Fifteen operations answer `403` and did not say so — eleven behind the owner-only gate
    (`GET /api/v1/days`, `GET /api/v1/days/{date}`, `GET /api/v1/symptoms`, `GET
    /api/v1/stats/overview`, `GET /api/v1/users/current`, `DELETE /api/v1/users/current`, `DELETE
    /api/v1/sessions/current`, the three onboarding steps, `POST
    /api/v1/users/current/data-wipe/validate`), and four on the deployment gate that turns the
    local-credential surface off in an SSO-only install (`POST /api/v1/users`, `POST
    /api/v1/sessions`, `POST /api/v1/password-resets`, `POST /api/v1/password-resets/redeem`). The
    `Forbidden` component's description now covers the second cause too.
  - Ten operations answer `400` on input they parse themselves and did not declare it: the three
    `/api/v1/exports/*` range parameters, `DELETE` and `HEAD /api/v1/days/{date}` on the path date,
    the `interface` / `tracking` / `reminders` settings bodies, `POST /api/v1/onboarding/complete`,
    and `POST /api/v1/sessions`.
  - `POST /api/v1/users/current/recovery-code` and `PUT /api/v1/users/current/2fa` share the
    per-account re-auth budget with the four endpoints that got their `429` in the previous
    release, and answer the same `429`.
  - The three step-up endpoints (`POST /api/v1/users/current/{password,data-wipe,deletion}/step-up`)
    answer `503` when the identity provider they must re-authenticate through is disabled or
    unreachable. A `ServiceUnavailable` response component carries it.
  - Three auth endpoints documented only their browser redirect and never their JSON success
    answer: `POST /api/v1/users` returns `201`, `POST /api/v1/password-resets/redeem` returns
    `200`, both with the new `NextStepResponse` shape (`ok`, `next_step`, `next_path`), and `POST
    /api/v1/password-resets` returns `200 {ok: true}`. A programmatic client had no documented
    success status on any of the three.

  A new test walks each `/api/v1` route's registered handler chain as Go AST and fails when the
  chain can reach a `fiber.Status*` the spec does not declare for that operation, so the class
  cannot reopen silently.
