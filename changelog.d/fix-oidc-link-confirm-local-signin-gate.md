### Security

- **Confirming an OIDC identity link no longer hands out a session on an oidc-only instance.**
  `/auth/oidc/link/confirm` checks a local password to confirm a pre-existing account before
  linking it to an OIDC identity — the same anti-takeover step `Login`, `ForgotPassword`, and the
  auth pages already refuse to run once an operator turns `OIDC_LOGIN_MODE=oidc_only` on. This route
  was the one place that check still ran anyway, and a correct password still ended in a live
  session. The identity link itself stays reachable — it is the only way a pre-existing
  local-password account ever links to OIDC once local sign-in is off, and refusing it would strand
  that account for good — but the link no longer signs the browser in directly: the account signs in
  on its next OIDC round-trip instead, through the identity it just linked.
