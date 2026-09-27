### Security

- **Settings actions that ask for the current password no longer say outright that an account has
  no local password set.** Clear-data, delete account, enroll two-factor sign-in, change password,
  regenerate the recovery code, and link or unlink an OIDC identity used to answer "local password
  required" (403) for that account state but "invalid password" (401) for a wrong password on an
  account that has one — two differently-shaped refusals a live, unauthenticated-for-this-purpose
  session could use to learn whether an account has a local password at all. All of them now answer
  identically: 401, the same error key, the same form target. The account-state distinction is
  still written to the security log for the owner's own audit trail, via a new `reauth_cause`
  field (server log only, never the response) so an operator can still tell "wrong password"
  apart from "no local password" now that the caller-visible refusal is byte-identical. Removing
  an account's last sign-in method and the OIDC-only password-change redirect keep their own,
  unrelated 403 answers, since neither compares the submitted password against the stored one.
  Disabling two-factor sign-in is unaffected by this change: it was already merging both cases
  into one generic "invalid credentials" answer at the service layer, and logs no account-state
  distinction either way.
