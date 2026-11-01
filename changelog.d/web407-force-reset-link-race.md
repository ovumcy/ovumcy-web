### Internal

- **`reset-password --full` is tested against an OIDC link racing it on Postgres, and clears the
  second factors in its one UPDATE.** A link queued on the account ahead of the reset commits while
  the reset waits, and the reset still deletes the identity it added. The TOTP and recovery-code
  clear now rides the reset's own UPDATE instead of a second statement in the same transaction.
