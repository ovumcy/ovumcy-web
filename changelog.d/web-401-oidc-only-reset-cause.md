### Security

- **The `reset_required` audit line now names its cause** (audit stream on with
  `AUDIT_LOG_ENABLED=true`; it is off by default). `auth.login` and
  `auth.oidc_callback` record `cause=must_change_password` for an account an operator flagged
  with `ovumcy reset-password`, and `cause=totp_unverifiable` for one whose stored TOTP secret
  no longer decrypts. The second is what a `SECRET_KEY` rotation leaves behind: every
  TOTP-enrolled account then signs in with its password alone and is sent to a forced password
  change instead of a 2FA challenge no code can answer. That trade-off is deliberate — refusing
  those sign-ins would lock every such owner out after a rotation — but until now nothing told
  the operator it was happening. After a rotation, expect `totp_unverifiable` lines and have the
  affected owners re-enrol TOTP.

### Fixed

- **The recovery path for an OIDC-only account with TOTP is documented and tested.** An account
  created through the identity provider, with no local password, cannot be sent to the forced
  reset when its TOTP stops decrypting: the callback lands back on `/login` and the audit line,
  when the stream is on, records `cause=no_local_password`. The way back is the operator command
  `ovumcy reset-password --full <email>`, which clears the dead TOTP enrollment and the links and
  sets a temporary password; the owner signs in with it, changes it and relinks the provider. A
  code comment that promised a self-service escape for this account is corrected, and
  `docs/self-hosted.md` no longer claims such an account recovers on its own.
