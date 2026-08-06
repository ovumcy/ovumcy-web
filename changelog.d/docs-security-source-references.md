### Internal

- **The security docs now cite the file each control actually lives in.** Five references still
  pointed at `cmd/ovumcy/main.go` for the CSP, `securityHeadersMiddleware`, CSRF and
  `rateLimitKeyGenerator`, all of which moved out of it — into `cmd/ovumcy/server.go` and
  `cmd/ovumcy/ratelimit.go` — when the server wiring was split out. The controls themselves are
  unchanged; only the paths a reader follows were wrong.
- **The per-account rate-limit list now includes recovery-code redemption.** `POST
  /api/v1/password-resets` has always carried an identity-keyed budget on top of its per-IP row,
  and the recovery-code section pointed at that list for it, but the list itself did not name it.
