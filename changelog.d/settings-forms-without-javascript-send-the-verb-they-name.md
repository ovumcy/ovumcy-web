### Fixed

- **Settings forms submitted without JavaScript now do what their button says.** Six settings
  forms send PUT or DELETE through htmx but fall back to a plain POST without JavaScript: disable
  and enable 2FA, change password, unlink an OIDC identity, hide a symptom, and revoke the
  calendar feed. That POST got 405 on five of them. On the calendar feed it hit the generate route,
  so "Revoke" issued a fresh feed URL instead. Each form now carries a hidden `_method` field, and
  the server routes a urlencoded form POST as the PUT, PATCH or DELETE it names. The override runs
  ahead of the rate limiters and CSRF, so both see the verb that actually runs and the CSRF token
  stays mandatory. JSON bodies, the query string and every verb other than POST are never
  overridden. Any other `_method` value, an empty one or a repeated one is refused with 400.
