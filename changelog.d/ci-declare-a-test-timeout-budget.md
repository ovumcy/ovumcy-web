none

CI change with no user-visible effect: every `go test` run in the workflow now
declares `-timeout 20m` instead of inheriting Go's implicit 10-minute
per-package default, which `internal/api` was running within seconds of.
