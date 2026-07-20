# Data Inventory, Retention & Deletion

_Part of the [Ovumcy security policy](../../SECURITY.md)._

## Data Inventory

What Ovumcy persists per account and per record. All storage is in the operator's configured SQLite file or Postgres database; nothing is sent to any external service unless the owner explicitly enables an integration (OIDC sign-in, or webhook reminders — see [docs/notifications.md](../notifications.md)). This sentence is the canonical egress statement; README and the GDPR guide defer to it.

**`users`** — one row per account:

- Identity: `id`, `email` (unique), `display_name`, `role` (default `owner`), `created_at`.
- Credentials: `password_hash` (bcrypt cost 12), `recovery_code_hash` (bcrypt cost 12), `local_auth_enabled`, `auth_session_version`, `must_change_password`.
- Onboarding: `onboarding_completed`.
- Cycle preferences: `cycle_length`, `period_length`, `luteal_phase`, `auto_period_fill`, `irregular_cycle`, `unpredictable_cycle`, `age_group`, `usage_goal`, `last_period_start`, `long_period_warning_cycle_start`.
- Tracking preferences: `track_bbt`, `temperature_unit`, `track_cervical_mucus`, `hide_sex_chip`, `hide_cycle_factors`, `hide_notes_field`, `show_historical_phases`, `shown_period_tip`.
- 2FA: `totp_enabled`, `totp_secret` (AES-256-GCM aad-bound under an HKDF-derived key, see *Field-Level Encryption*), `totp_last_used_step` (RFC 6238 replay floor).

**`daily_logs`** — one row per (user, calendar day). Dates are stored as UTC midnight and rendered in the user's timezone at read time.

- Period: `is_period`, `cycle_start`, `is_uncertain`, `flow` (`none|spotting|light|medium|heavy`).
- Wellbeing: `mood` (signed scale), `sex_activity` (`none|protected|unprotected`), `bbt` (nullable float, unit selected per account; NULL = not measured), `cervical_mucus` (`none|dry|moist|creamy|eggwhite`), `pregnancy_test` (`none|negative|positive`).
- `cycle_factor_keys` (JSON list), `symptom_ids` (JSON list, references owner-managed symptoms), `notes` (free text).
- The string value domains listed above (`flow`, `sex_activity`, `cervical_mucus`, `pregnancy_test`) are normalized and validated in `internal/services`, not by DB `CHECK` constraints — the columns are plain `TEXT`. (An early `flow` `CHECK` omitted `spotting` and was dropped in migration 003.)

**`symptom_types`** — owner-managed symptom catalog with archive support: `name`, `icon`, `color`, `is_builtin`, soft-archive flag.

**`oidc_identities`** — populated only when OIDC is enabled. `(issuer, subject) → user_id` link plus `created_at`, `last_used_at`. Rows carry `FOREIGN KEY ... ON DELETE CASCADE` on `users.id`.

**`oidc_logout_states`** — short-lived per-session OIDC end-session metadata (`end_session_endpoint`, `id_token_hint`, `post_logout_redirect_url`). Keyed by `session_id` with `expires_at`. Rows are TTL-bounded, not user-id-bounded.

**`register_pickup_tokens`** — opaque single-use nonces for the post-register pickup flow. 5-minute TTL. Rows are not foreign-keyed; they expire and become unreachable on their own.

**Not stored**: analytics, telemetry, third-party identifiers, advertising attribution, error reports, or per-action audit history. Per-action security-event logging is **off by default** and can be toggled per deployment via `AUDIT_LOG_ENABLED` — see *Logging Policy* below.

## Retention and Deletion

**`POST /api/v1/users/current/data-wipe`** (`Settings → Clear data`) wipes per-account health data while keeping the account active:

- Deletes every `daily_logs` row for the user.
- Deletes every user-defined row in `symptom_types` (built-in symptoms remain).
- Resets cycle and tracking preferences to documented defaults.
- Atomically bumps `auth_session_version`, invalidating every other auth cookie for the account. The originating device is re-issued a fresh cookie inline so the user stays signed in there.

`clear-data` does **not** touch email, password hash, recovery code hash, role, display name, OIDC identity links, TOTP state, or onboarding status.

**`DELETE /api/v1/users/current`** removes the account entirely:

- Deletes every `daily_logs` row for the user.
- Deletes every `symptom_types` row for the user (including built-ins).
- Deletes every `oidc_identities`, `register_pickup_tokens`, and `oidc_logout_states` row for the user explicitly, then deletes the `users` row itself. `oidc_identities` also carries `ON DELETE CASCADE`, but the deletion is performed explicitly so erasure stays complete even if foreign-key enforcement is ever disabled, and so `register_pickup_tokens` (which has no foreign key) is removed rather than left to expire. `oidc_logout_states` gained a `user_id` column in migration 031; rows written before it have a NULL `user_id` and age out via their TTL instead of the explicit delete.

The `oidc_logout_states` table is not joined to `users.id`; it carries only a short-lived provider-driven logout reference that becomes unreachable and is pruned on its own TTL.

Both operations require the current password through `validateSettingsActionPassword`. OIDC-only accounts must enrol a local password through the step-up re-auth flow before either danger-zone action becomes available.
