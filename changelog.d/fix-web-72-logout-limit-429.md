### Fixed

- **The per-IP logout rate limiter now answers 429 for every client format, never a 303 redirect
  to the login page.** `DELETE /api/v1/sessions/current` refused by the edge budget used to
  redirect a plain (no JSON `Accept`, no `HX-Request`) client to `/login` instead of answering
  `429` with `Retry-After`, because the limiter's error mapping treated the endpoint as a browser
  form even though no `<form>` can submit `DELETE`. JSON and HTMX clients already got the correct
  answer and are unchanged.
