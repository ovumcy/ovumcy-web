none

CI-only: the weekly mutation job's 20-way `internal/api` split still cancelled
`internal_api_20` at the 180-minute cap in runs 36351058682 and 36413106904, because
per-shard wall time does not follow the token-count weight. `internal/api` now shards 30 ways
(`SHARDED_PKGS`, the `mutation.yml` matrix, `TESTING.md`).
