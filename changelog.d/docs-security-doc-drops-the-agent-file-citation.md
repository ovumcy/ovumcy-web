### Internal

- **A pointer in `docs/security/known-disclosures.md` named a file that is not in this
  repository.** The sentence about OIDC link-confirm requiring a TOTP code in the same
  submission ended in a parenthesis a reader had nothing to open. It now names the code
  path the sentence is about — `CompleteOIDCLinkConfirmation` in
  `internal/api/handlers_auth_oidc_link_confirm.go`, where the submission is gated on a
  TOTP code whenever the target account's second factor is verifiable. The claim is
  unchanged; only where the reader is sent to check it.
