### Security

- **Operator commands now refuse a database the server refuses to start on.** `ovumcy users`,
  `reset-password`, `link-oidc-identity`, `webhook` and `notify` run the same normalized-email index
  check the server runs at startup, and answer with the same `schema check failed: …` message before
  they prompt, read or write anything. Before, `users create` or `users set-email` could put a second
  account on one email address while the server was down because the index was missing.
- **Operators:** remove any duplicate accounts in SQL with the server stopped, then restore the index as
  the refusal describes. `ovumcy repair` is unaffected.
