none

CI change with no user-visible effect: the Go unit lane is sharded. `internal/api`
— 353 s of the lane's 369 s of testing — is split across three jobs by test name,
every other package runs in one lane beside it, and the analysis tools (staticcheck,
golangci-lint, actionlint, deadcode, vet) move into a third so they are paid once
rather than per shard. The per-lane coverage profiles are merged back into the
single artifact the Codecov upload and the patch-coverage gate already consumed,
so both read the same whole-suite coverage as before.
