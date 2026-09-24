### Security

- **A 2FA sign-in can no longer undo a re-enrollment made at the same moment.** A successful 2FA
  check against a TOTP secret stored before per-account binding reseals that secret in place,
  without revoking sessions. The reseal was written unconditionally, after the code check, so a
  re-enrollment or a disable landing in between was overwritten by the OLD secret, and the retired
  authenticator passed 2FA again. The reseal now applies only while the stored ciphertext is still
  the one the check opened; when a re-enrollment or disable wins, the reseal is dropped, the check
  still succeeds, and the session it minted is revoked by the competing write as before.
