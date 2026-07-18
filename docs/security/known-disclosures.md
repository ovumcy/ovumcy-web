# Known Information Disclosure

_Part of the [Ovumcy security policy](../../SECURITY.md)._

## Known Information Disclosure

A few information-disclosure signals are accepted residual risk because closing them either breaks core flows or requires infrastructure Ovumcy does not assume self-hosters have (for example SMTP).

### Register enumeration: pickup-cookie follow-up oracle (residual)

`POST /api/v1/users` returns identical status, body, and Set-Cookie shape (a single sealed `ovumcy_register_pickup` cookie of fixed length, no `ovumcy_auth` or `ovumcy_recovery_code`) for both a brand-new email and a duplicate. Response timing is also equalized: the duplicate-email branch runs `equalizeRegistrationTiming` (two bcrypt-cost-10 comparisons against fixed placeholder hashes) to match the work `BuildOwnerUserWithRecovery` performs on the new-email branch (password hash + recovery-code hash), so the single POST response carries no observable difference. The follow-up `GET /register/welcome` then dispatches the pickup cookie either to `/register` (with auth and recovery cookies) for a valid pickup or to `/login` (with a neutral flash) for a decoy or expired pickup. An attacker who holds their own pickup cookie from a probe POST can therefore observe which redirect target their cookie resolves to and infer whether the email was new or already registered. This turns the single-request status / cookie / timing oracle into a two-request probe, with both endpoints under per-IP rate limiting, and the login bcrypt-timing equalization (`equalizeAuthCredentialsTiming`) further bounding any cross-endpoint follow-up.

Closing the residual signal entirely is mathematically impossible without an out-of-band verification channel: any in-app dispatch on the pickup cookie reveals the branch, and any login-after-register variant turns the probe into a follow-up `POST /api/v1/sessions` whose success or failure carries the same information. The only options are (a) a magic-link / email-driven enrollment that gates registration behind SMTP delivery (not assumed in the self-hosted deployment model), or (b) acceptance of the documented two-request probe. Both are revisited if Ovumcy ever ships a multi-tenant SaaS variant.

A separate vector — replay of a sealed `ovumcy_register_pickup` cookie captured from somebody else's response within the 5-minute TTL — is **not** part of this residual. The pickup cookie carries an opaque nonce that is consumed atomically through `register_pickup_tokens` on the first `GET /register/welcome` call; a captured cookie reaching the welcome endpoint a second time gets the same neutral `/login` redirect as a decoy or expired pickup, and cannot mint a second `ovumcy_auth` session.

### Auth sessions: absolute expiry only, no idle timeout (accepted design decision)

`ovumcy_auth` is bounded by an absolute TTL — 7 days by default, 30 days with remember-me — and carries no separate idle/inactivity timeout: a session that is used once and then left untouched remains valid until the absolute TTL elapses, not until some shorter period of inactivity. This widens the exposure window for a cookie captured early in its lifetime compared to a design with a sliding idle timeout.

The trade-off is deliberate rather than an oversight. Exposure is bounded two ways instead of one: the absolute TTL caps how long any captured cookie stays valid regardless of activity, and every request re-verifies `users.auth_session_version`, so any credential or posture change (password change/reset, recovery-code regeneration, forced operator reset, TOTP enable/disable, clear-data) revokes the cookie instantly rather than waiting for it to expire. For a personal, self-hosted health tracker — not a shared-workstation enterprise product — an idle timeout mainly adds friction (repeated re-logins) without a correspondingly large security gain, since the operator is typically also the sole user. This is not test-enforced; it is reviewed as a design decision, and revisited if the deployment model changes (see *Threat Model* below).

### Rate-limit and attempt-limiter state: process-local, resets on restart (accepted design decision)

Per-IP HTTP rate limits (Fiber's limiter middleware) and the per-account `AttemptLimiter` budgets (`internal/services/attempt_limiter.go`) are held in the process's own memory. They are correct only under the single-instance self-hosted deployment this codebase assumes: state is not shared across replicas, and a process restart silently resets every counter and cooldown.

This is a deliberate scoping choice, not a gap discovered after the fact: sharing limiter state across processes would require an external store (Redis or similar) that Ovumcy does not assume a self-hoster has provisioned, and the baseline deployment path is explicitly single-instance (see *Deployment* in `docs/SECURITY_INVARIANTS.md`). Anyone horizontally scaling Ovumcy behind a load balancer must be aware that each replica enforces its own independent budget — running N replicas multiplies the effective per-IP and per-account attempt budget by N — and should either keep the deployment single-instance or introduce a shared limiter store before scaling out. This is not test-enforced; it is a documented operational constraint of the current architecture.
