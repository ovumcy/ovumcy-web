### Security

- **A restored backup no longer brings back a password an operator reset replaced, and
  `ovumcy notify` no longer delivers from a restored database before the server has seen it.**
  A restore already signed every owner out and cleared every webhook endpoint on the next start,
  but the password hash came back as the backup held it: an account that was compromised and then
  force-reset accepted the compromised password again. Setting a forced password change would not
  have helped — the forced-change route accepts that same password, and it ranks above two-factor
  sign-in. So the start that detects a restore now revokes every local password in the same step.
  An owner with a linked OIDC identity signs in through it; for everyone else, run
  `ovumcy reset-password <email>` and hand the owner the new password, which they are made to
  replace at their first sign-in. The startup line names how many passwords were revoked.

  `ovumcy notify` now checks the restore fence, read-only, before it reads any endpoint, and
  refuses — sending nothing — until the server has started once over a restored database, so a
  cron job or sidecar cannot post to an endpoint an owner removed after the backup point. Run it
  where the server's fence file is visible, as `reset-password` already requires.
  `CALENDAR_FEED_FENCE_OPTOUT=true` lets it run without a fence, never over a disagreeing one.
  Removing a webhook endpoint or saving webhook settings now advances the fence too, so a restore
  across nothing but a webhook change is detected.

  A fence that can be read but not written no longer repeats the restore revocation on every
  start: the start ends unanchored, disarms the calendar feeds as before, and revokes nothing
  else. Full contract: `docs/self-hosted.md → Calendar Feed Restore Fence`.
