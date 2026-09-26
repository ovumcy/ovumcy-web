none

CI-only: the weekly mutation job's `internal/services` split stayed at 10
file-subset shards even though it moved to weight-dealt partitioning
alongside `internal/api` (WEB-78/WEB-79). Run 36138463409 (2026-09-25)
cancelled two of the ten shards at `mutation.yml`'s 180-minute cap with
no upper bound on their true rate. `internal/services` now shards 14
ways, matching `internal/api`; `scripts/mutation.sh`'s `SHARDED_PKGS`,
the `mutation.yml` matrix, and `TESTING.md`'s shard-count line move
together.
