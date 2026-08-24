# Logging Policy

_Part of the [Ovumcy security policy](../../SECURITY.md)._

## Logging Policy

Ovumcy does **not** emit per-action audit logs by default. The `AUDIT_LOG_ENABLED` environment variable controls the audit-event stream:

- `AUDIT_LOG_ENABLED=false` (default) — the runtime emits no `security event:` lines. Everything else keeps writing: Go panics, startup configuration errors and warnings, the Fiber request log, and a small set of operational diagnostics described below.
- `AUDIT_LOG_ENABLED=true` — the runtime emits per-action security-event lines to stderr via the Go standard `log` package. Each line includes the action name, outcome, request method, **sanitized** request path (concrete date parameters are replaced with `:date` and other identifiers are similarly masked), response format, and — for authenticated requests — `user_id` and role. Example:

  ```
  security event: action="health.day_upsert" outcome="success" method="POST"
                  path="/api/v1/days/:date" format="json" user_id="42"
                  role="owner" domain="health_data" target="day_entry"
  ```

  An action that changes health data carries the two further fields shown above: `domain="health_data"`, and a `target` naming the scope it touched (`day_entry`, `symptom`, `cycle_settings`, `reminder_settings`, `timezone`, and — for the two actions that erase everything — `account_data` for a full data wipe, `account` for an account deletion). The target is a fixed designator chosen per action, never an identifier, an email, or submitted text, so an incident review can filter on `domain="health_data"` to select the data-changing actions, erasures included.

  The filter is a statement about the **effect** of an action, not about the sensitivity of the value submitted. Two settings therefore carry it although neither value is itself an observation: the reminder lead window (`settings.reminders_update`) decides when a cycle prediction is announced, including off-host through the webhook, and the stored timezone (`settings.timezone_update`) is the zone the request-free passes resolve "today" in, so changing it re-dates which calendar day a predicted period falls on without a single day record changing. For the same reason the filter selects onboarding: `onboarding.cycle_start_update` and `onboarding.cycle_update` write the same `users` columns the settings cycle form writes, and `onboarding.complete` seeds the first period's day records, so "were the cycle settings changed?" is answerable whichever surface the owner used.

  A mutation that changes the account record without touching the cycle record is audited under `domain="account"` instead — today that is `settings.profile_update` (`target="profile"`), the display name. It is deliberately outside `domain="health_data"`: nothing in the cycle math reads a display name, and a filter that selected it would stop meaning what it says. Two state-changing endpoints emit no mutation event at all because neither persists anything to the account: `PATCH /api/v1/users/current/interface` (language is a cookie, theme is client-side storage) and `POST /lang` (the language cookie alone).

  An action that carries health data **out** of the instance is tagged the same way but under a **separate** domain, `domain="health_egress"`:

  ```
  security event: action="data.export" outcome="success" method="GET"
                  path="/api/v1/exports/csv" format="html" user_id="42"
                  role="owner" domain="health_egress" export_format="csv"
                  target="account_data"
  ```

  This is a third distinct value rather than a modifier on `health_data`, for the same reason `account` is one: each domain answers a different question, and a domain that answers two answers neither precisely. `domain="health_data"` answers *was tracked data changed or destroyed?* and keeps selecting exactly the actions it selected before this split — an erasure review must not start collecting every routine CSV download. `domain="health_egress"` answers *did any tracked data leave, from which account, and in what form?*. A review that wants both health classes at once matches the shared prefix in one clause (`domain="health_`).

  Three actions are tagged as egress today:

  | Action | `target` | Emitted when |
  | --- | --- | --- |
  | `data.export` | `account_data` | A CSV, JSON or summary export is served, refused, or fails. `export_format` (`csv` / `json` / `summary`) is on **every** outcome, including a request refused before any data is read, so a rejected download is still attributable to the format that was asked for. |
  | `settings.calendar_feed_reveal` | `calendar_feed` | The one-time page shows the `.ics` subscribe URL. |
  | `auth.recovery_code_reveal` | `recovery_code` | A recovery code is displayed — on the dedicated page or in the register page's inline block; the sanitized `path` distinguishes the two. |

  Two properties hold for every egress line. The event records the **fact** of the disclosure, never the value: no subscribe URL, no feed token, and no recovery code ever reaches a log line. And the audited moment is the one where the data or the secret reaches a person — which is why the subscribe URL is audited when it is revealed and **not** on each later poll of `GET /calendar/feed/:token.ics`; the reasoning for that boundary is recorded under *Calendar-feed polling is not audited* in [Known Information Disclosure](known-disclosures.md).

When enabled, these lines are visible to the operator through their container runtime (`docker compose logs`, journald, etc.) and never leave the host. They are intended for ad-hoc incident investigation — for example, to confirm whether a suspected compromise produced state-changing requests, and from which `user_id`. The audit stream is not designed as a compliance audit trail; nothing in Ovumcy itself ships, archives, or rotates these lines.

If you enable `AUDIT_LOG_ENABLED=true`, plan retention and access control around the persistent-identifier content (`user_id`, role). Treat the resulting log stream as the same sensitivity class as the database itself.

The Fiber request log (`time | status | latency | method | path | error`) is independent of `AUDIT_LOG_ENABLED` and remains enabled in all configurations. It does not include `user_id` or authenticated-session metadata. Both of its potentially sensitive columns are sanitized before they are written: the path through `SafeRequestLogPath` (route template, opaque tokens masked) and the trailing error through `SafeLogError`, which masks emails, opaque tokens, recovery codes and submitted one-time codes so a handler error string cannot carry an account identifier or a credential into the log. Short secrets are matched by shape rather than by length — a recovery code and a six-digit code are both too short for the opaque-token rule — so that dates, status codes, route templates and ordinary identifiers stay readable for diagnosis.

The startup banner reflects the current setting (`audit_log=true|false`) so operators can confirm the effective configuration on each boot.

## Operational diagnostics (always on)

A handful of lines outside both streams above report that an internal operation could not complete. They are always enabled, and **five of them name an owner by `user_id`**, so `AUDIT_LOG_ENABLED=false` does not mean "no account identifier ever reaches the log":

- the derived-cycle refresh that follows a day write, when it cannot load that owner's logs or store the recomputed luteal phase (`refreshDerivedCycleSettings: … for user N failed: …`) — the only two lines here that also carry the raw driver error, which does not pass through `SafeLogError`;
- the webhook reminder pass, when an owner's stored endpoint cannot be decrypted, their logs cannot be loaded, or the sent-watermark write fails after delivery (`webhook notify: … owner id=N`);
- the flash cookie, when the message that explains an error cannot be encoded or sealed (`flash cookie: sealed write failed: …`) — the request still redirects, so without this line an operator would see only users reporting unexplained redirects. It names no account: the reason only, through `SafeLogError`, never the payload it was carrying.

None of them carries health data, an email, a URL, or a token — the identifier and the reason only. The first pair is a symptom of write contention (see *Concurrency on the SQLite baseline* in [docs/self-hosted.md](../self-hosted.md#concurrency-on-the-sqlite-baseline)); the second is how a `SECRET_KEY` rotation surfaces for webhook owners, and is the signal the reminder pass's own summary does not carry.

If your deployment treats any `user_id` in a log as sensitive, plan retention and access control for the default stream too, not only for the audit stream.
