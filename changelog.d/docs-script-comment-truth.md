### Internal

- **`scripts/mutation.sh`'s header comment now describes the script that
  ships.** It documented a `merge-api-shards` subcommand (the real one is
  `merge-shards <base>`), claimed `verify-shards` proves the `internal/api`
  partition alone (it proves every entry in `SHARDED_PKGS`, and
  `internal/services` has been registered there since it was sharded too), and
  named two identifiers that do not exist — `API_SHARD_COUNT` and
  `api_shard_files`, against the live `SHARDED_PKGS` and `shard_files`. It also
  called `internal/api` the largest mutation target; `internal/services` is
  larger (11,555 source lines against 8,099), while `internal/api` remains the
  slowest, which is the fact the budgeting advice actually rests on.
- **`scripts/readmeversion/main_test.go` no longer names a release tag in its
  own doc comment.** It said "currently v1.8.0" while the latest tag is v1.9.2.
  The assertions are version-agnostic, so the version was dropped rather than
  bumped: a comment that has to be updated on every release is the drift this
  test exists to catch.
