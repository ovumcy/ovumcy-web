### Fixed

- **SSO sign-in works with providers that send the email only from UserInfo.** Ovumcy read the
  email address and its verification flag from the ID token alone. OpenID Connect delivers those
  claims from the provider's UserInfo endpoint in the authorization code flow, and a provider may
  leave them out of the ID token entirely, as Authelia 4.39 and later do by default. Every first
  sign-in through such a provider was refused as an unavailable account, whether it would have
  created an account or matched an existing one. When the ID token carries no verified address, a
  first sign-in now reads the address and its verification flag from the UserInfo endpoint. An ID
  token that does carry one is used as before, with no UserInfo request, and a sign-in through an
  already linked identity never asks UserInfo either.

### Security

- **The provider's UserInfo address must belong to the configured provider.** Ovumcy now reads the
  `userinfo_endpoint` from the provider's discovery document and holds it to the rule the token,
  key and sign-in page addresses already follow: same scheme, host and port as `OIDC_ISSUER_URL`.
  An address on another host, without https, or one that names the machine Ovumcy runs on is
  dropped and never requested. Sign-in is not refused over it, so a provider that advertises such an
  address keeps signing in as before.

- **A UserInfo answer is used only for the sign-in it belongs to.** Ovumcy uses the email from
  UserInfo only when the answer names the same subject as the ID token, and, when it says which
  issuer sent it and which client it is for, only when those are the configured issuer and Ovumcy.
  Any other answer fails the sign-in, and so does a signed answer whose `exp` has already passed.
  The address and its verification flag always come from the same answer.

- **SSO sign-in no longer reveals whether an email holds an account.** An unlinked SSO sign-in
  whose email matched an existing account was refused with a different message from one whose
  email matched no account, which told anyone able to assert that email at the provider that the
  account existed. Both now show the same message; the account holder still signs in with their
  existing method and links the provider from Settings.
