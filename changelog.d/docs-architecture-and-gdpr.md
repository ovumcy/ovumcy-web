### Internal

- **`docs/architecture.md` and `docs/gdpr.md` stop contradicting the security suite.** Accuracy
  pass from the 2026-08-28 documentation audit. `architecture.md`'s no-secret-in-transport bullet
  claimed recovery codes never appear in HTML and named the calendar-feed token as the single
  sanctioned carve-out — both halves false against the suite's own ledger; it now defers to
  `docs/SECURITY_INVARIANTS.md` and names the feed token (the one exception usable on its own),
  the TOTP enrollment seed (the second, narrower declared exception) and the two sanctioned
  one-time HTML reveals. `gdpr.md`: the `oidc_logout_states` parenthetical still described
  pre-031 NULL rows aging out by TTL — migration 033 purged them and `Save` refuses unattributed
  writes, so erasure covers the whole table immediately; the Art. 16 row no longer claims every
  profile field is editable (the email address has no in-app rectification control, and the
  operator column now says what fulfilling that request actually takes); the retention advice no
  longer prescribes "an operator cron job calling the same internal repository methods" — those
  are unreachable from outside the binary, so the doc now names what exists (scoped direct SQL,
  or a feature request) and scopes "no built-in scheduler" to retention, since the reminder
  scheduler exists; the restore-drill anecdote, which no dated record backs, is replaced by the
  mechanism statement; the `/privacy` paragraph defers the egress enumeration to the canonical
  statement in `docs/security/data-handling.md`. No behaviour changes.
