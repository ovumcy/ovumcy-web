### Security

- **The calendar feed no longer receives a cookie it was never supposed to carry, on any spelling of
  its URL.** `GET /calendar/feed/:token.ics` is a cookieless, capability-token route by design — a
  calendar client presents no session and no CSRF token, and `docs/SECURITY_INVARIANTS.md` already
  documented that it sets no `Set-Cookie` on any outcome. Two app-wide middlewares mounted ahead of
  the route did not honor that, and did not honor it identically to how fiber itself routes: on a
  GET or HEAD lacking a matching cookie, the CSRF middleware minted and set its own token cookie, and
  the timezone middleware did the same whenever a caller named a zone, before either had any reason
  to run for this route at all — reachable through a case-folded or trailing-slash spelling of the
  URL (the app does not require an exact case or a bare path), not only the canonical one, and on
  HEAD as well as GET. Neither ever validated anything here — GET and HEAD are safe methods the CSRF
  middleware was never going to check — so nothing about who may poll the feed changes; it now polls
  exactly as before, with one cookie fewer riding along for no reason.

  The same fix also stops a path that merely starts with the feed's prefix but names no real token —
  a bare `/calendar/feed/`, or a nested path segment — from being swept into the feed's own exclusion:
  such a path already answered the ordinary "page not found" screen, but was silently missing its
  language and its CSRF token. It now gets both, like any other page.

  One consequence is deliberate: the feed no longer reads a timezone from the polling request at all.
  The calendar day it renders comes from the owner's saved timezone, as before; an owner whose timezone
  the app never captured now gets the server's zone instead of whatever zone the request happened to
  name, so the same subscribe URL renders the same day in every client.
