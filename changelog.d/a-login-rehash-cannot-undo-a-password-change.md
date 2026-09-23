### Security

- **A sign-in can no longer undo a password change made at the same moment.** A successful login
  against an account whose stored hash predates the current bcrypt cost upgrades that hash in place,
  without revoking sessions. The upgrade was written unconditionally, after a full cost-12 hash, so
  a password change or reset landing in between was overwritten by a fresh hash of the OLD password,
  and the replaced password signed in again. The upgrade now applies only while the stored hash is
  still the one the login verified; when a change wins, the upgrade is dropped, the login still
  succeeds, and the session it minted is revoked by the change as before.
