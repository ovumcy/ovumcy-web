### Security

- **Breaking (auth behavior): signing in through OIDC to an account that already has TOTP enabled
  now requires the second factor, the same way local sign-in does.** The OIDC callback resolved an
  already-linked `(issuer, subject)` straight to a session — `CompleteOIDCLogin` had no TOTP check
  anywhere on that path, unlike the local login route, which redirects a TOTP-enabled account to
  `/auth/2fa` before ever issuing a cookie. `OIDCLoginService.Authenticate` now reports
  `RequiresTOTP` the same way `LoginService.Authenticate` reports it for local login, and the OIDC
  callback gates on it before `setAuthCookie`, ordered the same way: an account also carrying
  `must_change_password` still goes to the forced-reset flow first, never to the 2FA challenge —
  the sanctioned escape hatch for a TOTP secret that can no longer be decrypted after a `SECRET_KEY`
  rotation is unaffected. Operators whose SSO users have TOTP enabled will see those accounts
  challenged for a code on their next OIDC sign-in where they were not before.
