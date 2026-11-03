### Security

- **A calendar-feed or webhook revocation made while the restore-fence volume was unmounted can no
  longer be undone by restoring a backup.** Revoking or rotating a feed, or saving or removing a
  webhook, over a fence volume that had dropped out of a running server recorded the change in the
  database alone. If the volume was mounted again and a backup taken before the change was restored
  before the next start, the restored database agreed with the old fence file, and the revoked
  subscribe URL or removed webhook endpoint worked again. These writes now retry the fence-file
  write like sign-outs do, and when the fence file stays unwritable, reads as missing or cannot be
  read, they are refused with nothing changed and can be retried.
