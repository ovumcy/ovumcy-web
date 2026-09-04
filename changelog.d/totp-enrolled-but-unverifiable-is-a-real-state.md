### Security

- **An account whose TOTP secret can no longer be decrypted (a `SECRET_KEY` rotation) now reaches the
  operator-reset flow automatically instead of being stranded at a 2FA challenge no code could ever
  pass.** Every session-issuing path — local login, OIDC sign-in, and OIDC account-link confirmation —
  now derives TOTP verifiability by attempting the decryption instead of trusting the `totp_enabled`
  column alone, and routes an enrolled-but-unverifiable account to the same forced-reset escape hatch
  an operator-flagged account already used, without waiting for that operator action.
