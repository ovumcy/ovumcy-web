### Security

- **Breaking (API shape): `oidc_only` no longer issues recovery codes nothing can redeem.** Both
  places a recovery code is used — the password-reset flow and local sign-in — are closed under
  `OIDC_LOGIN_MODE=oidc_only`, yet setting a local password in Settings still minted a code and
  showed it as "store this offline". Under `oidc_only` the step-up now saves the password, keeps
  this device signed in and returns to Settings without minting a code, and
  `POST /api/v1/users/current/recovery-code` answers `403 recovery code unavailable` instead of
  rotating one. `hybrid` instances are unchanged.
