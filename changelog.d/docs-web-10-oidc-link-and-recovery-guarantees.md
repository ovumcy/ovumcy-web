### Internal

- **OIDC linking and recovery docs now describe what the code actually does.** Five places still
  described the public `/auth/oidc/link-confirm` password-and-TOTP page as the way to finish linking
  an identity: `docs/oidc.md`'s Current Contract summary, `docs/self-hosted.md`'s OIDC section,
  `docs/security/threat-model.md`'s account-takeover mitigation entry, and two supporting mentions in
  `docs/security/auth-policy-and-rate-limits.md` and `docs/security/known-disclosures.md`. That page
  has been retained-but-unreachable since issue #701 — the callback never mints the cookie it reads —
  and the real paths are the authenticated Settings step-up (current password, then a fresh
  `prompt=login` re-authentication at the provider; no separate TOTP challenge) and the operator CLI's
  `link-oidc-identity` for an account with no working sign-in. `docs/oidc.md` and
  `docs/self-hosted.md` also undersold `OIDC_LOGIN_MODE=oidc_only`'s local-recovery guarantee as
  hiding buttons "from the browser UX"; `Login`, `Register`, and `ForgotPassword` actually refuse the
  request server-side before doing anything else. `docs/security/oidc-and-sessions.md`'s "same
  session" language already matched the code (an account-id comparison, not a session-id one) and
  needed no change. No Go changes; the callback, the step-up handlers, and the CLI command behave
  exactly as before.
