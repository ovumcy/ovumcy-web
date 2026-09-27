### Security

- **A revoking write against an account whose session-version counter was stuck at 0 (only
  possible on a hand-edited or restored row; every account has carried at least 1 since migration
  008) now actually signs out every other device.** The counter's own bump used to write `0 + 1 =
  1`, the exact version every session already granted against that row was already reading as
  current, so a password reset, a recovery-code regeneration, a password change, a two-factor
  change, a clear-data wipe, or an OIDC identity link/unlink on such a row left every other session
  signed in. A migration raises any row still at 0 or below to 1 on startup, and every one of those
  compare-and-set writers — the password-reset path and the one shared by the rest — no longer
  treats a stored 0 as a match for version 1.
