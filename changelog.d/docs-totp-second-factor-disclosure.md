### Internal

- **The security docs now declare both password-reset paths that issue a session without a TOTP
  challenge.** `docs/security/known-disclosures.md` carried no TOTP entry at all, while two routes
  end at the same handler that goes from a completed reset straight to an auth cookie. They are
  written down separately, because they are different decisions. The **operator-forced reset**
  outranks the second factor by design — it is the way back in for an owner whose `users.totp_secret`
  stopped decrypting after a `SECRET_KEY` rotation, which is exactly what the rotation runbooks
  instruct, so making TOTP win would lock out precisely those accounts; on the OIDC link-confirm
  path, which gates a TOTP-enabled target on its own code before either branch, what a reversal
  would drop is the routing to `/reset-password`, not the second factor. The **recovery-code
  reset** is stated as intended design: the code stands in for the second factor and never for the
  password, so the route costs two secrets, and the entry names the property that bounds it — a
  recovery code outlives routine password changes until it is redeemed or regenerated. Both paths
  still bump `auth_session_version` in the same write that rewrites the password hash. The entry
  states the forced reset as the one sanctioned exception to session-issuance parity rather than
  as parity preserved, and `SECURITY.md` now indexes it alongside the other accepted signals.
  Documentation only; no behaviour changes, and the precedence is already pinned by test at the
  service and at both of its callers.
