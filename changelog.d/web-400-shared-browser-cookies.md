### Security

- **A password-reset hand-off no longer waits half an hour in a shared browser.** The cookie that
  carries a reset in progress — a forced reset after sign-in, or one started with a recovery code —
  was kept for 30 minutes, and on its own it was enough to set a new password and get signed in.
  Whoever used the same browser next could finish it. The cookie now lasts only for the browser
  session, and the reset token inside it is valid for 10 minutes instead of 30.

- **A refused session takes its leftovers with it.** When a session is refused — expired, revoked or
  invalid — the browser is now also told to drop the one-time recovery-code page, a pending
  two-factor enrollment and the one-time calendar-feed link that only that session could use. A
  reset or two-factor sign-in already under way is left alone.

- **A session token without a session version is refused.** It used to be read as version 1. No such
  token can be made without the server's key; the parser now simply refuses it, as the other
  sign-in grants already did.

- **The erasure re-authentication buttons check which account the page was opened for.** Starting
  the single-sign-on confirmation for clearing data or deleting the account from a settings page
  opened under another account in the same browser is refused, like every other settings write.
