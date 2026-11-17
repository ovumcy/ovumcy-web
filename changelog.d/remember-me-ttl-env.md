### Added

- **Operators can set how long "Remember me" keeps owners signed in.** `REMEMBER_ME_TTL_DAYS` (8 to 365,
  default 30) sets the lifetime of a remembered sign-in, for instances whose owners open the app less
  than once a month. The sign-in checkbox names the configured number of days. Lowering the value also
  ends longer sessions already issued, and a value outside the range refuses to start the app. A longer
  value keeps a stolen session cookie usable for longer; a password or 2FA change still signs out every
  other device at once.
