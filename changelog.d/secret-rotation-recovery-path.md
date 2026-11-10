### Fixed

- **Docs: what a `SECRET_KEY` rotation does to 2FA accounts.** Several documents said a 2FA
  challenge answers HTTP 500 after the key changes, and that recovery needs the password plus the
  recovery code. Neither is true any more. After the correct password, sign-in sends such an
  account to the forced password reset; the owner then re-enrols TOTP from Settings. The operator
  CLI is needed only when the password is forgotten, or with `--full` for an OIDC-only account
  that has TOTP. Updated: `docs/security/cryptography.md`, `docs/self-hosted.md`, `docs/gdpr.md`,
  `docs/security/known-disclosures.md`.
