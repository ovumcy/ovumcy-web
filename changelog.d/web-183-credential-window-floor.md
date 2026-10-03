### Changed

- **The login, registration, forgot-password, 2FA challenge and password-reset redeem rate-limit
  windows now start at one minute.** `RATE_LIMIT_{LOGIN,REGISTER,FORGOT_PASSWORD,TOTP_CHALLENGE,PASSWORD_RESET_REDEEM}_WINDOW`
  used to accept anything from one second; a value below `1m` is now out of range like any other
  out-of-range `RATE_LIMIT_*` value. It is logged once at boot and that window falls back to its
  default (15 minutes; one hour for forgot-password). The `*_MAX` set beside it is kept, and the
  per-minute rate ceiling on the pair is unchanged. An operator who ran one of these five windows
  at seconds gets the default window from this release; set `1m` or longer instead. Every other
  window (logout, API, calendar page and feed) keeps its one-second floor, and both logout rows
  are unchanged. Regression: `TestCredentialRateLimitWindowsHaveAOneMinuteFloor`.
