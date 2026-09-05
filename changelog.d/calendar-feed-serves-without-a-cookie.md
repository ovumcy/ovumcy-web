### Security

- **The calendar feed no longer receives a cookie it was never supposed to carry.** `GET
  /calendar/feed/:token.ics` is a cookieless, capability-token route by design — a calendar client
  presents no session and no CSRF token, and `docs/SECURITY_INVARIANTS.md` already documented that
  it sets no `Set-Cookie` on any outcome. Two app-wide middlewares mounted ahead of the route did
  not honor that: on any GET lacking a matching cookie, the CSRF middleware minted and set its own
  token cookie, and the timezone middleware did the same whenever a caller named a zone, before
  either had any reason to run for this route at all. Neither ever validated anything here — a GET
  is a safe method the CSRF middleware was never going to check — so nothing about who may poll the
  feed changes; it now polls exactly as before, with one cookie fewer riding along for no reason.

  One consequence is deliberate: the feed no longer reads a timezone from the polling request at all.
  The calendar day it renders comes from the owner's saved timezone, as before; an owner whose timezone
  the app never captured now gets the server's zone instead of whatever zone the request happened to
  name, so the same subscribe URL renders the same day in every client.
