### Security

- **An operator command moves the restore fence once.** `ovumcy users delete`, a forced
  `reset-password`, `users set-email` and `webhook set` confirmed and advanced the fence, and the
  write that followed advanced it a second time. A fence file that failed on that second write left
  the two halves disagreeing, and the next start revoked every session, webhook endpoint and local
  password on the instance, including the password the operator had just set. The confirmed
  advance now covers that write.

### Changed

- **Concurrent sign-outs share one restore-fence write.** Every sign-out records itself in the
  fence; sign-outs that arrive while one is being recorded now wait for a single shared write
  instead of queuing one synced file write each.
