none

Test-only: the operator CLI's calendar-feed gate now has an end-to-end guard in
`scripts/backuprestoredoc`, over both engines. It runs the real binary as its
own process against the runbook's own backup and restore commands, and takes
its verdict from an unauthenticated HTTP GET of the old subscribe URL — the
surface a calendar client polls — instead of a repository read. No change to
shipped behaviour.
