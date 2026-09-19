### Security

- **An OIDC step-up now requires the provider's `auth_time`; `iat` no longer counts as proof of a
  fresh sign-in.** Clearing data, deleting the account, enrolling a local password and linking an
  identity from Settings send the provider `prompt=login` and `max_age=0` and then check that the
  sign-in behind the returned ID token is under five minutes old. When the token carried no
  `auth_time`, that check fell back to `iat` — the time the token was minted, not the time the user
  signed in — so a provider answering from a cached session could pass it with a fresh token over a
  stale sign-in. A token without `auth_time` is now refused, and there is no setting to relax this.
  With a provider that omits `auth_time` under `max_age=0`, an OIDC-only account can no longer
  clear its data, delete itself or enrol a local password, and no account can link another identity
  from Settings; ordinary SSO sign-in is unaffected, and the operator commands
  `ovumcy link-oidc-identity` and `ovumcy users delete` still work. See *Provider requirement for
  step-up* in [docs/oidc.md](docs/oidc.md).
