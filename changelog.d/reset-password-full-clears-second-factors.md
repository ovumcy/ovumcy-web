### Security

- **`ovumcy reset-password --full` recovers a compromised account completely.** The operator
  reset replaced the password and signed every device out, but left two-factor sign-in, the
  recovery code and linked OIDC identities in place, so an identity or authenticator an intruder
  had added still worked after the reset. `--full` clears all three in the same write that resets
  the password and revokes the sessions. Without the flag the command behaves as before.
