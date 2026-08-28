# GDPR Operator Guide

Ovumcy is privacy-critical software that handles sensitive health data (menstrual and reproductive cycle records). When you self-host Ovumcy you act as the **data controller** under EU/UK GDPR for the personal data your instance processes — including your own. This guide maps each obligation onto the technical controls Ovumcy already provides and onto operator actions that the codebase cannot perform for you.

This document is operator guidance, not legal advice. If you self-host Ovumcy for users other than yourself, consult a qualified privacy professional in your jurisdiction.

## Scope

| You are | This guide |
| --- | --- |
| Self-hosting for yourself only | Most obligations are trivially satisfied — you are both controller and data subject. The relevant work is encryption at rest, backup hygiene, and `SECRET_KEY` management. |
| Self-hosting for a household / partner | Add a written agreement covering consent and breach notification expectations. Encryption-at-rest and access control sections are mandatory. |
| Self-hosting for a small group or organisation | All sections apply. You may also need a Data Processing Agreement template, a public privacy notice tailored to your deployment, and a DPIA. |

Ovumcy is single-tenant, single-instance software. Multi-tenant SaaS, organisational identity management, and shared-controller arrangements are explicitly out of scope.

Ovumcy is built for personal and household self-hosting. It is not, by itself, a turnkey GDPR compliance solution for a public multi-user service: consent records (policy version, timestamp, withdrawal), a DPIA, and controller documentation are the operator's responsibility and are not implemented in-app.

## Lawful Basis (Art. 6 + Art. 9)

Ovumcy processes menstrual and reproductive health information. Under Art. 9(1) GDPR this is a **special category** of personal data and requires a specific Art. 9(2) lawful basis on top of Art. 6.

| Operator deployment | Likely Art. 6 basis | Likely Art. 9 basis |
| --- | --- | --- |
| Yourself only | Art. 6(1)(a) consent (to yourself) | Art. 9(2)(a) explicit consent |
| Household / partner | Art. 6(1)(a) consent | Art. 9(2)(a) explicit consent — captured in writing |
| Small group | Art. 6(1)(a) or (b) | Art. 9(2)(a) — explicit, granular, revocable, capturable |

