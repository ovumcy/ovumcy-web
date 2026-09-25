### Fixed

- **Every rate limiter guarding `DELETE /api/v1/sessions/current` now answers `429`, never a `303`
  redirect to the login page.** A plain client (no JSON `Accept`, no `HX-Request`) refused by
  either the per-IP logout row or the app-wide `/api` limiter behind it used to be redirected to
  `/login` instead of getting `429` with `Retry-After`, because the error-mapping switch had no
  case for the endpoint and fell through to its forward-compat default. JSON and HTMX clients
  already got the correct answer for both limiters and are unchanged, including the JSON
  `error_detail.target`, which stays `auth_form`.
