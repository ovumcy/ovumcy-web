### Security

- **A restore across nothing but a sign-out, password change, TOTP change or OIDC link is now
  seen.** The first start after a restore signs every owner out and revokes local passwords, but
  it could only tell a restore by the fence, and these writes did not move it: restoring a backup
  taken just before one of them brought back the signed-out cookie, the old password, the removed
  or missing second factor, or the unlinked identity. Every write that signs an account out or
  changes how it signs in now records itself in the fence first, as feed and webhook changes
  already did. `ovumcy users set-email` now needs `CALENDAR_FEED_FENCE_PATH`, like `users delete`
  and a forced `reset-password`, and refuses without it.
