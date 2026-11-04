### Security

- **Linking an OIDC identity held by an account sign-in refuses answers with sign-in's reply.**
  When the identity being linked from Settings is already held by an account that sign-in
  refuses — a legacy row whose role is not `owner` — the link step-up and its confirmation now
  answer with the reply sign-in gives that identity instead of "That identity is already linked to
  another account.", so the refusal no longer names an account sign-in does not. An identity held
  by an ordinary owner account is still refused by name; that wording is now a declared exception
  in `SECURITY.md`.
