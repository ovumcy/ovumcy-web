### Security

- **Linking an OIDC identity no longer outlives a revocation that lands while the link is being
  confirmed.** The link revoked the account's sessions by incrementing whatever version it found, so
  a TOTP re-enrollment or password change committed between the factor checks and the link write
  was folded into the link's own bump, and the session issued afterwards carried the version that
  revocation produced. The link now revokes only from the version its factors were verified
  against: if the account's sessions were revoked in between, nothing is linked and no session is
  issued. From Settings the owner signs in again and repeats the link; `ovumcy link-oidc-identity`
  reports the change and asks to be run again.
