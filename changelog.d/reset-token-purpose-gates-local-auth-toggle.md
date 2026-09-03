### Security

- **A forced password-reset link minted by local sign-in no longer outlives the operator switching
  local sign-in off.** The earlier fix for this ("A password-reset link no longer outlives local
  sign-in") decided at redeem time from a `forced` flag carried in the sealed reset cookie — not
  forgeable, but not signed into the token either, and every mint path set it the same way regardless
  of *which* factor had actually been checked. A token minted by the plain local sign-in form set the
  same flag as one minted by an SSO sign-in, so once local sign-in was switched off, a reset started
  the minute before still rewrote the password, turned local sign-in back on, and handed out a
  session — on an instance whose operator had just closed local sign-in for that exact reason. The
  redeem decision now reads a purpose signed into the token itself: only a token minted by an SSO
  sign-in survives local sign-in being off; one minted by a local password is refused exactly like an
  ordinary password-recovery link. OIDC link-confirm's own password challenge mints the same
  local-password purpose, since it checks a local password too.
- **A reset link minted before this update stops working within its existing 30-minute lifetime**,
  rather than being reinterpreted under the new rule. No account action is needed; a reset started
  before the update and not finished within 30 minutes is answered "invalid reset token" and the
  stale cookie is cleared, the same way an expired link has always been answered.
