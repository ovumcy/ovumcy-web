### Internal

- **A required CI check can no longer disappear behind a `t.Skipf`.** `bash`/`docker`/`git`
  absence and a missing tz database were skipped identically to an operational failure — bash
  answering garbage, docker's daemon not responding, git exiting non-zero, zoneinfo parsing wrong —
  so a misconfigured runner reported the same quiet green as a developer machine that never
  installed the tool. `internal/testenv` now gives every site one fail-closed switch per resource
  (`OVUMCY_REQUIRE_BASH`/`DOCKER`/`GIT`/`TZDATA`), armed on the lane that owns each check in
  `ci.yml`: absence skips only where the lane never declared the resource mandatory, and an
  operational error always fails, on every machine. `dashboard_today_request_timezone_regression_test.go`'s
  previously unconditional `t.Skip` is now the same conditional absence check as the rest.