The `/privacy` page renders a public-facing privacy notice that states what is stored and that data leaves the server only through integrations the owner enables; the canonical, complete egress enumeration — including the owner-armed `.ics` calendar feed a third-party calendar client polls — is the Data Inventory preamble of [`docs/security/data-handling.md`](security/data-handling.md#data-inventory). Operators running Ovumcy for anyone other than themselves should layer a deployment-specific privacy notice on top — naming the controller, lawful basis, and Data Subject contact route.

## Data Inventory

The full data inventory lives in [`docs/security/data-handling.md`](security/data-handling.md#data-inventory) under "Data Inventory". The short version:

- `users`: identity, password/recovery hashes, cycle preferences, tracking flags, interface language, optional encrypted TOTP secret.
- `daily_logs`: per-day cycle entries, owner-controlled symptoms, free-text notes.
- `symptom_types`: owner-managed symptom catalogue.
- `oidc_identities`: federated-login link (only when OIDC is enabled).
- Auxiliary short-lived tables: `register_pickup_tokens` carries a required `user_id`; `oidc_logout_states` carries a `user_id` since migration 031, migration 033 purged the NULL rows written before it, and `Save` refuses an unattributed write — so every row is owner-attributed and the explicit per-user delete covers the whole table at the moment erasure is requested. Neither foreign-keys to `users`; every row is TTL-bounded regardless.
- `app_state`: non-personal operational key-value bookkeeping (for example, the built-in reminder scheduler's last-run marker) — not scoped to any user.

**No personal data beyond the above is stored.** No analytics, telemetry, third-party identifiers, advertising attribution, error reports, or persistent per-action audit log. Per-action security-event logging is **off by default** and can be enabled per deployment through `AUDIT_LOG_ENABLED=true`.

## Data Subject Rights (Art. 15-22)

| Right | How Ovumcy supports it | Operator action |
| --- | --- | --- |
| **Access (Art. 15)** | The data subject can view all their records in the dashboard, calendar, and stats pages, and their account profile and cycle/tracking settings in `Settings`. The machine-readable exports (`GET /api/v1/exports/csv`, `GET /api/v1/exports/json`) cover the **day-level health record**: per-day entries, notes, and custom symptoms as logged on an exported day. They do **not** include the account profile (email, display name), cycle/tracking settings, reminder/webhook configuration, or symptoms that were only ever archived and never logged — those remain viewable in the UI. | None required when the subject controls their own login and can read both the export and their `Settings`. For a third-party subject, fulfil within one month by exporting the health record on their behalf **and** separately supplying the profile and settings fields (`Settings`, or `ovumcy users list`). |
| **Rectification (Art. 16)** | Every cycle, day and symptom field is editable from the UI, and so are the display name and the cycle/tracking/reminder/interface settings; the email address is read-only after registration and has no in-app rectification control. Note that hiding a custom symptom in `Settings → Symptoms` (the UI "delete", `DELETE /api/v1/symptoms/{id}`) is an **archive**, not an erasure: the symptom stays restorable and its name persists in the database until `Clear data` or account deletion removes it. | None for in-app fields. An email-rectification request is a manual operator action (direct database update, or erase-and-re-register) — no CLI or UI control exists for it. |
| **Erasure (Art. 17)** | `Settings → Clear data` (`POST /api/v1/users/current/data-wipe`) wipes per-account health records while keeping the account active. `Settings → Delete account` (`DELETE /api/v1/users/current`) removes the account entirely, including built-in and custom symptoms. Every user-scoped row — `daily_logs`, `symptom_types`, `oidc_identities`, `register_pickup_tokens`, `oidc_logout_states` — is deleted **explicitly**, not left to `ON DELETE CASCADE`: foreign-key enforcement is a runtime pragma in SQLite, `register_pickup_tokens` carries no foreign key at all, and erasure has to hold on an instance running with `foreign_keys` off. The cascade declared in migration 014 is defence in depth, never the mechanism. Both flows bump `auth_session_version` and require fresh re-authentication: the current local password, or — for an account provisioned through OIDC, which has no password to confirm with — a fresh purpose-bound step-up at the identity provider. Neither flow is unavailable to a subject for want of a local password. See [`Retention and Deletion`](security/data-handling.md#retention-and-deletion). | Document the contact channel for subjects who cannot self-serve. Schedule a backup purge that respects the deletion (see *Backups*, below). |
| **Restriction (Art. 18)** | Built-in tracking toggles let owners hide sex activity, cycle factors, and notes from new entries. Stored history stays in the database; if a subject demands full processing pause, use `clear-data` or revoke their account credentials. | Manual operator action if a subject requests full restriction beyond UI hiding. |
| **Portability (Art. 20)** | CSV and JSON exports provide the day-level health record (per-day entries, notes, and symptoms logged on those days) in machine-readable form. Account profile and cycle/tracking settings are not part of the export; they are viewable and rectifiable in `Settings`. | None required for the health record. If a portability request also covers profile/settings, supply those separately (`Settings`, or `ovumcy users list`). |
| **Objection (Art. 21)** | Ovumcy does not process data for direct marketing or automated profiling. There are no objection cases that apply technically. | None required. |
| **Automated decision-making (Art. 22)** | Ovumcy's cycle predictions are statistical aggregations of the owner's own logs. They are not used to make decisions producing legal effects on the subject. | Document this clearly in your deployment-specific privacy notice if you carry one. |

## Lawfulness Checks Ovumcy Already Enforces

| Control | File | Test |
| --- | --- | --- |
| Owner-only middleware on every state-mutating `/api/*` endpoint | [`internal/api/routes.go`](../internal/api/routes.go) | [`internal/api/owner_only_coverage_regression_test.go`](../internal/api/owner_only_coverage_regression_test.go) |
| Sealed (AEAD) auth, recovery, reset, OIDC, TOTP, flash and calendar-feed-reveal cookies (12 purposes) | [`internal/api/secure_cookie_codec.go`](../internal/api/secure_cookie_codec.go) | `internal/api/secure_cookie_codec_*_test.go` |
| CSRF protection on every state-changing request | [`cmd/ovumcy/server.go`](../cmd/ovumcy/server.go) | `internal/api/state_mutation_csrf_regression_test.go` |
| Session revocation on credential rotation | [`internal/services/auth_service.go`](../internal/services/auth_service.go) | `internal/api/auth_password_change_session_regression_test.go` and siblings |
| PII never logged | [`internal/api/request_logging.go`](../internal/api/request_logging.go) | `cmd/ovumcy/main_test.go` (`TestAuthRateLimitHandlerLogsSecurityEventWithoutPII`, `TestCSRFMiddlewareErrorHandlerLogsSecurityEventWithoutPII`) |
| TOTP secret encryption at rest | [`internal/security/field_crypto.go`](../internal/security/field_crypto.go) | [`internal/security/field_crypto_test.go`](../internal/security/field_crypto_test.go) |

The full mapping from `SECURITY.md` claims to enforcing tests lives in [`SECURITY.md → Test Enforcement Matrix`](../SECURITY.md#test-enforcement-matrix).

## Encryption at Rest (Art. 32)

Ovumcy encrypts **a specific subset** of database fields at the application layer (TOTP secrets via AES-256-GCM with HKDF-derived keys and per-row AAD binding). **It does not encrypt the database file itself.** This is a deliberate scope choice for a single-tenant self-hosted application; for the operator-as-adversary threat model see [`Threat Model`](security/threat-model.md#threat-model).

For GDPR Art. 32 (security of processing), operators must add filesystem- or volume-level encryption appropriate to their environment:

| Platform | Recommended baseline |
| --- | --- |
| Linux server | LUKS-encrypted block device for `/var/lib/ovumcy` (or wherever `data/` lives). |
| Docker / Compose | Mount the SQLite volume on a host directory that lives on an encrypted block device. The compose stack does not perform encryption itself. |
| Postgres advanced path | Enable Postgres TDE (or the equivalent provided by your distribution); separately encrypt the underlying volume. |
| Backups | The SQLite/Postgres archive **must** be encrypted independently of the host disk. Common choice: `age` or `gpg` with a key kept separate from `SECRET_KEY` and from the data backup itself. |

`COOKIE_SECURE=true` and HTTPS at the reverse proxy are **mandatory** for production; they cover Art. 32 in transit. The OIDC handler refuses to issue its sign-in cookies when `COOKIE_SECURE=false` because those cookies require `SameSite=None`, which is invalid over plain HTTP. The misconfiguration is caught earlier than that, though: with `OIDC_ENABLED=true` and `COOKIE_SECURE=false` the app **refuses to start** (`OIDC_ENABLED=true requires COOKIE_SECURE=true`), so an operator learns at boot rather than when the first subject tries to sign in.

## `SECRET_KEY` Management

`SECRET_KEY` (or the file behind `SECRET_KEY_FILE`) is the single application-wide secret. See [`SECRET_KEY Usage Map`](security/cryptography.md#secret_key-usage-map) for the full derivation table.

Operational rules:

1. Generate `SECRET_KEY` with at least 32 bytes of cryptographically secure randomness — `openssl rand -hex 32` or equivalent. Ovumcy refuses placeholder values like `change_me_in_production` at startup.
2. Store the secret separately from the data backup. Restoring data with a different key invalidates auth cookies and breaks 2FA. The backup/restore split is documented in [`docs/self-hosted.md`](self-hosted.md).
3. Rotation impact on TOTP-enabled accounts is documented in [`SECRET_KEY Usage Map`](security/cryptography.md#secret_key-usage-map). Plan rotation as planned maintenance and communicate it in advance.

## Records of Processing (Art. 30)

Per-action audit logs are **off by default** (`AUDIT_LOG_ENABLED=false`). When you enable them via `AUDIT_LOG_ENABLED=true`, the runtime emits per-action lines to stderr containing the action name, outcome, sanitized request path, response format, and (for authenticated requests) `user_id` and role. The full schema is documented in [`Logging Policy`](security/logging.md#logging-policy).

If you have a multi-subject deployment and need an Art. 30 record, enable audit logs and route them to a tamper-resistant store (journald with `Storage=persistent`, an external syslog server, or an immutable object store). Treat the resulting log stream as the same sensitivity class as the database itself; rotate and access-control it accordingly.

## Breach Notification (Art. 33)

In the default configuration Ovumcy calls no external service. When the owner enables OIDC or webhook reminders, the configured identity provider or webhook endpoint becomes a recipient the operator must account for. A breach in the strict GDPR sense therefore requires the database, `SECRET_KEY`, the host system, or an owner-configured integration endpoint to be compromised.

The disclosure channel for vulnerabilities is [`SECURITY.md → Reporting a Vulnerability`](../SECURITY.md#reporting-a-vulnerability). For a confirmed breach affecting personal data of EU/UK subjects, the controller (you, the operator) must notify the relevant supervisory authority within **72 hours**, unless the breach is unlikely to result in risk to the rights and freedoms of natural persons.

Operator workflow:

1. Detect — `AUDIT_LOG_ENABLED=true` plus reverse-proxy access logs are the minimum useful signal source.
2. Contain — rotate `SECRET_KEY` (planned maintenance window). All active auth cookies become invalid, all 2FA-enrolled accounts need to recover via recovery code, and every calendar-feed subscribe URL — of any generation, including pre-migration-032 ones (disarmed at the first boot under the new key) — stops serving. Verify containment: a leaked feed URL must return `404` after the restart.
3. Notify — file with your supervisory authority within 72 hours.
4. Notify affected subjects when the breach is "likely to result in a high risk".

## DPIA Trigger

A Data Protection Impact Assessment is recommended (and frequently required by national law) when processing involves special-category data on a large scale or systematic monitoring. Even a single household instance benefits from a short DPIA covering: lawful basis, data minimisation justification, retention, encryption-at-rest design, and breach plan. Ovumcy ships the technical inputs you need; the DPIA itself is an operator artefact.

## Retention

Ovumcy does not auto-delete user records. Health data is retained until the data subject (or the operator on their behalf) invokes `clear-data` or `delete-account`. This is consistent with the way the application is used — a multi-cycle longitudinal record is the product.

If your deployment commits to a specific retention period (for example, "delete logs older than 5 years"), know that no supported tooling implements one today: the internal repository methods are not reachable from outside the binary, and no CLI subcommand performs partial or age-based deletion. Selective retention therefore means direct SQL against `daily_logs` with the same per-`user_id` scoping the application uses (against a stopped instance or a verified backup), or a feature request. There is no built-in **retention** scheduler — the built-in reminder scheduler named under *Data Inventory* schedules webhook reminders, never deletion.

Auxiliary short-lived tables (`register_pickup_tokens` ≤ 5 min, `oidc_logout_states` ≤ 7 days — a fixed value Ovumcy sets itself, not supplied by the OIDC provider) are TTL-bounded and pruned in the normal sweep.

## Backup Restore and the Calendar Feed

Restoring the data volume from a backup returns the database to its state at backup time, including `calendar_feed_selector` and the verifier columns. If an owner revoked or rotated their calendar-feed subscription after that backup was taken, restoring from it resurrects the old subscribe URL — a direct consequence of restoring the feed columns, not a theoretical edge. After any restore, re-check `Settings → Calendar feed` for every owner and revoke or regenerate the feed again if it should no longer be armed.

## Cookie Consent (ePrivacy)

Ovumcy uses only first-party functional cookies (auth, CSRF, language, timezone, flash, OIDC state, recovery, reset). All are strictly necessary for the requested service and therefore do not require an ePrivacy consent banner. The full inventory is in [`Cookies`](security/cryptography.md#cookies). The language cookie is also mirrored on the account (`users.interface_language`) so a signed-in owner keeps their language on a new device; it is a display preference, stored under the same lawful basis as the rest of the account settings.

There are no analytics, advertising, or third-party tracking cookies — this is enforced by the strict Content Security Policy (`default-src 'self'; script-src 'self'; ...`). If you ever add an integration that introduces non-essential cookies, ePrivacy obligations attach and you must add a consent banner; the existing CSP makes accidental introduction unlikely.

## Data Processing Agreement

Ovumcy itself does not act as a processor for anyone — there is no Ovumcy-operated service. If you self-host for other people and your hosting provider has access to the underlying volumes (most managed VPS providers do), you need a DPA with that provider. Ovumcy provides nothing to sign.

## Compliance Sanity Checklist

For an operator running Ovumcy for one or more other people:

- [ ] HTTPS terminated at a reverse proxy.
- [ ] `COOKIE_SECURE=true` and `TRUST_PROXY_ENABLED=true` set in `.env`.
- [ ] `SECRET_KEY` generated with `openssl rand -hex 32` or equivalent.
- [ ] Database file lives on an encrypted volume.
- [ ] Backups encrypted with a key separate from `SECRET_KEY`, stored in a separate location.
- [ ] `AUDIT_LOG_ENABLED=true` and logs shipped to a tamper-resistant store if you carry an Art. 30 obligation.
- [ ] Deployment-specific privacy notice in place naming the controller and the lawful basis.
- [ ] Process documented for handling DSARs (access/erasure) and breach notification.
- [ ] Tested the export, clear-data, and delete-account flows from a real user's perspective.
- [ ] If using OIDC, verified `OIDC_CA_FILE` points to a regular file ≤ 1 MB containing valid PEM (Ovumcy enforces this at startup).

For a single-self deployment, only the first six items are practically meaningful.
