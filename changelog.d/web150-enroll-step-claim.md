### Security

- **The code that confirms a 2FA enrollment can no longer be replayed at the first sign-in.**
  Enabling two-factor sign-in reset the account's TOTP replay floor to zero, so the confirmation
  code, still inside its ±30-second validity window, passed the next sign-in challenge as well.
  Enrollment now records the time step that code matched in the same write that enables 2FA,
  and the sign-in challenge refuses it like any other reused code. Disabling 2FA still resets
  the floor along with the secret.
