none

Tests only: the limiter-import walk in `internal/api` no longer reads Go files under
`node_modules`, so a local `go test ./internal/api` stays green after `npm ci`; the timezone
validator test writes its NUL fixture as an escape instead of a raw byte.
