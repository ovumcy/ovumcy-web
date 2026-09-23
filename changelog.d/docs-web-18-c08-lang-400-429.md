### Fixed

- **The API reference now lists the language switch's `400` and `429`.** `docs/openapi.yaml`
  documented only the success answers of `POST /lang`. It now also lists the `400` that a missing
  or blank `lang` gets, with the shared `bad_request` error envelope, and the `429` from the
  route's own per-IP rate limit, with `Retry-After`. Each one says which callers get the JSON
  envelope and which get an HTML fragment instead. The `lang` field no longer claims a closed
  list of codes: the server falls back to the default language for a code it does not ship. The
  `303` description now says that the redirect goes to the form's same-origin `next` path, not
  to the referer. A guard test now finds every rate limiter mounted on a documented path outside
  `/api/v1`, so the spec cannot leave out a `429` there. The server's behaviour is unchanged.
