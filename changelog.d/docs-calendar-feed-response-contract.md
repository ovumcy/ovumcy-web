### Internal

- **The security documents now state the calendar feed's real response contract.**
  `docs/SECURITY_INVARIANTS.md`, the `SECURITY.md` matrix and `docs/openapi.yaml` all claimed the
  feed answers its rejections as a bare status with no body — a body-less `404`, and a bare `429`
  carrying only `Retry-After`. The code never matched the first half in its detail — `c.SendStatus`
  has always filled the fixed status text, so the `404` is a cause-free `Not Found` body, identical
  for a malformed token, an unknown selector, a wrong verifier and a disabled feed (pinned by
  `TestCalendarFeedReturnsBare404WithoutOracleForBadTokens`) — and deliberately stopped matching
  the second when the feed's `429` joined the shared limiter path like every other route
  (`RespondCalendarFeedRateLimited`, required by the rate-limit envelope regressions). The three
  documents, the `envelopeExemptRoutes` reason string and the test comments now describe that
  contract: the envelope exemption covers the feed's not-found answer, never the route wholesale.
  The no-oracle property itself was never wrong and is unchanged. In the same pass:
  `SECURITY_INVARIANTS.md`'s opening quantifier now scopes the test-enforcement promise to
  test-enforceable entries (the matrix's own carve-out); `SECURITY.md` states the rolling support
  policy explicitly (a fix ships in the next release, never as a backport), scopes the Art. 15 row
  to the day-level export `docs/gdpr.md` describes, enumerates what Art. 16 actually lets an owner
  edit (not the email address), qualifies "egress paths are exactly two" to standing third-party
  paths, and uses a `vX.Y.Z` placeholder in the verification examples. No behaviour changes.
