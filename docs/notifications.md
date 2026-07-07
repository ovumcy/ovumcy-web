# Reminders & Notifications

Ovumcy can remind an owner about an upcoming period or ovulation through three
independent, self-hosted channels: an in-app dashboard banner, an outbound
webhook (for example a self-hosted [ntfy](https://ntfy.sh/) or
[Gotify](https://gotify.net/) instance), and a private, read-only calendar
(`.ics`) subscription. All three read the same underlying prediction and the
same per-owner lead-time setting. There is no third-party notification service
involved in any of them: this is a zero-cost, fully self-hosted notification
model, consistent with Ovumcy's single-tenant, operator-controlled design.

> [!IMPORTANT]
> Every reminder is an estimate, never a fact. The in-app banner, every
> webhook payload, and the calendar feed all carry the same medical-safety
> framing shown elsewhere in the app:
> **"These are estimates, not medical advice or a method of contraception."**

This document covers all three channels: what each one is, how an owner
enables it, and — for the webhook channel, which needs a scheduled delivery
pass — how an operator wires that up.

## Contents

- [1. In-app dashboard banner](#1-in-app-dashboard-banner)
- [2. Webhook notifications](#2-webhook-notifications)
- [3. Calendar (.ics) subscription](#3-calendar-ics-subscription)
- [Related documentation](#related-documentation)

## 1. In-app dashboard banner

When a period or ovulation is predicted soon, the dashboard shows a small
reminder banner ("Period likely today", "…likely tomorrow", or "…likely in
~N days"). This needs no setup beyond normal cycle tracking — it is on by
default for any owner with enough logged data to produce a single-date
estimate.

**How to configure it:** on the Settings page, under **Reminders**, the
**Reminder lead time (days)** field controls how many days ahead of the
estimated date the banner starts showing.

- Range: **0–14** days. A value outside that range is clamped, not rejected.
- Default: **3** days.
- 0 means "only show it on the day itself."
- This setting is **shared** with the webhook channel below — one lead-time
  value drives both the dashboard banner and the webhook "notify before"
  timing. There is no separate lead time for webhooks.

The banner never shows a prediction that is itself suppressed or uncertain —
for example while cycle length is still unpredictable, or when the dashboard
is already displaying a date range instead of an exact date — so it never
contradicts the main prediction surface.

Code references verified for this section: `internal/api/handlers_settings_reminders.go`
(`PATCH /api/v1/users/current/reminders`, `OwnerOnly` + CSRF-protected, form
field `reminder_lead_days`), `internal/services/webhook_settings_service.go`
(`MinReminderLeadDays`/`MaxReminderLeadDays`/`NormalizeReminderLeadDays`),
`internal/models/user.go` (`DefaultReminderLeadDays = 3`, column
`reminder_lead_days`), and `internal/services/dashboard_reminder_banner.go`
(banner threshold policy).

## 2. Webhook notifications

Ovumcy can POST a small JSON message to a webhook URL the owner controls when
a period or ovulation reminder becomes due — the same prediction and the same
lead-time setting the dashboard banner uses.

### How to enable it

On the Settings page, under **Webhook reminders**:

1. Enter your webhook URL (for example a self-hosted ntfy topic URL or a
   Gotify endpoint) in the **Webhook URL** field. This field is **write-only**:
   once saved, it always renders blank — the stored value is never redisplayed
   in the browser, even to the owner who set it. Leave it blank on a later save
   to keep the current endpoint unchanged; use the dedicated **Remove saved
   URL** checkbox to clear it.
2. Turn on **Enable webhook reminders**. A URL must already be configured (or
   supplied in the same save) to enable delivery.
3. Choose **Notify before period** and/or **Notify before ovulation**
   independently — each is its own on/off switch.
4. Save. The URL is validated as an absolute `http`/`https` address and is
   stored **encrypted at rest** (AES-256-GCM, bound to your account) — a
   ntfy/Gotify access token embedded in the URL is protected the same way.

The same settings can also be read and changed from the local operator CLI
(`ovumcy webhook show|set <email> ...`) — see
[Configuring webhook settings from the CLI](#configuring-webhook-settings-from-the-cli)
below. That path is for an operator managing an account from the server shell,
not a replacement for the Settings-page form.

### How delivery works

Delivery is a **request-free batch pass**: at pass time, Ovumcy looks at every
owner, decides whose reminders are due, and POSTs each due reminder to that
owner's configured webhook. It is **not** a request-triggered notification —
nothing fires from a page load. Two independent ways to run that pass:

- **`ovumcy notify`** — a CLI subcommand an operator schedules externally
  (cron, systemd timer, a Docker one-shot, or Windows Task Scheduler). Off by
  default in the sense that nothing runs until you schedule it.
- **The built-in daily scheduler** (optional, off by default) — an in-process
  goroutine that runs the identical batch pass automatically once a day, with
  no external scheduler required. See
  [The built-in daily scheduler](#the-built-in-daily-scheduler) below.

Use whichever fits your deployment: the CLI pass if you already manage cron/
systemd/Task Scheduler for this host, or the built-in scheduler if you would
rather not maintain an external schedule at all. Running both is unnecessary
but harmless (delivery is idempotent — see
[Idempotency and safety](#idempotency-and-safety)).

#### The `ovumcy notify` CLI

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

##### Cron

Add a line to the crontab of the user that has access to the Ovumcy binary,
database path, and `SECRET_KEY` (or `SECRET_KEY_FILE`):

```cron
# Run the Ovumcy webhook notify pass daily at 09:00 in the server's local time.
0 9 * * * SECRET_KEY_FILE=/etc/ovumcy/secret_key DB_DRIVER=sqlite DB_PATH=/var/lib/ovumcy/ovumcy.db /usr/local/bin/ovumcy notify >> /var/log/ovumcy-notify.log 2>&1
```

Adjust `DB_DRIVER`/`DB_PATH` (or `DATABASE_URL` for Postgres) and the secret
source to match your deployment's actual environment.

##### systemd timer

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

##### Docker (one-shot against the compose service)

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

##### Windows Task Scheduler

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

##### Recommended cadence

Once daily, at a fixed local hour, across all of the above. There is no
supported sub-daily interval requirement — the shared reminder lead-time
setting already gives several days of lead time, so a once-a-day pass is
sufficient to catch every due reminder before the event. Running it more than
once a day is harmless (idempotent — see below) but unnecessary.

#### The built-in daily scheduler

Instead of (or in addition to) scheduling `ovumcy notify` externally, the
server binary can run the identical batch pass itself, once a day, with no
cron/systemd/Task Scheduler needed. It is **opt-in and off by default** — a
brand-new deployment makes no outbound webhook calls until you turn it on.

| Variable | Default | Meaning |
| --- | --- | --- |
| `REMINDER_SCHEDULER_ENABLED` | `false` | Turns the built-in scheduler on. When `false` (the default), no scheduler goroutine, timer, or outbound component exists in the running process at all. |
| `REMINDER_SCHEDULER_HOUR` | `9` | The **local hour of day** (0–23) the daily pass runs at, in the server's configured timezone (`TZ`; see [Timezone behavior](#timezone-behavior)). There is no separate scheduler timezone. |

Set both as regular environment variables (`.env` for Docker, or your
process/unit environment for a direct binary):

```env
REMINDER_SCHEDULER_ENABLED=true
REMINDER_SCHEDULER_HOUR=9
```

Notes:

- This is an **always-on** component once enabled: it makes outbound webhook
  calls from the running server process every day at the configured hour, for
  as long as the process runs — there is no separate "pause" toggle short of
  setting `REMINDER_SCHEDULER_ENABLED=false` and restarting. Startup logs a
  clear one-line note whenever it is on, naming the hour and timezone.
- Delivery still requires each owner to have their own webhook configured and
  enabled in Settings — turning the scheduler on does not itself send
  anything to an owner who hasn't set up a webhook.
- If the process was down when the scheduled hour passed, it catches up with
  **at most one pass for the current day** on the next start — it never
  backfills multiple missed days.
- On graceful shutdown (`SIGINT`/`SIGTERM`), the server waits briefly for an
  in-flight pass to finish before closing the database.
- It reuses the exact same delivery path, idempotency watermark, and security
  hardening as the `ovumcy notify` CLI (below) — running both is unnecessary
  but not harmful.

Code reference verified for this subsection: `cmd/ovumcy/main.go`
(`reminderSchedulerSettings`, `startReminderScheduler`,
`REMINDER_SCHEDULER_ENABLED`/`REMINDER_SCHEDULER_HOUR` env resolution) and
`internal/reminders/scheduler.go` (catch-up, panic isolation, drain-on-shutdown
behavior).

#### Configuring webhook settings from the CLI

An operator with local shell access to the instance can also inspect or set an
owner's webhook configuration directly, without going through the browser:

```
usage: ovumcy webhook <show|set> <email> [--enabled=<bool>] [--notify-period=<bool>] [--notify-ovulation=<bool>] [--reminder-lead-days=<0-14>] [--url-stdin] [--clear-url] [--dry-run]
```

- `ovumcy webhook show <email>` prints the owner's current settings: whether
  delivery is enabled, the endpoint **host only** (never the full URL or any
  embedded token), and the notify-period/notify-ovulation toggles.
- `ovumcy webhook set <email> --enabled=true --notify-period=true --url-stdin`
  configures settings. Boolean flags take an explicit `=true`/`=false` value so
  an unspecified flag leaves that setting untouched.
- The webhook URL is a **secret** (it can embed an ntfy/Gotify access token)
  and is deliberately **never accepted as a command-line argument** — argv
  leaks into shell history and process listings on a shared host. Supply it
  instead via the `OVUMCY_WEBHOOK_URL` environment variable for the single
  invocation, or interactively with `--url-stdin` (a no-echo terminal prompt,
  or the first line of piped stdin). It is never echoed back.
- `--clear-url` removes any stored endpoint; `--dry-run` validates and prints
  the result without writing anything.

This CLI path and the Settings-page form write to the exact same columns —
either is fine for a self-hosted single-owner setup, and the CLI is
particularly useful for scripted provisioning (for example, setting a default
webhook as part of an install script that also runs `ovumcy users create`).

### Idempotency and safety

- Each reminder kind (period, ovulation) has its own **watermark**, storing
  the cycle-start anchor date the reminder was last successfully sent for.
- The watermark advances **only after a successful (2xx) delivery**. A failed
  delivery (timeout, non-2xx, refused redirect, connection error) leaves the
  watermark untouched.
- Consequence: re-running the pass is always safe.
  - A reminder already delivered this cycle is not sent again.
  - A reminder whose delivery failed last time is retried automatically on the
    next pass, with no separate retry mechanism to configure — the schedule
    itself **is** the retry loop.
- This means you can run the pass (or the built-in scheduler) on an ordinary
  daily schedule and never worry about double-notifying an owner because a
  previous run overlapped, was re-triggered, or ran twice due to a scheduler
  misconfiguration.
- A pass never fails all owners because of one bad owner: a decrypt failure, a
  load failure, or a delivery failure for one owner is logged (owner id and
  host only) and the pass continues to the next owner.

### Timezone behavior

Each owner's "today" is evaluated in:

1. **The owner's own persisted timezone**, if one is set (captured
   automatically from the owner's browser) — this is preferred.
2. Otherwise, the **server's local timezone** — resolved from the `TZ`
   environment variable (default `Local`, i.e. whatever the host/container's
   local timezone is) — as a fallback for owners with no persisted timezone.

In a household with owners in different timezones who have each had their
timezone captured, each owner's reminders are decided against their own local
calendar day, not the server's. If you run the pass (or the built-in
scheduler) once daily at a server-local hour and most or all of your owners
have not yet had their timezone captured, that hour is effectively "09:00
server time" for everyone until each owner's browser records its timezone.

### Security notes

Operator-relevant summary (the full, test-backed claim list lives in
[`docs/SECURITY_INVARIANTS.md`](SECURITY_INVARIANTS.md) and
[`SECURITY.md` → Webhook Notifications (outbound egress)](../SECURITY.md#webhook-notifications-outbound-egress)):

- **SSRF stance — LAN allowed by design.** The webhook URL is fully
  owner-controlled, and self-hosted ntfy/Gotify/Apprise instances commonly live
  on the same LAN as the Ovumcy host. Private, loopback, and link-local
  addresses are **allowed by default** so that setup works out of the box.
  Instead of blocking the destination, the request envelope itself is
  hardened: a 10-second hard timeout, no connection keep-alive/pooling, zero
  redirects, a capped response read, and `http`/`https` schemes only.
- **Optional hardening.** Set `WEBHOOK_BLOCK_PRIVATE_ADDRESSES=true` (default:
  `false`) to refuse delivery to loopback/private/link-local **IP literal**
  targets. Leave it unset/`false` for the common self-hosted-on-LAN case (a
  webhook URL like `http://ntfy.local` or `http://192.168.1.20:8080/...`).
  Turn it on only if your threat model specifically requires blocking
  private-network egress from the notify pass. Note this check matches IP
  address literals in the URL host, not resolved hostnames. This same flag
  applies to both the CLI pass and the built-in scheduler.
- **Host-only logging.** Every log line the delivery path emits — success,
  failure, or skip — includes at most the destination **hostname**, never the
  full URL, path, query string, or userinfo.
- **Disclaimer in every payload.** Every delivered JSON body includes a
  `disclaimer` field carrying the exact medical-safety string shown elsewhere
  in the app: *"These are estimates, not medical advice or a method of
  contraception."*
- **URL encrypted at rest.** The stored webhook URL is AES-256-GCM ciphertext,
  bound to the owning user's id, exactly like a TOTP secret. If `SECRET_KEY`
  is rotated, existing stored URLs can no longer be decrypted; delivery fails
  safe and skips that owner (no delivery to a garbage target) until the owner
  re-saves their URL under the new key. See the *SECRET_KEY Usage Map* in
  [`SECURITY.md`](../SECURITY.md) for the full rotation impact table.
- **No secrets in the payload or CLI output.** The JSON payload carries only a
  title, message, the disclaimer, the reminder type, the estimated event date,
  and the lead-day count — never the webhook URL, never `SECRET_KEY`, never a
  health specific beyond the single estimated date.

#### Payload shape

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

## 3. Calendar (.ics) subscription

An owner can generate a private, read-only calendar feed URL and subscribe to
it from any standard calendar app (Google Calendar, Apple Calendar,
Thunderbird, or any client that supports "subscribe by URL"). The feed shows
predicted period and ovulation days and updates automatically each time the
calendar app refreshes it — there is nothing to schedule or run.

### How to enable it

On the Settings page, under **Calendar feed**:

1. Click **Generate feed link**. Ovumcy creates a private subscribe URL and
   shows it to you **exactly once**, on a dedicated confirmation page — copy
   it immediately, since it cannot be redisplayed later.
2. Paste that URL into your calendar app's "subscribe to calendar by URL" (or
   equivalent) feature.
3. If you suspect the URL leaked, click **Rotate link** to mint a fresh URL —
   the previous one stops working immediately. To turn the feed off entirely,
   click **Turn off feed** — any previously issued URL then 404s.

The feed is **read-only**: nothing a calendar app does can write back into
Ovumcy through it. It is scoped to the single owner who generated it, exactly
like every other per-day and per-account resource in Ovumcy.

### Security rationale (brief)

The subscribe URL itself is the credential — a calendar client sends no
session cookie — so it is treated as a bearer capability token, not a normal
authenticated resource: it is generated with cryptographic randomness, stored
only as a hashed verifier (never recoverable from the database), shown to the
owner exactly once, and revocable at any time by rotating or turning the feed
off. The full rationale and test-backed invariants live in
[`docs/SECURITY_INVARIANTS.md`](SECURITY_INVARIANTS.md) under **Calendar feed
subscription** — see that section for the complete, current detail rather than
this summary.

Code references verified for this section: `internal/api/handlers_calendar_feed.go`
(`GET /calendar/feed/:token.ics`, no auth cookie, 404-no-oracle),
`internal/api/handlers_settings_calendar_feed.go` (`POST`/`DELETE
/api/v1/users/current/calendar-feed`, `POST .../calendar-feed/rotate`, all
`OwnerOnly` + CSRF-protected; one-time reveal page at `/settings/calendar-feed`),
and `internal/services/calendar_feed_settings_service.go` (generate/rotate/
revoke lifecycle).

## Related documentation

- [`docs/self-hosted.md`](self-hosted.md) — the broader operator guide
  (deployment, environment variables, backups); see its "Advanced knobs"
  section for where `TZ`, `WEBHOOK_BLOCK_PRIVATE_ADDRESSES`, and the built-in
  scheduler's environment variables fit into the rest of the environment
  surface.
- [`docs/SECURITY_INVARIANTS.md`](SECURITY_INVARIANTS.md) and
  [`SECURITY.md`](../SECURITY.md) — the full, test-backed security invariant
  list, including the webhook egress hardening and calendar-feed token model
  this document summarizes.
- [`docs/cycle-prediction.md`](cycle-prediction.md) — how the underlying
  period/ovulation estimates are computed; the same math and the same
  medical-safety framing apply to all three reminder channels.
