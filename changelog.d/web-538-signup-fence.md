### Security

- **A restore across nothing but a sign-up is now detected.** Creating an account (sign-up, OIDC
  auto-provisioning, `ovumcy users create`) now records itself in the calendar-feed restore fence the same
  way a sign-out or password change does. Before, restoring a backup taken before a sign-up was not read as
  a restore, so the session epoch did not rotate and a cookie issued to the lost account could resolve as a
  new account that reused its id. If the fence file cannot be written, the sign-up is refused and can be
  retried; nothing is created. `ovumcy users create` needs `CALENDAR_FEED_FENCE_PATH` set to the server's
  fence path, as `users delete` already does.
