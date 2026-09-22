none

Lint-only: golangci-lint now enables `forbidigo` with a single pattern that
refuses `strconv.ParseUint` in `internal/api` production code, outside
`parseRequestUint`, which parses at `strconv.IntSize` so the result converts to
`uint` without truncation on a 32-bit target. The one other call site, the
request-log numeric-segment check, now goes through that helper; its verdict
is unchanged on 64-bit targets.
