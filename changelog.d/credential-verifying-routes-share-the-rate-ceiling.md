### Security

- **The 2FA login challenge and the password-reset redeem now sit under their own edge rate
  ceiling, like every other credential-verifying route.** Both endpoints verify a credential — a
  TOTP code, and a signed reset token — but drew only on the general `/api` catch-all budget (300
  requests/min), with no ceiling of their own. Neither route has a service-level attempt budget
  behind it, so the catch-all was the only bound on guessing TOTP codes or reset tokens against
  them. Both routes now share the credential rate ceiling (`RATE_LIMIT_TOTP_CHALLENGE_MAX`/`WINDOW`,
  `RATE_LIMIT_PASSWORD_RESET_REDEEM_MAX`/`WINDOW`, defaulting to 8/15min like login) through the
  same `getCredentialRateLimit` check as login, registration and forgot-password.
