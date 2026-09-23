### Fixed

- **The API reference now lists the language switch's refusals.** `docs/openapi.yaml` documented
  only the success answers of `POST /lang`. It now also lists the `400` that a missing or blank
  `lang` gets, with the shared `bad_request` error envelope, and the `429` from the route's own
  per-IP rate limit, with `Retry-After` and `retry_after_seconds`. The `303` description now says
  that the redirect goes to the form's same-origin `next` path, not to the referer. A guard
  test now finds every rate limiter mounted on a documented path outside `/api/v1`, so the spec
  cannot leave out a `429` there. The server's behaviour is unchanged.
