### Security

- **Linking an OIDC identity no longer says more than signing in with it.** When the identity being
  linked from Settings is already held by an account that
  sign-in refuses — a legacy row whose role is not `owner` — the link step-up and its confirmation
  now answer with sign-in's own reply instead of "That identity is already linked to another
  account.", so the refusal no longer reveals an account sign-in keeps hidden. An identity held by
  an ordinary owner account is still refused by name; that wording is now a declared exception in
  `SECURITY.md`.
