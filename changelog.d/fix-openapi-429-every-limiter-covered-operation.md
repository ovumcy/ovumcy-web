### Fixed

- **`docs/openapi.yaml` now declares `429` (`RateLimited`) on every `/api/v1` operation.** The
  app-wide API limiter (`cmd/ovumcy/server.go`'s `app.Use("/api", limiter.New(...))`) is mounted
  ahead of route registration with no `Next` filter, so it sits in front of all 47 `/api/v1`
  operations — not only the twelve that also carry a narrower per-account budget and were the only
  ones documented. This was the same "N of N+1" shape as the status-reachability gap fixed
  previously: a prior change added `429` where the per-account reauth budget lives and stopped
  there, leaving the shared edge limiter's `429` real but undocumented everywhere else. No server
  behavior changed — this is documentation only.
  - A new test, `TestOpenAPIDeclaresRateLimitedOnEveryLimiterCoveredOperation`, reads the registered
    `/api/v1` routes and fails when any of them lacks a declared `429`, so the gap cannot reopen
    silently.
