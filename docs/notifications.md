# Webhook Notifications

Ovumcy can remind an owner about an upcoming period or ovulation by POSTing a
small JSON message to a webhook URL the owner controls — for example a
self-hosted [ntfy](https://ntfy.sh/) or [Gotify](https://gotify.net/) instance,
or any other endpoint that accepts a JSON POST. There is no third-party
notification service involved and no outbound call to anything other than the
URL the owner configured: this is a zero-cost, fully self-hosted notification
path, consistent with the rest of Ovumcy's single-tenant, operator-controlled
model.

> [!IMPORTANT]
> Every notification is an estimate, never a fact. Every delivered payload
> carries the same medical-safety disclaimer shown in the app:
> **"These are estimates, not medical advice or a method of contraception."**

This document covers enabling webhook notifications per owner, scheduling the
delivery pass, and the idempotency and security behavior an operator should
understand before wiring it into cron/systemd/Task Scheduler.

## What it is

- A period or ovulation reminder is decided per owner from that owner's own
  logged data and cycle settings — the same prediction inputs the in-app
  dashboard banner uses (issue #123 / #124 slice 1).
- When a reminder is due, Ovumcy POSTs a minimal JSON body to the owner's
  configured webhook URL.
- Delivery is **not** a background goroutine inside the running server. It is a
  separate, request-free CLI pass (`ovumcy notify`) that an operator schedules
  to run periodically (cron, systemd timer, a scheduled Docker one-shot, or
  Windows Task Scheduler — see [Scheduling the pass](#scheduling-the-pass)
  below).
- Re-running the pass is safe: a reminder that was already delivered
  successfully is never sent twice in the same cycle (see
  [Idempotency and safety](#idempotency-and-safety)).

## How to enable it per owner

Webhook notifications are driven entirely by columns on the `users` table:

| Setting | Column | Meaning |
| --- | --- | --- |
| Enabled | `webhook_enabled` | Master on/off switch for this owner's webhook delivery. |
| URL | `webhook_url` | The owner's webhook endpoint. Stored **encrypted at rest** (AES-256-GCM via `security.EncryptField`, AAD-bound to the owner's user id) — never in plaintext. |
| Notify on period | `webhook_notify_period` | Whether a period-soon reminder is delivered. |
| Notify on ovulation | `webhook_notify_ovulation` | Whether an ovulation-soon reminder is delivered. |
| Lead days | `reminder_lead_days` | How many days before the estimated event a reminder becomes due. **Shared** with the in-app dashboard reminder banner — one setting drives both. Clamped to 0–14; defaults to 3. |

**Honest gap:** as of this slice, there is no Settings-page form and no
`/api/v1/*` HTTP endpoint for an owner to set `webhook_enabled`, `webhook_url`,
`webhook_notify_period`, or `webhook_notify_ovulation` from the browser. The
save-and-encrypt logic exists and is fully covered
(`internal/services/webhook_settings_service.go`,
`WebhookSettingsService.SaveWebhookSettings`), and the CLI/notify pass reads
these columns at delivery time, but no route in `internal/api/routes.go`
currently calls it — `internal/bootstrap.BuildDependencies` (the HTTP handler's
dependency graph) does not wire a `WebhookSettingsService` at all. Today an
operator (or a future UI slice) sets these four columns directly, for example
with the SQLite CLI against the data volume, or a one-off script using the same
`security.EncryptField` scheme for the URL column. Do not write the URL as
plaintext — an unencrypted `webhook_url` will fail to decrypt as ciphertext
when the notify pass reads it, and that owner will be skipped (fails safe, see
below).

`reminder_lead_days` is the one exception: it is a plain integer column with no
sensitive content, and it already drives the in-app dashboard reminder banner
(`internal/services/dashboard_reminder_banner.go`), so it is reachable through
whatever settings path exists for the dashboard banner feature today. If your
Ovumcy build does not yet expose a lead-days control in the UI either, the
column default (3 days) applies.

If you maintain this deployment and add the settings UI in a later slice,
update this section — the columns and the encryption contract described above
will not change underneath it.

## Scheduling the pass

`ovumcy notify` is a local-only CLI subcommand (same family as `ovumcy users`
and `ovumcy healthcheck` — see the [Operator CLI](../README.md#operator-cli)
section of the README). One invocation is one pass: it lists every owner,
decides which reminders are due, delivers them, and exits.

```
usage: ovumcy notify [--dry-run] [--fail-on-delivery-error]
```

- `--dry-run` computes what **would** be sent — owners scanned, reminders due,
  and a preview line per due reminder (type, estimated date, destination
  **host only**) — but makes no outbound HTTP request and writes no watermark.
  Use it to verify a schedule or a fresh deployment before it starts actually
  delivering.
- `--fail-on-delivery-error` makes the process exit non-zero if **any**
  individual delivery failed during the pass. Without it (the default), a
  single unreachable owner endpoint is treated as an expected transient — the
  pass still exits 0 as long as it completed, so a monitoring system watching
  the process exit code will not page on one owner's Gotify instance being
  briefly offline. The failed delivery is retried automatically on the next
  scheduled pass (see [Idempotency](#idempotency-and-safety)). Turn the flag on
  if you specifically want your scheduler (cron mailer, systemd
  `OnFailure=`, etc.) to surface delivery failures.
- A pass-level failure (cannot open the database, invalid `SECRET_KEY`, bad
  arguments) always exits non-zero, regardless of the flag.
- The command prints only aggregate counts and owner ids to stdout — never a
  URL, token, or health specific — so its output is safe to capture in an
  operator log or cron mailer.

Run it once daily at a fixed local hour that suits your household — for
example, mid-morning, so a period-due reminder for today already reflects
"today" in the owner's own timezone rather than the previous day rolling over
mid-cycle-check. See [Timezone behavior](#timezone-behavior) below for exactly
which zone "today" is evaluated in.

### Cron

Add a line to the crontab of the user that has access to the Ovumcy binary,
database path, and `SECRET_KEY` (or `SECRET_KEY_FILE`):

```cron
# Run the Ovumcy webhook notify pass daily at 09:00 in the server's local time.
0 9 * * * SECRET_KEY_FILE=/etc/ovumcy/secret_key DB_DRIVER=sqlite DB_PATH=/var/lib/ovumcy/ovumcy.db /usr/local/bin/ovumcy notify >> /var/log/ovumcy-notify.log 2>&1
```

Adjust `DB_DRIVER`/`DB_PATH` (or `DATABASE_URL` for Postgres) and the secret
source to match your deployment's actual environment.

### systemd timer

A service unit plus a timer unit keeps the schedule declarative and gives you
`systemctl status`/`journalctl` for free:

```ini
# /etc/systemd/system/ovumcy-notify.service
[Unit]
Description=Ovumcy webhook notify pass
After=network-online.target

[Service]
Type=oneshot
EnvironmentFile=/etc/ovumcy/notify.env
ExecStart=/usr/local/bin/ovumcy notify
User=ovumcy
```

```ini
# /etc/systemd/system/ovumcy-notify.timer
[Unit]
Description=Run the Ovumcy webhook notify pass daily

[Timer]
OnCalendar=*-*-* 09:00:00
Persistent=true

[Install]
WantedBy=timers.target
```

`/etc/ovumcy/notify.env` holds `SECRET_KEY_FILE=...`, `DB_DRIVER=...`,
`DB_PATH=...` (or `DATABASE_URL=...`), and `TZ=...` if you want to pin the
fallback timezone explicitly rather than inherit the host's. Enable with
`systemctl enable --now ovumcy-notify.timer`.

### Docker (one-shot against the compose service)

The bundled `docker-compose.yml` defines the app service as `ovumcy`. Run the
notify pass as a one-off container that reuses the same image, environment,
and data volume as the running service, without restarting it:

```bash
docker compose run --rm ovumcy /app/ovumcy notify
```

Schedule that command with the host's cron or systemd timer (per above),
pointing at your compose project directory. `--rm` cleans up the one-shot
container after each run so it never accumulates stopped containers. If your
deployment overrides the service name in your own compose file, substitute
that name for `ovumcy`.

### Windows Task Scheduler

For a Windows host running the binary directly (not in a container), create a
daily task:

```powershell
$action = New-ScheduledTaskAction -Execute "C:\ovumcy\ovumcy.exe" -Argument "notify" -WorkingDirectory "C:\ovumcy"
$trigger = New-ScheduledTaskTrigger -Daily -At 9:00am
Register-ScheduledTask -TaskName "OvumcyNotify" -Action $action -Trigger $trigger -Description "Ovumcy webhook notify pass"
```

Set `SECRET_KEY`/`SECRET_KEY_FILE`, `DB_DRIVER`, `DB_PATH` (or
`DATABASE_URL`), and optionally `TZ` as machine or user environment variables
before registering the task, since `Register-ScheduledTask` does not carry
your current shell's environment into the task's run context. For a
containerized Windows deployment, prefer the Docker one-shot above instead of
a native scheduled task.

### Recommended cadence

Once daily, at a fixed local hour, across all of the above. There is no
supported sub-daily interval requirement — `reminder_lead_days` already gives
several days of lead time, so a once-a-day pass is sufficient to catch every
due reminder before the event. Running it more than once a day is harmless
(idempotent — see below) but unnecessary.

## Timezone behavior

Each owner's "today" is evaluated in:

1. **The owner's own persisted timezone**, if one is set (the per-user
   timezone captured from the browser, issue #159) — this is preferred.
2. Otherwise, the **server's local timezone** — resolved from the `TZ`
   environment variable (default `Local`, i.e. whatever the host/container's
   local timezone is) — as a fallback for owners with no persisted timezone.

This is exact: `internal/services/webhook_notify_service.go`'s
`resolveOwnerLocation` loads `time.LoadLocation(ownerTimezone)` first and only
falls back to the injected server location when the owner's stored timezone is
empty or fails to parse as a valid IANA zone. In a household with owners in
different timezones who have each had their timezone captured, each owner's
reminders are decided against their own local calendar day, not the server's.

If you run the pass once daily at a server-local hour and most or all of your
owners have not yet had their timezone captured, that hour is effectively
"09:00 server time" for everyone. Once an owner's browser has recorded their
timezone (this happens automatically per #159), their reminders shift to their
own local day boundary regardless of what server-local hour you scheduled the
pass at, since the decision is timezone-aware, not merely clock-time-aware.

## Idempotency and safety

- Each reminder kind (period, ovulation) has its own **watermark** column per
  owner (`webhook_period_last_sent_cycle_start`,
  `webhook_ovulation_last_sent_cycle_start`), storing the cycle-start anchor
  date the reminder was last successfully sent for.
- The watermark advances **only after a successful (2xx) delivery**. A failed
  delivery (timeout, non-2xx, refused redirect, connection error) leaves the
  watermark untouched.
- Consequence: re-running the pass is always safe.
  - A reminder already delivered this cycle is not sent again — the decision
    layer treats it as already covered by the watermark and it doesn't count
    toward that pass's due total.
  - A reminder whose delivery failed last time is retried automatically on the
    next pass, with no separate retry mechanism to configure — the schedule
    itself **is** the retry loop.
- This means you can run the pass on an ordinary daily schedule and never worry
  about double-notifying an owner because a previous run overlapped, was
  re-triggered, or ran twice due to a scheduler misconfiguration.
- A pass never fails all owners because of one bad owner: a decrypt failure, a
  load failure, or a delivery failure for one owner is logged (owner id and
  host only) and the pass continues to the next owner.

## Security notes

The delivery envelope was hardened in the slice that shipped outbound egress
(#124 slice 3) and is already documented in the security invariants matrix —
see [`docs/SECURITY_INVARIANTS.md`](SECURITY_INVARIANTS.md) (the "Webhook
Notifications (outbound egress)" invariant) and the corresponding
[`SECURITY.md` → Webhook Notifications (outbound egress)](../SECURITY.md#webhook-notifications-outbound-egress)
test-enforcement rows for the full, test-backed claim list. Operator-relevant
summary:

- **SSRF stance — LAN allowed by design.** The webhook URL is fully
  owner-controlled, and self-hosted ntfy/Gotify/Apprise instances commonly live
  on the same LAN as the Ovumcy host. Private, loopback, and link-local
  addresses are **allowed by default** so that setup works out of the box.
  Instead of blocking the destination, the request envelope itself is
  hardened: a 10-second hard timeout, no connection keep-alive/pooling, zero
  redirects (a 3xx response can never steer the request or its body to a
  second, unvalidated origin), a capped response read (a hostile endpoint
  cannot force an unbounded body read), and `http`/`https` schemes only.
- **Optional hardening.** Set `WEBHOOK_BLOCK_PRIVATE_ADDRESSES=true` (default:
  `false`) to refuse delivery to loopback/private/link-local **IP literal**
  targets. Leave it unset/`false` for the common self-hosted-on-LAN case (a
  webhook URL like `http://ntfy.local` or `http://192.168.1.20:8080/...`).
  Turn it on only if your threat model specifically requires blocking
  private-network egress from the notify pass (for example, a shared or
  less-trusted host). Note this check matches IP address literals in the URL
  host, not resolved hostnames.
- **Host-only logging.** Every log line the delivery path emits — success,
  failure, or skip — includes at most the destination **hostname**, never the
  full URL, path, query string, or userinfo. This matters because a webhook
  URL for a service like ntfy commonly embeds an access token in the userinfo
  or query string; that token is never written to a log or printed by the CLI.
- **Disclaimer in every payload.** Every delivered JSON body includes a
  `disclaimer` field carrying the exact medical-safety string shown elsewhere
  in the app (i18n key `dashboard.prediction_disclaimer`): *"These are
  estimates, not medical advice or a method of contraception."* This is
  enforced by `TestNotifyDisclaimerPresentInEveryPayload`.
- **URL encrypted at rest.** `users.webhook_url` is stored as AES-256-GCM
  ciphertext (`security.EncryptField`), bound via additional authenticated
  data to the owning user's id, exactly like `users.totp_secret`. If
  `SECRET_KEY` is rotated, existing stored URLs can no longer be decrypted;
  the notify pass fails safe and skips that owner (no delivery to a garbage
  target) until the owner re-saves their URL under the new key. See the
  *SECRET_KEY Usage Map* in [`SECURITY.md`](../SECURITY.md) for the full
  rotation impact table.
- **No secrets in the payload or the CLI's own output.** The JSON payload
  carries only a title, message, the disclaimer, the reminder type, the
  estimated event date, and the lead-day count — never the webhook URL, never
  `SECRET_KEY`, never a health specific beyond the single estimated date. The
  `ovumcy notify` command's stdout report is similarly limited to counts and
  owner ids.

### Payload shape

```json
{
  "title": "Period reminder",
  "message": "Estimated next period around 2026-07-14.",
  "disclaimer": "These are estimates, not medical advice or a method of contraception.",
  "type": "period-soon",
  "event_date": "2026-07-14",
  "lead_days": 3
}
```

`type` is machine-readable (`period-soon` or `ovulation-soon`) so a downstream
consumer (an ntfy topic rule, a Gotify filter, a home-automation flow) can
route on it without parsing `message`. `disclaimer` is present on every
payload, unconditionally.

## Related documentation

- [`docs/self-hosted.md`](self-hosted.md) — the broader operator guide
  (deployment, environment variables, backups); see its "Advanced knobs"
  section for where `TZ` and `WEBHOOK_BLOCK_PRIVATE_ADDRESSES` fit into the
  rest of the environment surface.
- [`docs/SECURITY_INVARIANTS.md`](SECURITY_INVARIANTS.md) and
  [`SECURITY.md`](../SECURITY.md) — the full, test-backed security invariant
  list, including the webhook egress hardening this document summarizes.
- [`docs/cycle-prediction.md`](cycle-prediction.md) — how the underlying
  period/ovulation estimates are computed; the same math and the same
  medical-safety framing apply to webhook reminders as to the in-app dashboard
  banner.
