### Security

- **The calendar feed no longer receives a cookie it was never supposed to carry, on any spelling
  of its URL.** `GET /calendar/feed/:token.ics` is a cookieless, capability-token route by design
  — a calendar client presents no session and no CSRF token, and `docs/SECURITY_INVARIANTS.md`
  already required the identical `404` it answers an unknown token with to carry no `Set-Cookie`;
  that requirement now reads on every outcome of the route, which is what this fix makes true.
  Every GET or HEAD to the feed's own URL — whatever its case or trailing slash — used to get one
  anyway: an `ovumcy_csrf` cookie
  handing out a fresh CSRF token, and, whenever the request named a timezone, an `ovumcy_tz`
  cookie recording it, both set before the request had any reason to carry either. Nothing about
  who may poll the feed changes — a calendar client was never going to present a CSRF token or
  read a cookie back — so it now polls exactly as before, with two cookies fewer riding along for
  no reason.

  The exclusion is scoped precisely to the feed's own concrete URL: a path that merely starts with
  its prefix but names no real token — a bare `/calendar/feed/`, or a nested path segment — keeps
  answering the ordinary "page not found" screen with its language and CSRF token exactly as before,
  whatever its own case or trailing slash.

  One consequence is deliberate: the feed no longer reads a timezone from the polling request at all.
  The calendar day it renders comes from the owner's saved timezone, as before; an owner whose timezone
  the app never captured now gets the server's zone instead of whatever zone the request happened to
  name, so the same subscribe URL renders the same day in every client.
