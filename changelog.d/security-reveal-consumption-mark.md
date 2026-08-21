### Security

- **A shown-once secret is now consumed on the server, so replaying the reveal cookie shows
  nothing.** Both surfaces that reveal a secret exactly once — the recovery code (its page and the
  inline block after sign-up) and the calendar-feed subscribe URL — enforced that by writing a
  cleared cookie into the reveal response and nothing else. Clearing a cookie asks a browser to
  forget a value it was handed, and binds nothing that kept it: a client holding the sealed value
  could present it again on the owner's own session and be shown the secret a second time. For the
  feed URL there was no bound at all — the window closed only when the token was rotated, the feed
  revoked, or `SECRET_KEY` changed.

  Each reveal now claims a per-account mark on the `users` row (`recovery_code_revealed_at`,
  `calendar_feed_revealed_at`, migration 036) with a compare-and-set before it renders anything. A
  replay, a reload and a second tab all lose that race and are refused; the refusal retracts the
  cookie in the same response, costs the display only, and lands the owner where an absent cookie
  lands. A refused feed reveal also records no second reveal in the audit stream, so an operator
  counting reveals counts disclosures.

  Re-issuing the secret re-arms exactly one reveal: every write that mints a fresh recovery code or
  a fresh feed token clears the matching mark in the same statement, so generate, rotate,
  regenerate and reset all keep working as before. Because the mark is a database row rather than
  process state, it survives a restart, a second replica and a backup restore.

  **Operators:** migration 036 adds two nullable timestamp columns to `users` on both SQLite and
  PostgreSQL. Nothing already revealed is retroactively refused, and no data is lost in either
  direction — reverting the code leaves the columns in place, unread.
