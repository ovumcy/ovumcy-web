### Security

- **Linking a sign-in identity now asks for the account password, and a link can be removed.**
  Linking an SSO identity from Settings sent the owner to the identity provider to sign in again,
  which proved control of the identity being linked — but not of the account. Someone holding a
  stolen session could link their own provider account and keep a permanent way back in. The link
  now also requires the account's current password, and an account without one sets a password
  first. Linking an identity, and the new `DELETE /api/v1/users/current/oidc/identities/{id}`
  that removes one, both sign out every other session; `ovumcy link-oidc-identity` now signs
  out every session of the account it links. Removing the last identity is refused unless
  password sign-in remains available on the instance, so nobody can lock themselves out — and
  the check runs inside the removal itself, so two removals sent at once cannot take away both
  of an account's last two identities.

- **An identity token without a subject is refused.** Sign-in and linking key every identity on
  the provider's issuer and subject; a token with a blank subject is now refused when it is read,
  and a blank key is never looked up.

- **The post-logout address is checked as first-party each time it is used.** The address the
  provider sends the browser to after sign-out must share the origin of the configured OIDC
  callback; anything else ends the sign-out locally instead of handing the provider a third-party
  destination.
