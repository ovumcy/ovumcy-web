### Fixed

- **OpenAPI: logout rate limit and GET security declarations.** `docs/openapi.yaml` stated the
  `DELETE /api/v1/sessions/current` per-account limit as 60 / 15 min; the per-account default is
  20 / 15 min and 60 / 15 min is the per-IP default, and the description now names both. The
  `GET` operations that need a session (days, symptoms, stats overview, the three exports and the
  current user) no longer declare a CSRF token, which the server never validates on a safe method;
  they require only the session cookie. State-changing operations are unchanged.
