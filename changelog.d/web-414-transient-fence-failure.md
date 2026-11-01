### Security

- **A momentary failure to write the calendar-feed restore fence no longer locks every owner out.**
  When the fence file could not be written for a moment (a full disk, an I/O error), an owner's
  calendar-feed or webhook change still went through and only the database half of the fence moved.
  The next start could not tell that from a restored backup and revoked every session, webhook and
  local password on the instance, leaving the operator to run `ovumcy reset-password` for every
  account. Those changes are now refused instead, with the fence untouched, and succeed on retry.
  A password change, recovery-code rotation, clear-data or account deletion is still never refused
  over the fence; if one meets the same failure, the next start still runs the full revocation,
  because a restore across it could not be told apart otherwise.

- **`ovumcy webhook set` now confirms the restore fence before it writes.** Run from a shell that
  could not see the server's fence (`CALENDAR_FEED_FENCE_PATH` unset or a different file), it used
  to withdraw an endpoint in the database alone, and a restore of an earlier backup let
  `ovumcy notify` deliver to it again. It now refuses with "Nothing was changed" unless it can
  advance the server's own fence first, as `ovumcy users delete` and `ovumcy reset-password` already
  do. `--dry-run` still works anywhere.
