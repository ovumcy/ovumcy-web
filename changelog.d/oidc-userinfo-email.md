### Security

- **The provider's UserInfo address must belong to the configured provider.** Ovumcy now reads the
  `userinfo_endpoint` from the provider's discovery document and holds it to the rule the token,
  key and sign-in page addresses already follow: same scheme, host and port as `OIDC_ISSUER_URL`.
  An address on another host, without https, or one that resolves to the machine Ovumcy runs on is
  dropped and never requested. Sign-in is not refused over it, so a provider that advertises such an
  address keeps signing in as before.
