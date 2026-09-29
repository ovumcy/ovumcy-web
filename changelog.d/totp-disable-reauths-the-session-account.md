### Security

- **Turning off two-factor sign-in now checks the password of the signed-in account itself.** The
  password confirming the disable was checked by looking the account up again by its email, not
  against the account the session belongs to. On a database whose email index is missing and where
  two accounts share an address, that lookup refused every attempt, so the owner could never turn
  2FA off and each try counted against the disable attempt limit. The password is now checked
  against the signed-in account's own stored password. The attempt limit, the "invalid
  credentials" answer and its timing are unchanged. As on sign-in and the other Settings
  confirmations, spaces around the password are ignored, and a disable no longer upgrades an older
  stored password hash (sign-in still does). The failed-attempt count is cleared only once the
  disable has gone through, not when the password alone was right.
