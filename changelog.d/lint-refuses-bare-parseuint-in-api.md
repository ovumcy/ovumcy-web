none

Lint-only: golangci-lint now enables `forbidigo` with a single pattern that
refuses `strconv.ParseUint` in `internal/api` production code, outside
`parseRequestUint`, which parses at `strconv.IntSize` so the result converts to
`uint` without truncation on a 32-bit target. The one other call site, the
request-log numeric-segment check, needs no integer at all and is now a digit
scan: a numeric segment is masked as `:id` whatever the platform's int size,
including one past the 64-bit range (21 to 23 digits were previously logged
verbatim, 24 or more as `:token`).
