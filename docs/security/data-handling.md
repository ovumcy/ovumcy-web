# Data Inventory, Retention & Deletion

_Part of the [Ovumcy security policy](../../SECURITY.md)._

## Data Inventory

What Ovumcy persists per account and per record. All storage is in the operator's configured SQLite file or Postgres database; nothing is sent to any external service unless the owner explicitly enables an integration (OIDC sign-in, or webhook reminders — see [docs/notifications.md](../notifications.md)). This sentence is the canonical egress statement; README and the GDPR guide defer to it.

**`users`** — one row per account:

- Identity: `id`, `email` (unique), `display_name`, `role` (default `owner`), `created_at`.
- Credentials: `password_hash` (bcrypt cost 12), `recovery_code_hash` (bcrypt cost 12), `local_auth_enabled`, `auth_session_version`, `must_change_password`.
- Onboarding: `onboarding_completed`.
- Cycle preferences: `cycle_length`, `period_length`, `luteal_phase`, `auto_period_fill`, `irregular_cycle`, `unpredictable_cycle`, `age_group`, `usage_goal`, `last_period_start`, `long_period_warning_cycle_start`.
- Tracking preferences: `track_bbt`, `temperature_unit`, `track_cervical_mucus`, `hide_sex_chip`, `hide_cycle_factors`, `hide_notes_field`, `show_historical_phases`, `shown_period_tip`, `week_starts_on`.
- Interface: `timezone` — the last known IANA zone name observed on a request, used to resolve "today" for date-only writes. Not a secret. `interface_language` — the UI language the owner chose explicitly in `Settings` (migration 034), re-issued as the `ovumcy_lang` cookie on every sign-in so a fresh browser keeps it. Empty means never chosen, in which case the language is negotiated per request as before. Not a secret.
- 2FA: `totp_enabled`, `totp_secret` (AES-256-GCM aad-bound under an HKDF-derived key, see *Field-Level Encryption*), `totp_last_used_step` (RFC 6238 replay floor).
- Webhook reminders (only meaningful once the owner enables them): `webhook_enabled`, `webhook_url` (**AES-256-GCM aad-bound under an HKDF-derived key, the same field-encryption path as `totp_secret`** — it is an owner-chosen egress destination, not a display value), `webhook_notify_period`, `webhook_notify_ovulation`, `reminder_lead_days`, and the per-kind send watermarks `webhook_period_last_sent_cycle_start` / `webhook_ovulation_last_sent_cycle_start` that stop a reminder firing twice for one cycle.
- Calendar (`.ics`) feed subscription (only populated once the owner generates a feed): `calendar_feed_selector` — the non-secret lookup half of the capability token — plus `calendar_feed_verifier_mac` (keyed HMAC-SHA256 under a `SECRET_KEY`-derived label, the value the endpoint actually compares) and the legacy `calendar_feed_verifier_hash` (bcrypt, still written for rollback and still accepted for rows created before migration 032). This is the one sanctioned bearer-token surface; see *Calendar feed subscription* in `SECURITY.md`.

**`daily_logs`** — one row per (user, calendar day). Dates are stored as UTC midnight and rendered in the user's timezone at read time.

- Period: `is_period`, `cycle_start`, `is_uncertain`, `flow` (`none|spotting|light|medium|heavy`).
- Wellbeing: `mood` (signed scale), `sex_activity` (`none|protected|unprotected`), `bbt` (nullable float, unit selected per account; NULL = not measured), `cervical_mucus` (`none|dry|moist|creamy|eggwhite`), `pregnancy_test` (`none|negative|positive`).
- `cycle_factor_keys` (JSON list), `symptom_ids` (JSON list, references owner-managed symptoms), `notes` (free text).
- The string value domains listed above (`flow`, `sex_activity`, `cervical_mucus`, `pregnancy_test`) are normalized and validated in `internal/services`, not by DB `CHECK` constraints — the columns are plain `TEXT`. (An early `flow` `CHECK` omitted `spotting` and was dropped in migration 003.)

**`symptom_types`** — owner-managed symptom catalog with archive support: `name`, `icon`, `color`, `is_builtin`, soft-archive flag.

