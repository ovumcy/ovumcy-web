### Security

- **An OIDC sign-in under closed registration no longer reveals whether an email has an account.**
  With `OIDC_AUTO_PROVISION=true` and `REGISTRATION_MODE=closed`, an unlinked identity whose email
  no account held was refused with "sso temporarily unavailable", while one whose email an account
  held got "sso sign-in unavailable", so anyone with an identity at the configured provider could
  tell the two apart. Both now get "sso sign-in unavailable".
