### Security

- **A crash during a password reset, recovery-code regeneration, clear-data or account deletion
  can no longer leave a calendar feed restorable.** These operations remove the owner's
  calendar-feed link and record that removal in the restore fence, but they recorded it only
  after their own write had committed. A crash in between left the fence agreeing with a backup
  taken before the removal, so restoring that backup brought the removed subscribe link back.
  They now record the removal first, as revoking the feed itself already did. A fence that cannot
  be written still never blocks the operation.
