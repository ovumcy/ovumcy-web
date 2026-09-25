none

Wording only: the documented local full-suite command — `TESTING.md`'s run block and its
coverage recipe, `CONTRIBUTING.md`, and `README.md` — now passes `-timeout 30m` instead of `20m`.
The `internal/api` DB-integration suite alone took 887-1101 s on a development machine (measured
2026-09-20), which left under two minutes of margin under 20m; CI keeps its per-cell 20m because it
shards that suite. No behavior changes.
