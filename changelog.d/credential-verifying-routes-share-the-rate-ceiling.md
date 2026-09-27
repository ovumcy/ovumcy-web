### Security

- **The 2FA login challenge and the password-reset redeem now sit under their own edge rate
  ceiling, like every other credential-verifying route.** Both endpoints verify a credential — a
  TOTP code, and a signed reset token plus a bcrypt hash of the new password — but drew only on
  the general `/api` catch-all budget (300 requests/min), with no ceiling of their own. The redeem
  route in particular carries no service-level attempt budget behind it at all: every well-formed
  request pays a bcrypt hash, unbounded except by the catch-all. Both routes now share the
  credential rate ceiling (`RATE_LIMIT_TOTP_CHALLENGE_MAX`/`WINDOW`,
  `RATE_LIMIT_PASSWORD_RESET_REDEEM_MAX`/`WINDOW`, defaulting to 8/15min like login) through the
  same `getCredentialRateLimit` check as login, registration and forgot-password.
