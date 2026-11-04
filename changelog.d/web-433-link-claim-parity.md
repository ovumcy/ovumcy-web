### Security

- **Breaking (API shape): linking an OIDC identity whose holder sign-in would not reach now gets
  the account-unavailable reply.** `POST /api/v1/users/current/oidc/link/confirm` (and the link
  step-up's `/settings` flash) used to answer every cross-user claim with `409` "That identity is
  already linked to another account." Two holder cases now get `403` "sso sign-in unavailable"
  instead: a legacy row whose role is not `owner`, and an account sent to a forced password reset
  that has no local password to issue it for. The refusal no longer names an account sign-in does
  not reach. An identity held by an owner
  account sign-in reaches is still refused by name with `409`; that wording is a declared exception
  in `SECURITY.md`.
