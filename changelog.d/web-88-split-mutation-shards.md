none

CI-only: the weekly mutation job's `internal/api` split stayed at 14 file-subset
shards even after moving to weight-dealt partitioning (WEB-78/WEB-79). Run
36274821938 (2026-09-27) cancelled `internal_api_12` and `internal_api_13` at
`mutation.yml`'s 180-minute cap with no upper bound on their true rate, so
`mutation-merge (internal_api)` failed while every other cell (all 12 other
`internal_api` shards, all 14 `internal_services` shards) succeeded. `internal/api`
now shards 20 ways; `scripts/mutation.sh`'s `SHARDED_PKGS`, the `mutation.yml`
matrix, and `TESTING.md`'s shard-count line move together.
