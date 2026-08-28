### Fixed

- **The per-account logout budget ran at the per-IP number.** Two limiters guard
  `DELETE /api/v1/sessions/current`: an edge limiter keyed on the client address (60 requests
  / 15 minutes) and an identity-keyed budget `AuthService` enforces per account, documented and
  coded as 20 failures / 15 minutes. Both read from the single `RATE_LIMIT_LOGOUT_MAX` /
  `RATE_LIMIT_LOGOUT_WINDOW` pair, so the account budget shipped at 60 — three times its
  documented size — and `services.DefaultLogoutAttemptsLimit` never took effect outside tests.
  The two now read from two pairs: the per-IP one keeps its name, the per-account one is
  `RATE_LIMIT_LOGOUT_ACCOUNT_MAX` / `RATE_LIMIT_LOGOUT_ACCOUNT_WINDOW`, defaulted from the
  service constants so the number in the security policy and the number in the code cannot
  drift. Operators who tuned `RATE_LIMIT_LOGOUT_*` to widen the per-IP budget for a household
  instance were widening the account budget with it; that side effect is gone, and the account
  budget is now tunable on its own. Regression: `TestLogoutBudgetsMoveIndependently`.
