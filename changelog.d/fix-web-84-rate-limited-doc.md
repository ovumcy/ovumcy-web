### Fixed

- **The `429` API docs now say when `retry_after_seconds` is missing.** Login and password-reset can be refused by a per-account attempt lockout that carries no refill timer, not only the per-IP rate limiter — the shared `RateLimited` response schema now documents both cases instead of promising the field unconditionally.
