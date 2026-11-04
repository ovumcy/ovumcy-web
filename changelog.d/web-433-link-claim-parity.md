### Security

- **Linking an OIDC identity whose holder sign-in would not reach now gets sign-in's answer.**
  When the identity being linked from Settings is already held by an account that sign-in with it
  stops short of — a legacy row whose role is not `owner`, or an account sent to a forced password
  reset that has no local password to issue it for — the link step-up and its confirmation now
  answer with the error sign-in stops on instead of "That identity is already linked to another
  account.", so the refusal no longer names an account sign-in does not. An identity held by an
  owner account sign-in reaches is still refused by name; that wording is now a declared exception
  in `SECURITY.md`.
