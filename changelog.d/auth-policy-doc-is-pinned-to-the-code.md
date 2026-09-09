### Internal

- **The auth policy document is now read back by a test.** `docs/security/auth-policy-and-rate-limits.md`
  states the per-IP rate-limit budgets, the environment variables that tune them, and the per-account
  lockout thresholds; nothing read any of those numbers back, so the document could drift from the
  server without a single check going red. Two tests now pin it in both directions: a budget stated
  in the document must match what the boot resolves, and a `RATE_LIMIT_*` variable read by
  `cmd/ovumcy/config.go` must have a row in the document. The per-account thresholds are compared
  against what `bootstrapOptions` actually wires rather than against the service constants they fall
  back to — the recovery budget ships as the one-hour `RATE_LIMIT_FORGOT_PASSWORD_WINDOW` default,
  not as the fifteen-minute constant behind it, and pinning the constant would have asserted a value
  no request ever meets.
