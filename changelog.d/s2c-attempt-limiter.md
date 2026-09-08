### Security

- **Flooding the attempt limiter no longer lifts a lockout or erases another budget.** The
  in-memory `AttemptLimiter` behind every per-account budget (login, recovery, TOTP, settings
  re-authentication) bounds its own memory: a periodic sweep drops entries whose window has lapsed,
  and above 1024 entries the coldest are evicted. Both bounds judged every entry by the budget of
  whichever failure triggered them. A login failure's sweep therefore erased a recovery lockout
  forty minutes before its own hour lapsed, and 1025 fresh login identities — the login form
  accepts any email — evicted the coldest entries first, which is exactly a locked-out victim,
  since the lockout is what stopped the attacker from touching that key; the same flood reached
  a partially spent TOTP budget. Every entry now carries the scope, limit and window it was
  recorded under: the sweep uses each entry's own window, the cap is enforced per scope, and an
  entry at its limit — an active lockout — is pinned until it expires. Regressions:
  `TestAttemptLimiterChurnDoesNotLiftActiveLockout`, `TestAttemptLimiterChurnDoesNotEraseTOTPBudget`,
  `TestAttemptLimiterSweepRespectsEachEntryWindow`.
- **A spent logout budget no longer keeps a session alive.** `DELETE /api/v1/sessions/current`
  checked the per-account logout budget before revoking the session, so once the budget was spent
  the owner could not sign out at all — on a shared device the session stayed usable until the
  window lapsed. The revoke and the cookie retraction now come first and the budget check second:
  a refused sign-out still ends the session and answers `429` (a browser form is sent to the
  sign-in page with the refusal as a flash) with the cookies already gone; what it withholds is
  the provider sign-out bridge. The budget is also keyed on the session being
  ended, qualified by its owner, instead of on the owner alone, so one owner's devices no longer
  spend each other's budget; its client bucket is `(address, session)` rather than the address by
  itself, which had run a second per-address cap of 20 under the documented per-IP row of 60 and
  refused a household's — or a test run's — 21st sign-out from one address. A logout is recorded
  against this budget only — never as a failure against, nor a reset of, the login, recovery or
  TOTP budgets. Regressions:
  `TestLogoutRevokesTheSessionBeforeTheBudgetIsChecked`, `TestLogoutBudgetIsKeyedByTheSession`,
  `TestLogoutAccountingNeverTouchesAnotherBudget`.
- **Every `RATE_LIMIT_*` setting has a ceiling.** A `*_MAX` accepted any positive integer and a
  `*_WINDOW` any duration of a second or more, so `RATE_LIMIT_LOGIN_MAX=100000000` switched the
  sign-in limiter off and a window of a year kept a refused address refused until restart. Each
  `*_MAX` now has a ceiling — 100 for login, registration and password reset (each request costs
  a bcrypt compare), 600 for the per-IP logout row, 200 for the per-session logout budget, 3000
  for the API catch-all and 120 for the calendar feed — and each `*_WINDOW` must lie between one
  second and one day. A value outside its range is logged at boot and the default is used, as an
  unparseable value already was. Operators who had set a `*_MAX` above its ceiling get the default
  from this release and can widen the budget by shortening the window instead; the e2e harness
  now does exactly that. The bcrypt cost stays the load-bearing limit and is unchanged.
  Regressions: `TestRateLimitMaxSettingsHaveCeilings`, `TestRateLimitWindowSettingsHaveCeilings`.