**`oidc_identities`** — populated only when OIDC is enabled. `(issuer, subject) → user_id` link plus `created_at`, `last_used_at`. Rows carry `FOREIGN KEY ... ON DELETE CASCADE` on `users.id`.

**`oidc_logout_states`** — short-lived per-session OIDC end-session metadata (`end_session_endpoint`, `id_token_hint`, `post_logout_redirect_url`). Keyed by `session_id` with `expires_at`, set to a hard 7-day TTL (`defaultOIDCLogoutStateTTL`, `internal/services/oidc_logout_state_service.go`) regardless of what the provider requests. Gained a `user_id` column in migration 031 (not foreign-keyed) so every row can be resolved to its owner; migration 033 deleted the unattributed rows written before 031, so no NULL-`user_id` row exists. Every row still ages out on that TTL.

**`register_pickup_tokens`** — opaque single-use nonces for the post-register pickup flow, carrying a required `user_id` (not foreign-keyed) since creation. 5-minute TTL; rows also expire and become unreachable on their own.

**`app_state`** — process-level key/value operational bookkeeping (migration 028), not scoped to any `user_id` and holding no personal or health data. Its keys record things like the built-in reminder scheduler's last-completed-run date (restart safety and current-day catch-up); the table is a general-purpose store, so further operational keys land here as they appear.

**Not stored**: analytics, telemetry, third-party identifiers, advertising attribution, error reports, or per-action audit history. Per-action security-event logging is **off by default** and can be toggled per deployment via `AUDIT_LOG_ENABLED` — see *Logging Policy* below.

## Retention and Deletion

**`POST /api/v1/users/current/data-wipe`** (`Settings → Clear data`) wipes per-account health data while keeping the account active:

- Deletes every `daily_logs` row for the user.
- Deletes every user-defined row in `symptom_types` (built-in symptoms remain).
- Resets cycle and tracking preferences to documented defaults.
- **Disarms webhook reminders**: clears `webhook_enabled`, blanks the encrypted `webhook_url`, resets `reminder_lead_days` and the per-kind opt-ins to their defaults, and clears both send watermarks so no reminder fires against the freshly emptied account.
- **Revokes the calendar feed**: NULLs `calendar_feed_selector` and both verifier columns, so a subscribe URL issued earlier stops resolving and answers `404`. Calendar clients holding it need a fresh URL from Settings.
- Atomically bumps `auth_session_version`, invalidating every other auth cookie for the account. The originating device is re-issued a fresh cookie inline so the user stays signed in there.

`clear-data` does **not** touch email, password hash, recovery code hash, role, display name, OIDC identity links, TOTP state, onboarding status, or the interface language (`interface_language`) — the language the owner reads the product in is not part of the health record, and resetting it would answer a wipe by switching the interface back to the operator default. Account deletion removes the row, and with it the column.

**`DELETE /api/v1/users/current`** removes the account entirely:

- Deletes every `daily_logs` row for the user.
- Deletes every `symptom_types` row for the user (including built-ins).
- Deletes every `oidc_identities`, `register_pickup_tokens`, and `oidc_logout_states` row for the user explicitly, then deletes the `users` row itself. `oidc_identities` also carries `ON DELETE CASCADE`, but the deletion is performed explicitly so erasure stays complete even if foreign-key enforcement is ever disabled, and so `register_pickup_tokens` (which has no foreign key) is removed rather than left to expire. `oidc_logout_states` gained a `user_id` column in migration 031, and migration 033 deleted the unattributed rows written before it — every row is attributable, so this explicit per-user delete covers the whole table.

Rows written before migration 031 carried a NULL `user_id` the per-user delete could never match; migration 033 removed them, so no unattributed row remains. Live rows are additionally bounded by their own 7-day TTL.

Both operations require fresh re-authentication. An account with a local password confirms through `validateSettingsActionPassword`. An account provisioned through OIDC has no password to confirm with, so it confirms at the provider instead: `POST /api/v1/users/current/data-wipe/step-up` and `POST /api/v1/users/current/deletion/step-up` mint a purpose-bound step-up, and the erasure runs in the `/auth/oidc/callback` that returns from it. The confirmed operation rides in the sealed step-up state, not in the callback request. An account that *has* a local password is refused those endpoints, so the SSO route can never stand in for a password gate that applies.
