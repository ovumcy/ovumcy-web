none

Test-only: a guard in scripts/ciguards now requires every `go test -race` run in the
workflows to pass the checkptr-off gcflags for modernc.org, so a new race lane cannot
silently re-enable it in the SQLite driver.
