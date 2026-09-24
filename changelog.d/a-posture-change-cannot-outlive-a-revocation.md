### Security

- **A security change made from Settings no longer outlives a revocation that lands while it is
  being made.** Changing the password, enrolling a local password, regenerating the recovery code,
  turning two-factor sign-in on or off, clearing data, and linking or unlinking an OIDC identity
  each revoked the account's sessions by incrementing whatever version they found, so a sign-out
  everywhere or another device's password change committed after the request was authenticated
  was folded into the change's own bump, and the change went through on a session that had
  already been revoked. Each now revokes only from the version of the session that made the
  request: if the account's sessions were revoked in between, nothing is changed and this device
  is signed out. A link or unlink that a revocation follows is kept, and this device is signed out
  rather than handed a session that would outlive that revocation. The owner signs in again and
  repeats the change.
