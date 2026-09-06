### Fixed

- **A `HEAD` request now reaches the page or endpoint it names, instead of the 404.** Every address
  the app serves on `GET` — the sign-in page, the dashboard, `/healthz` and `/readyz`, the `/api/v1`
  reads, the calendar feed's `.ics` URL — answered `HEAD` with "not found", because the framework
  adds its `HEAD` counterparts only at startup and they landed behind the catch-all that answers
  unknown addresses. Uptime checks, link checkers, reverse-proxy probes and calendar clients that
  ask for headers before downloading all saw a dead site. They now get the same status and headers
  the matching `GET` returns, with no body, and an address that really does not exist still answers
  404 on either method. Pages that show a secret once, or mint one-time auth material a redirect
  would carry — the recovery code, the freshly minted subscribe URL, the registration hand-off,
  starting sign-in with a provider — deliberately keep answering `HEAD` with 404: a headers-only
  request there would spend the single use with nobody able to see or follow it. The one-time
  notice a page shows after a redirect (a sign-in error, a forgotten-password prefill) is left
  untouched by `HEAD` too, so the browser's own next visit is still the first to read it.
