### Security

- **A backup taken while a revocation was committing can no longer bring it back.** Every
  revoke-or-erase write (sign-out, password change or reset, recovery-code rotation, TOTP on/off,
  OIDC link/unlink, email re-home, feed rotate/revoke, webhook save/remove, clear-data, account
  deletion, and the operator CLI's `users delete`, `reset-password` and `webhook set`) now writes
  the restore-fence file first and then records the fence's database marker in the SAME
  transaction as the protected row. A consistent snapshot can no longer hold the new marker beside
  the old row, so restoring one taken mid-write is read as a restore on the next start and
  answered with the full revocation. Concurrent sign-outs are no longer coalesced into one fence
  advance; each takes its own. Backups taken while the server was running are still restored with
  the fence file removed — see "Restoring a backup taken online" in the self-hosted guide.
