none

Test-only change: adds a regression test pinning that the account-lockout 429
on POST /api/v1/sessions and POST /api/v1/password-resets carries no
Retry-After header or retry_after_seconds field, unlike the edge
rate-limiter's 429 on the same routes. No production behavior changed.
