none

Docs-only fix: a sentence in `docs/security/known-disclosures.md` pointed at a
file that does not exist in this repository, so the reader had nothing to open.
It now names the code path the sentence is about — `CompleteOIDCLinkConfirmation`
in `internal/api/handlers_auth_oidc_link_confirm.go`, where the link-confirm
submission is gated on a TOTP code.
