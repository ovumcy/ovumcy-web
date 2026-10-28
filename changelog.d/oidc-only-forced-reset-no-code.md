### Security

- **Breaking (API shape): the last two `oidc_only` paths stop issuing recovery codes nothing can
  redeem.** Under `OIDC_LOGIN_MODE=oidc_only`, completing an operator-forced password change after
  an OIDC sign-in still minted a recovery code and showed it on `/recovery-code`, and
  `ovumcy users create` still minted one. The forced change now saves the password and signs the
  user in with no code — `POST /api/v1/password-resets/redeem` answers `{"ok": true}` instead of
  pointing at `/recovery-code` — and `ovumcy users create` prints that no code was issued, even
  with `--show-recovery-code`. `hybrid` instances are unchanged.
