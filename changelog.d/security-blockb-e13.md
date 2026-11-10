### Internal

- **Docs: `OIDC_CA_FILE`, `OIDC_POST_LOGOUT_REDIRECT_URL` and `REMINDER_SCHEDULER_*` have rows in
  SECURITY.md and docs/SECURITY_INVARIANTS.md.** A new test checks that a post-logout redirect off
  the callback origin, over plain http, or carrying a query or fragment fails startup validation.
