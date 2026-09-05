none

Test hardening: the calendar feed's no-oracle wrong-verifier case now runs against a still-armed subscription instead of one already revoked. The restored-backup and pre-032-migration regressions now check that same armed-feed precondition (no Set-Cookie, a calendar body), and the api and services tests now build every selector/verifier pair through SplitCalendarFeedToken instead of a hand-copied offset; still no user-visible change.
