### Security

- **An OIDC sign-in under closed registration answers like any other refused sign-in.** This is
  defence in depth, not a fix for a leak in a running instance: startup refuses
  `OIDC_AUTO_PROVISION=true` unless `REGISTRATION_MODE=open`, so no deployment could reach this
  path. If that combination were ever allowed, an unlinked identity whose email no account held
  would have been refused with "sso temporarily unavailable", and one whose email an account held
  with "sso sign-in unavailable". That difference would tell anyone with an identity at the
  configured provider whether an email has an account. Both now get "sso sign-in unavailable", and
  the audit line records the refusal as `cause=registration_closed`.
