none

Wording only: `TESTING.md`'s local full-suite and coverage commands now pass `-timeout 30m`
instead of `20m`. The `internal/api` DB-integration suite alone took 887-1101 s on a development
machine, which left under two minutes of margin under 20m; CI keeps its per-cell 20m because it
shards that suite. No behavior changes.
