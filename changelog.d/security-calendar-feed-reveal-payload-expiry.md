### Security

- **The calendar-feed reveal now expires on the server, not just in the browser.** The sealed
  `ovumcy_calendar_feed` cookie, which carries the full `.ics` subscribe URL for its shown-once
  reveal, carried no expiry of its own, so its twenty-minute lifetime lived only in the
  `Set-Cookie` `Expires` attribute — a hint the client is free to ignore. A browser that kept the
  sealed value and never spent the reveal could present it back on the owner's own session and be
  shown the subscribe URL — a bearer capability token — months later, until the token was rotated,
  the feed revoked, or `SECRET_KEY` changed. The payload now carries the moment it is honored
  until, and the reveal page verifies that against the clock before it displays anything.

  This is the bound on a reveal that never happened; the server-side consumption mark
  (`users.calendar_feed_revealed_at`) remains what refuses a reveal that did.

  A cookie minted before this change carries no expiry and is **refused**: an absent bound is
  invalid input, not permission to display. So is a reveal minted in the twenty minutes before the
  upgrade. The refusal costs the display only — the feed itself keeps working, the owner keeps
  their session and lands on `/settings`, where an absent reveal cookie lands, and a rotate mints a
  fresh URL and re-arms the reveal in the same write. A cookie refused for either reason is
  retracted in the same response, and a visit that revealed nothing logs no reveal.
