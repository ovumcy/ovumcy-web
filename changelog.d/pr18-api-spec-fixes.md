### Fixed

- **`docs/openapi.yaml` now matches three places where the server's actual behavior had drifted
  from the published contract.** All three were pre-existing since v1.9.2; none is a v2.0.0
  breaking change.
  - Four settings endpoints that share the per-account re-auth budget — `DELETE
    /api/v1/users/current`, `PUT /api/v1/users/current/password`, `POST
    /api/v1/users/current/data-wipe/validate`, `POST /api/v1/users/current/data-wipe` — can answer
    `429` once the budget is spent, but the spec never declared it. All four now do.
  - `ForgotPasswordRequest` (`POST /api/v1/password-resets`) required `recovery_code` and
    `password` even though the handler is a two-step flow: step 1 sends `email` alone and gets
    `next_step: recovery_code` back, without starting anything. Only `email` is schema-required
    now, and the schema documents both steps. The endpoint's declared `401` was also unreachable —
    `mapPasswordRecoveryStartError` has no arm that returns it, every step-2 rejection is a uniform
    `400` — so the response now documents that instead.
  - `docs/openapi.yaml` declares `openapi: 3.1.0` but used the OpenAPI-3.0-only `nullable: true`
    keyword on nine properties (`DayPayload.bbt`, `DailyLog.bbt`, `Symptom.archived_at`, five
    `StatsOverview` projection fields, `ExportJSONEntry.bbt`), which fails structural validation
    under 3.1's JSON Schema base. Each now uses the 3.1 form, `type: [<type>, "null"]`.
    `npx @redocly/cli lint docs/openapi.yaml` is green.
