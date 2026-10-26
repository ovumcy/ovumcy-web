### Fixed

- An account without a local password (SSO-provisioned, password not yet set) opening
  Settings → Two-factor authentication is told that enabling or disabling 2FA is confirmed with the
  account password and linked to the local-password setup, instead of being shown a QR code and a
  form every submit of which was refused as "invalid password". No enrollment seed is minted for
  that account until it can confirm one.
- On an `OIDC_LOGIN_MODE=oidc_only` instance the account settings no longer invite setting a local
  password "for recovery codes", nor offer to regenerate one: both consumers of a recovery code
  (the public recovery flow and local sign-in) are closed in that mode, so the page names the
  identity provider or the operator as the recovery path. The local password still confirms safety
  actions and two-factor setup.
