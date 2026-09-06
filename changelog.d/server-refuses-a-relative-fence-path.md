### Fixed

- **A relative `CALENDAR_FEED_FENCE_PATH` now refuses the server's boot instead of being accepted on
  one side only.** The calendar-feed restore fence keeps one marker in the database and one in a file
  at that path, and both the server and the operator CLI (`ovumcy users delete`, a forced
  `ovumcy reset-password`) have to open the same file for a revocation to be recorded outside the
  database. The CLI already refused a relative path, because it resolves against whichever working
  directory the command runs in; the server took the same value verbatim and resolved it against its
  own. On a bare-binary deployment that left a server whose fence worked and operator removals that
  could never be completed — refused with a remedy, "point the server at this path", the operator had
  already carried out. Both sides now judge the value with one predicate, so the set of accepted paths
  is the same on each.

  **Upgrade note.** An instance whose `CALENDAR_FEED_FENCE_PATH` is relative will not start after this
  release: the boot stops with `invalid CALENDAR_FEED_FENCE_PATH=...`, naming the value and asking for
  an absolute one. Change the variable to the absolute path of the same file — for the bundled compose
  stacks and the image this is already `/app/fence/calendar-feed.fence`, so nothing changes there — and
  start again. Leaving the variable unset is unchanged and still supported: no fence is configured, and
  every armed calendar feed is disarmed on each start, as the startup line reports.
