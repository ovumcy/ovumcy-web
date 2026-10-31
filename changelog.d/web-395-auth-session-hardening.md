### Security

- **Account deletion no longer outruns a sign-out everywhere.** Deleting the account from Settings
  now runs only from the session version its re-authentication was checked against, as clearing
  data already did: a password change or a sign-out everywhere landing in between leaves the
  account in place and signs the device out. The operator's `ovumcy users delete` is unchanged.
- **Behind a proxy that forwards `X-Forwarded-For`, a stranger can no longer lock a victim's
  address out of sign-in.** The per-client sign-in, 2FA, recovery and re-authentication budgets
  now key on the same rightmost untrusted hop the edge rate limiters use, not on the leftmost
  entry the client writes itself.
- **A link on another site can no longer cancel a step-up in progress.** Opening the SSO sign-in
  start from another site leaves an in-flight step-up to its own ten-minute expiry; only a sign-in
  started from Ovumcy itself drops an abandoned one.

### Fixed

- Password recovery accepts the account password with surrounding spaces, as sign-in does.
- `ovumcy reset-password` reports a failure, not success, when the account was deleted between
  its lookup and the reset.
