### Fixed

- **Enabling or disabling 2FA now answers a JSON client with a body.** `PUT` and
  `DELETE /api/v1/users/current/2fa` content-negotiated on `HX-Request` alone: an HTMX caller got
  the inline status markup and *everyone else* — an API client sending `Accept: application/json`
  included — was redirected to `/settings`, a response carrying nothing a non-browser client can
  read, and one `docs/openapi.yaml` never declared. Both now take the JSON arm their sibling
  settings mutations (`data-wipe`, `DELETE /users/current`) already take and return the declared
  `{"ok": true}` alongside the re-issued session cookie; the browser redirect is unchanged, and so
  is the HTMX markup the settings page renders. The spec publishes the `200` body and states the
  `303` as the non-JSON fallback.
  - The contract guards did not miss this for want of walking the route table; they exclude `303`
    on purpose. `crossCuttingStatus` in the per-operation reachability test suppresses it because
    the shared error responder redirects any `/api/v1/users/current` path that does not accept
    JSON, which would make the status look emittable from every operation. Lifting that exclusion
    turns 20 operations red, and 18 of them falsely: those handlers return JSON from an
    `if acceptsJSON(c)` arm before ever reaching the redirect, which the guard's HTML-surface
    narrowing reads only as an `if` guard, never as an early return. Teaching it to see redirects
    therefore needs flow sensitivity plus a per-operation verdict on each survivor, and is tracked
    as its own change.
