none

CI-only: the weekly mutation job's `internal/services` split stayed at 10
file-subset shards even though it moved to weight-dealt partitioning
alongside `internal/api`'s bump to 14 (issue #161 follow-up). Run
36138463409 (2026-09-25, dispatched on main) showed the mismatch: 8 of the
10 shards, each carrying the intended ~290-weight share, finished between
101 and 172 wall-minutes, and the remaining two (`internal_services_6`,
`internal_services_7` — no single file heavier than 97 of either shard's
own ~289) were still mutating when `mutation.yml`'s 180-minute cap
cancelled them, so `mutation-merge (internal_services)` had nothing
complete to fold. `internal/services` now shards 14 ways, matching
`internal/api`; the resulting ~207-weight shards, at the same worst
observed rate, land near 123 minutes — the same cap headroom
`internal/api`'s split already carries. `scripts/mutation.sh`'s
`SHARDED_PKGS`, the `mutation.yml` matrix, and `TESTING.md`'s shard-count
line move together.
