# Logging Policy

_Part of the [Ovumcy security policy](../../SECURITY.md)._

## Logging Policy

Ovumcy does **not** emit per-action audit logs by default. The `AUDIT_LOG_ENABLED` environment variable controls the audit-event stream:

- `AUDIT_LOG_ENABLED=false` (default) — the runtime emits no `security event:` lines. Go panics, startup configuration errors, and the Fiber request log remain enabled.
- `AUDIT_LOG_ENABLED=true` — the runtime emits per-action security-event lines to stderr via the Go standard `log` package. Each line includes the action name, outcome, request method, **sanitized** request path (concrete date parameters are replaced with `:date` and other identifiers are similarly masked), response format, and — for authenticated requests — `user_id` and role. Example:

  ```
  security event: action="health.day_upsert" outcome="success" method="POST"
                  path="/api/v1/days/:date" format="json" user_id="42"
                  role="owner" domain="health_data" target="day_entry"
  ```

When enabled, these lines are visible to the operator through their container runtime (`docker compose logs`, journald, etc.) and never leave the host. They are intended for ad-hoc incident investigation — for example, to confirm whether a suspected compromise produced state-changing requests, and from which `user_id`. The audit stream is not designed as a compliance audit trail; nothing in Ovumcy itself ships, archives, or rotates these lines.

If you enable `AUDIT_LOG_ENABLED=true`, plan retention and access control around the persistent-identifier content (`user_id`, role). Treat the resulting log stream as the same sensitivity class as the database itself.

The Fiber request log (`time | status | latency | method | path`) is independent of `AUDIT_LOG_ENABLED` and remains enabled in all configurations. It does not include `user_id` or authenticated-session metadata.

The startup banner reflects the current setting (`audit_log=true|false`) so operators can confirm the effective configuration on each boot.
