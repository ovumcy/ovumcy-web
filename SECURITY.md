# Security Policy

## Supported Versions

Security fixes are provided for the `main` branch only.

| Version | Supported |
| --- | --- |
| `main` | :white_check_mark: |
| older commits/tags | :x: |

## Verifying Release Authenticity

Published container images are keyless-signed with [cosign](https://github.com/sigstore/cosign)
and carry a [SLSA build provenance](https://slsa.dev) attestation and a software bill of
materials, all produced by this repository's GitHub Actions and pushed to the registry. Before
running an image you can confirm it was built by this repository's CI from this source, rather
than substituted or tampered with.

Verify the signature (replace `v1.1.0` with the tag you are pulling):

```bash
cosign verify ghcr.io/ovumcy/ovumcy-web:v1.1.0 \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity-regexp '^https://github.com/ovumcy/ovumcy-web/\.github/workflows/docker-image\.yml@'
```

Verify the build provenance with the GitHub CLI:

```bash
gh attestation verify oci://ghcr.io/ovumcy/ovumcy-web:v1.1.0 --owner ovumcy
```

A failed check means the image was not produced by this repository's release workflow. Do not run it.

## Reporting a Vulnerability

Please report security vulnerabilities privately — not through public GitHub
issues. Use either channel:

- GitHub private vulnerability reporting (preferred):
  https://github.com/ovumcy/ovumcy-web/security/advisories/new
- Email: contact@ovumcy.com (subject: `SECURITY: <short summary>`)

Include impact, reproduction steps, affected endpoints/files, and a suggested
fix if you have one.

We practice coordinated vulnerability disclosure. After a report we acknowledge
receipt within 72 hours, share a triage assessment and remediation plan within
7 days, and aim to ship a fix and coordinate public disclosure within 90 days —
sooner for actively exploited issues. Please give us reasonable time to
remediate before disclosing a vulnerability publicly.

## Security Documentation

Detailed security design lives in per-concern documents under [`docs/security/`](docs/security/). This policy keeps the operator-facing essentials above plus the GDPR cross-reference and the Test Enforcement Matrix (the source-of-truth mapping of each claim to its Go test).

- [Known Information Disclosure](docs/security/known-disclosures.md) — accepted residual signals (register-enumeration oracle, absolute session expiry, process-local limiter state).
- [OIDC Account Linking & Session Invalidation](docs/security/oidc-and-sessions.md) — upstream-IdP trust boundary and credential-rotation session revocation.
- [Cryptography: Field Encryption, Cookies & SECRET_KEY](docs/security/cryptography.md) — field-level encryption, the cookie inventory, and the SECRET_KEY usage map.
- [Data Inventory, Retention & Deletion](docs/security/data-handling.md) — what is persisted per account and how clear-data / delete-account erase it.
- [Password & Auth Policy, Rate Limits](docs/security/auth-policy-and-rate-limits.md) — password/recovery/TOTP policy and the per-IP / per-account rate limits.
- [Threat Model](docs/security/threat-model.md) — in-scope defences and explicit out-of-scope assumptions.
- [Logging Policy](docs/security/logging.md) — the off-by-default audit stream and request-log sanitization.

## GDPR Cross-Reference

Ovumcy is privacy-critical software handling special-category personal data (Art. 9). Operators of self-hosted instances act as data controllers; Ovumcy provides the technical controls and operators provide deployment-specific policy. The full operator-facing compliance walkthrough lives in [`docs/gdpr.md`](docs/gdpr.md). The table below maps each in-scope obligation onto the existing technical control and its enforcing test.

| GDPR Article | Obligation | Technical control | Enforcing test |
| --- | --- | --- | --- |
| Art. 5(1)(c) — Data minimisation | Only data necessary for the cycle-tracking purpose is stored | `users`, `daily_logs`, `symptom_types` schemas as described in [*Data Inventory*](docs/security/data-handling.md#data-inventory); no analytics, telemetry, or third-party identifiers | `internal/db/migrations.go` + schema in `migrations/` |
| Art. 5(1)(f) — Integrity & confidentiality | Auth/recovery/reset cookies sealed; TOTP secret encrypted at rest; CSRF on every mutation | AES-256-GCM + HKDF-derived keys + AAD binding | `internal/api/secure_cookie_codec_*_test.go`, `internal/security/field_crypto_test.go`, `internal/api/state_mutation_csrf_regression_test.go` |
| Art. 9(2)(a) — Explicit consent | Self-hosted single-tenant deployments rely on operator-captured consent; the codebase exposes no third-party transmission | `/privacy` page renders the in-app privacy notice; no external network calls | `internal/api/privacy_route_regressions_test.go`, `e2e/privacy.spec.ts` |
| Art. 13/14 — Information to subject | `/privacy` page enumerates storage location, scope of processing, and opt-out paths in five locales | `internal/templates/privacy.html` + `internal/i18n/locales/*.json` privacy keys | `internal/api/privacy_route_regressions_test.go`, `e2e/privacy.spec.ts` |
| Art. 15 — Right to access | Subjects export their full data set as CSV or JSON | `GET /api/v1/exports/csv`, `GET /api/v1/exports/json`, `GET /api/v1/exports/summary` | `internal/api/export_regressions_test.go` and siblings |
| Art. 16 — Right to rectification | Every owner field is editable from the dashboard, calendar, and settings UI | `PUT /api/v1/days/{date}`, `PATCH /api/v1/users/current/profile`, `PATCH /api/v1/symptoms/{id}` | `internal/api/day_upsert_canonicalization_regression_test.go`, `internal/api/settings_profile_persist_regressions_test.go`, `internal/api/settings_symptoms_*_regression_test.go` |
| Art. 17 — Right to erasure | `clear-data` removes health records; `delete-account` explicitly erases every user-scoped row (daily logs, symptoms, OIDC identities, register-pickup tokens, OIDC logout states) plus the account | `POST /api/v1/users/current/data-wipe`, `DELETE /api/v1/users/current` | `internal/api/settings_clear_data_flow_test.go`, `internal/api/settings_delete_account_regression_test.go`, `internal/db/delete_account_completeness_test.go`, `TestClearDataPreservesAccountIdentityFields` |
| Art. 20 — Right to portability | Export endpoints return machine-readable formats and the JSON export can be re-imported into a fresh instance (additive restore — never overwrites or deletes existing days); structure documented in `docs/export.md` | `GET /api/v1/exports/csv`, `GET /api/v1/exports/json`, `POST /api/v1/imports/json` | `internal/api/export_regressions_test.go`, `internal/services/export_service_integration_test.go`, `internal/api/imports_regressions_test.go`, `internal/services/import_service_test.go` |
| Art. 22 — Automated decision-making | Cycle predictions are statistical aggregations of the subject's own logs; no decision producing legal effects | Prediction policy in `internal/services/prediction_explanation.go`; copy explains conservative bounds | `internal/services/prediction_explanation_test.go`, `internal/services/calendar_view_service_test.go` |
| Art. 25 — Privacy by design / by default | Owner-only middleware on every mutation; auto-period-fill **off by default**; audit log **off by default** | `handler.OwnerOnly` chained explicitly on every `/api/v1/*` mutation; `AUDIT_LOG_ENABLED=false` startup default | `internal/api/owner_only_coverage_regression_test.go`, `internal/api/security_event_logging_audit_flag_test.go`, `cmd/ovumcy/main_test.go` (`TestLoadRuntimeConfigDefaultsAuditLogOff`) |
| Art. 30 — Records of processing | Per-action audit stream available under `AUDIT_LOG_ENABLED=true`; sanitized to drop PII | `internal/api/security_event_logging.go`, `SafeRequestLogPath` | `internal/api/security_event_logging_audit_flag_test.go`, `internal/api/security_event_logging_mutation_regression_test.go` |
| Art. 32 — Security of processing | Sealed cookies, encrypted TOTP secrets, owner-only access, rate limits, CSP, secure headers; operator adds disk-level encryption | Multiple — see the [*Cryptography*](docs/security/cryptography.md) and [*Rate Limits*](docs/security/auth-policy-and-rate-limits.md#rate-limits) concern docs; `cmd/ovumcy/main.go` security headers | `internal/api/secure_cookie_codec_*_test.go`, `internal/security/field_crypto_test.go`, `cmd/ovumcy/main_test.go` (security headers, rate-limit PII), `internal/api/owner_only_coverage_regression_test.go` |
| Art. 33 — Breach notification (72 h) | Disclosure channel published; operator-side runbook in `docs/gdpr.md` | [`SECURITY.md → Reporting a Vulnerability`](#reporting-a-vulnerability), [`docs/gdpr.md → Breach Notification`](docs/gdpr.md#breach-notification-art-33) | Policy-level — not test-enforceable |
| Art. 34 — Communication to subject | Operator obligation; Ovumcy provides per-account email addresses for outreach | Stored in `users.email` (read-only after registration via UI) | Not test-enforceable |
| Art. 35 — DPIA | Operator obligation; Ovumcy ships the technical inputs (data inventory, threat model, encryption posture) needed to build one | [*Data Inventory*](docs/security/data-handling.md#data-inventory), [*Threat Model*](docs/security/threat-model.md#threat-model), [`docs/gdpr.md`](docs/gdpr.md) | Policy-level — not test-enforceable |

Items marked *Policy-level* are operator obligations that cannot be verified by `go test`. Items marked with concrete test files are verified by the runtime test suite; their concrete claim mapping lives in [*Test Enforcement Matrix*](#test-enforcement-matrix) below.

## Test Enforcement Matrix

This section maps each test-enforceable claim in this policy (and the concern documents under `docs/security/`) to the Go test that guards it. It is the mechanical check that the privacy and security claims remain true. When a claim changes, the corresponding test must change too; when a test is removed, the claim is no longer enforced and must be retracted.

Policy-level claims (threat model in/out-of-scope, design rationale, marketing-style statements like "privacy-first") are intentionally excluded — they are reviewed by humans, not by `go test`.

### Layering

| Claim | Enforced by |
| --- | --- |
| `templateToJSON` returns a plain string (not `template.JS`/`template.HTML`), so owner- or attacker-controlled values embedded in a `data-*` attribute (`data-chart`, `data-supported-languages`) stay subject to `html/template`'s contextual attribute escaping — HTML/JS-breaking characters cannot break out of the attribute or inject markup | `TestTemplateToJSONEscapesAttributeContext` in [internal/api/template_tojson_attr_escaping_test.go](internal/api/template_tojson_attr_escaping_test.go) |

### Field-Level Encryption

| Claim | Enforced by |
| --- | --- |
| `users.totp_secret` is AES-256-GCM encrypted under a key derived from `SECRET_KEY` via HKDF-SHA256 | `TestEncryptDecryptField_RoundTrip` in [internal/security/field_crypto_test.go](internal/security/field_crypto_test.go) |
| AAD-bound to row id; cross-row substitution under a different AAD fails to open | `TestDecryptField_RejectsWrongAAD` in [internal/security/field_crypto_test.go](internal/security/field_crypto_test.go) |
| Ciphertext is non-deterministic (distinct outputs for the same plaintext) | `TestEncryptField_ProducesDistinctCiphertexts` in [internal/security/field_crypto_test.go](internal/security/field_crypto_test.go) |
| Wrong key fails to decrypt | `TestDecryptField_WrongKey` in [internal/security/field_crypto_test.go](internal/security/field_crypto_test.go) |
| Tampered ciphertext fails to decrypt | `TestDecryptField_TamperedCiphertext` in [internal/security/field_crypto_test.go](internal/security/field_crypto_test.go) |
| Legacy no-AAD ciphertexts open through the fallback path | `TestDecryptField_LegacyFallback` in [internal/security/field_crypto_test.go](internal/security/field_crypto_test.go) |
| Empty `SECRET_KEY` is refused | `TestEncryptField_EmptyKey`, `TestDecryptField_EmptyKey` in [internal/security/field_crypto_test.go](internal/security/field_crypto_test.go) |
| Golden pre-consolidation ciphertexts (AAD-bound and legacy no-AAD) still decrypt to the same plaintext | `TestDecryptFieldOpensPreConsolidationGoldenCiphertexts` in [internal/security/field_crypto_golden_test.go](internal/security/field_crypto_golden_test.go) |

### OIDC Account Linking

| Claim | Enforced by |
| --- | --- |
| First-time link to a pre-existing local-auth account is refused without explicit password confirmation | `TestOIDCCallbackPendingLinkSealsCookieAndRedirectsToConfirmPage` in [internal/api/auth_oidc_regressions_test.go](internal/api/auth_oidc_regressions_test.go) |
| Pending-link cookie is rejected on tamper / cross-purpose AAD / rotated key / payload expiry | `oidc_link_pending_cookie_test.go` in `internal/api/` |
| `/auth/oidc/link-confirm` POST is CSRF-protected and rejects requests without `csrf_token` | `TestCompleteOIDCLinkConfirmationRejectsRequestWithoutCSRFToken` in [internal/api/auth_oidc_regressions_test.go](internal/api/auth_oidc_regressions_test.go) |
| Wrong password keeps the pending-link cookie alive for retry within the 5-minute TTL | `TestCompleteOIDCLinkConfirmationKeepsCookieOnWrongPassword` in [internal/api/auth_oidc_regressions_test.go](internal/api/auth_oidc_regressions_test.go) |
| Accounts with `local_auth_enabled=false` (OIDC-only) are refused at the link-confirm path | `TestOIDCCallbackPendingLinkForOIDCOnlyUserRefusesWithoutCookie`, `TestCompleteOIDCLinkConfirmationWithLocalAuthDisabledRefusesUnavailable` in [internal/api/auth_oidc_regressions_test.go](internal/api/auth_oidc_regressions_test.go) |
| TOTP-enabled targets require a valid 6-digit code together with the password; missing / wrong / replayed codes refuse the link and do not issue a session | `TestCompleteOIDCLinkConfirmationWithTOTPEnabledRequiresValidCode`, `TestCompleteOIDCLinkConfirmationWithTOTPEnabledRefusesMissingCode`, `TestCompleteOIDCLinkConfirmationWithTOTPEnabledRefusesWrongCode` in [internal/api/auth_oidc_regressions_test.go](internal/api/auth_oidc_regressions_test.go) |
| Link-confirm form exposes the TOTP input only when the target account has TOTP enabled | `TestShowOIDCLinkConfirmPageRendersTOTPFieldForTOTPEnabledTarget`, `TestShowOIDCLinkConfirmPageHidesTOTPFieldForNonTOTPTarget` in [internal/api/auth_oidc_regressions_test.go](internal/api/auth_oidc_regressions_test.go) |
| `MustChangePassword` targets are routed to `/reset-password` instead of receiving an auth cookie | `TestCompleteOIDCLinkConfirmationRoutesMustChangePasswordToReset` in [internal/api/auth_oidc_regressions_test.go](internal/api/auth_oidc_regressions_test.go) |
| `ConfirmAndLinkIdentity` provider/storage failures clear the pending cookie | `TestCompleteOIDCLinkConfirmationConfirmLinkErrorMappingClearsCookie` in [internal/api/auth_oidc_regressions_test.go](internal/api/auth_oidc_regressions_test.go) |
| Successful link emits a sanitized `auth.oidc_link_confirm linked` security event | `TestCompleteOIDCLinkConfirmationEmitsAuditLogOnSuccess` in [internal/api/auth_oidc_regressions_test.go](internal/api/auth_oidc_regressions_test.go) |

### Register Enumeration Residual

| Claim | Enforced by |
| --- | --- |
| `POST /api/v1/users` emits an identical-shape sealed pickup cookie for new and duplicate emails | `auth_register_regressions_test.go`, `auth_register_email_persistence_regressions_test.go` in `internal/api/` |
| Duplicate-email branch runs equalized bcrypt timing | `TestAuthenticateCredentialsEqualizesTimingForMissingUser`, `TestAuthenticateCredentialsEqualizesTimingForDisabledLocalAuth` in [internal/services/auth_service_credentials_timing_test.go](internal/services/auth_service_credentials_timing_test.go) |
| Timing-equalization placeholder hashes carry the production bcrypt cost (equalized paths are not measurably faster than a real compare) | `TestTimingEqualizationHashesMatchTargetCost` in [internal/services/auth_service_hash_cost_test.go](internal/services/auth_service_hash_cost_test.go) |
| Pickup nonce is single-use via `register_pickup_tokens` (atomic UPDATE) | `register_pickup_handler_test.go` in `internal/api/` |
| `GET /register/welcome` second consumption falls through to `/login` | `register_pickup_handler_test.go` in `internal/api/` |
| Recovery code shape `OVUM-XXXX-XXXX-XXXX` | `TestValidateRecoveryCodeFormat`, `TestNormalizeRecoveryCode` in [internal/services/auth_input_policy_test.go](internal/services/auth_input_policy_test.go) |

### Cross-Owner Data Isolation (household self-hosting)

| Claim | Enforced by |
| --- | --- |
| An instance may host several independent owners; each owner's data is isolated by `user_id`, and a resource id from the request is always combined with the session user — `PATCH`/`DELETE`/`restore` of another owner's symptom returns 404 and leaves it unchanged | `TestUpdateSymptomByOtherUserReturnsNotFound`, `TestDeleteSymptomByOtherUserReturnsNotFound`, `TestRestoreSymptomByOtherUserReturnsNotFound` in [internal/api/symptoms_idor_regression_test.go](internal/api/symptoms_idor_regression_test.go) |
| A day upsert carrying another owner's `symptom_id` is rejected (400) and writes no log row for the requester | `TestUpsertDayRejectsSymptomIDOwnedByOtherUser` in [internal/api/symptoms_idor_regression_test.go](internal/api/symptoms_idor_regression_test.go) |
| A restore whose `other_symptoms` name collides with another owner's custom symptom resolves to the importing owner's own row, never the other owner's id, and leaves that owner's catalog and logs untouched | `TestImportServiceScopesResolvedSymptomsToImportingOwner` in [internal/services/import_service_test.go](internal/services/import_service_test.go) |

### Data Import (Restore)

| Claim | Enforced by |
| --- | --- |
| The JSON restore (`POST /api/v1/imports/json`) is owner-only and CSRF-protected: a missing token returns 403; a valid token imports | `TestImportJSONRejectsMissingCSRF`, `TestImportJSONSucceedsWithCSRF` in [internal/api/imports_regressions_test.go](internal/api/imports_regressions_test.go) |
| Restore is additive — a day that already exists is never overwritten or deleted | `TestImportServiceSkipsExistingDays` in [internal/services/import_service_test.go](internal/services/import_service_test.go) |
| Imported records are re-validated and owner-scoped; round-tripping an export reproduces the same entries | `TestImportServiceRoundTripPreservesEntries` in [internal/services/import_service_test.go](internal/services/import_service_test.go) |
| A crafted file cannot persist an inconsistent anchor (`cycle_start` on a non-period day) | `TestImportServiceDropsCycleStartOnNonPeriodDay` in [internal/services/import_service_test.go](internal/services/import_service_test.go) |
| Custom-symptom creation on import is bounded (`MaxImportCustomSymptoms`), so a crafted file cannot force unbounded catalog growth / DB churn | `TestImportServiceCapsCustomSymptomCreation` in [internal/services/import_service_test.go](internal/services/import_service_test.go) |

### Webhook Notifications (outbound egress)

The request-free notify pass POSTs due period/ovulation reminders to an owner-configured URL. The endpoint is fully owner-controlled (self-hosted ntfy/Gotify commonly on the LAN), so the request envelope is hardened rather than the destination blanket-blocked. Owners configure the URL from the settings page (`POST /api/v1/users/current/webhook`), where the URL is treated as a write-only secret.

**Deployment posture.** Private/loopback/link-local egress is allowed by default so self-hosted-on-LAN setups (ntfy/Gotify next to the host) work out of the box; the destination is hardened rather than blanket-blocked. This is safe on a single-owner instance, but on a multi-owner or publicly reachable instance with `REGISTRATION_MODE=open` any account that signs up can point its webhook at an internal address and use the shared notify pass to probe your private network (SSRF). For those deployments set `WEBHOOK_BLOCK_PRIVATE_ADDRESSES=true`, or close sign-ups with `REGISTRATION_MODE=closed`. The server emits a startup warning when `REGISTRATION_MODE=open` and `WEBHOOK_BLOCK_PRIVATE_ADDRESSES=false`, so the trade-off is visible in the logs at boot.

| Claim | Enforced by |
| --- | --- |
| The webhook settings save is OwnerOnly + CSRF-protected and scoped to the session's owner — one owner's save can never write another owner's row | `TestWebhookSaveScopedToOwner` in [internal/api/settings_webhook_regressions_test.go](internal/api/settings_webhook_regressions_test.go), the `POST /api/v1/users/current/webhook` cases in `TestUnsupportedRoleRejectedAcrossEveryAuthedV1Route` ([owner_only_coverage_regression_test.go](internal/api/owner_only_coverage_regression_test.go)) and `TestStateMutatingEndpointsRejectMissingCSRFToken` ([state_mutation_csrf_missing_token_test.go](internal/api/state_mutation_csrf_missing_token_test.go)) |
| The webhook URL is a write-only secret: after it is stored, the settings page HTML never renders the URL, its token, or its path/query — only a configured/not-configured status plus at most the hostname | `TestWebhookSettingsPageNeverRendersStoredURL` in [internal/api/settings_webhook_regressions_test.go](internal/api/settings_webhook_regressions_test.go); `TestBuildWebhookURLDisplay` in [internal/services/webhook_settings_service_test.go](internal/services/webhook_settings_service_test.go) |
| A saved webhook URL is encrypted at rest (ciphertext in `webhook_url`, never plaintext) and round-trips back only through the aad-bound decrypt | `TestWebhookSaveEncryptsURLAtRestAndRoundTrips` in [internal/api/settings_webhook_regressions_test.go](internal/api/settings_webhook_regressions_test.go) |
| A non-`http(s)` webhook URL is rejected at save with `400` and nothing is persisted (scheme guard reused from the service, not re-implemented in transport) | `TestWebhookSaveNonHTTPSchemeRejectedNothingPersisted` in [internal/api/settings_webhook_regressions_test.go](internal/api/settings_webhook_regressions_test.go), `TestValidateWebhookURL` in [internal/services/webhook_settings_service_test.go](internal/services/webhook_settings_service_test.go) |
| A blank URL submission leaves the stored endpoint unchanged (write-only field); a distinct remove affordance clears it and forces delivery off | `TestWebhookSaveBlankURLKeepsStoredEndpoint`, `TestWebhookSaveRemoveClearsStoredEndpoint` in [internal/api/settings_webhook_regressions_test.go](internal/api/settings_webhook_regressions_test.go); `TestSaveWebhookSettingsFromFormBlankURLKeepsStoredEndpoint`, `TestSaveWebhookSettingsFromFormRemoveClearsAndDisables` in [internal/services/webhook_settings_service_test.go](internal/services/webhook_settings_service_test.go) |
| In a multi-owner batch, each owner's reminder is delivered only to that owner's URL — one owner's predicted health data never reaches another owner's URL | `TestNotifyCrossOwnerIsolation`, `TestNotifyCrossOwnerHealthDataStaysScoped` in [internal/services/webhook_notify_service_test.go](internal/services/webhook_notify_service_test.go) |
| Delivery refuses ALL redirects, so a 3xx cannot steer the request (or its body) to a second unvalidated origin | `TestWebhookDeliveryRefusesRedirect` in [internal/services/webhook_delivery_test.go](internal/services/webhook_delivery_test.go) |
| Only `http`/`https` schemes are delivered; other schemes are rejected at delivery time (defence in depth over save-time validation) | `TestWebhookDeliveryRejectsNonHTTPScheme` in [internal/services/webhook_delivery_test.go](internal/services/webhook_delivery_test.go) |
| With the off-by-default `WEBHOOK_BLOCK_PRIVATE_ADDRESSES` gate on, a hostname that resolves to any private/loopback/link-local/unspecified address — including RFC 6598 CGNAT (`100.64.0.0/10`, which `net.IP.IsPrivate()` omits) and the RFC 6052 NAT64 well-known prefix (`64:ff9b::/96`), whose embedded IPv4 is re-classified so a NAT64-wrapped private v4 is blocked while a NAT64-wrapped public v4 stays allowed — is refused before the request, and the IP dialed is one of the validated records — no second, independent resolution (DNS-rebinding TOCTOU closed); a mixed public/private answer is rejected and the gate-off default path is unchanged | `TestWebhookDeliveryBlocksHostnameResolvingToPrivate`, `TestWebhookDeliveryBlocksHostnameResolvingToCarrierRanges`, `TestWebhookDeliveryAllowsHostnameResolvingToPublic`, `TestWebhookDeliveryAllowsHostnameResolvingToTestNet3`, `TestWebhookDeliveryBlocksMixedPublicPrivateAnswer`, `TestIsPrivateIP` in [internal/services/webhook_delivery_test.go](internal/services/webhook_delivery_test.go) |
| A hostile endpoint cannot stall the pass (context/dialer timeout) or force an unbounded body read (response capped by `io.LimitReader`) | `TestWebhookDeliveryHonorsContextTimeout`, `TestWebhookDeliveryHandlesOversizedBody` in [internal/services/webhook_delivery_test.go](internal/services/webhook_delivery_test.go) |
| Delivery logs and returns the destination HOST only — never the full URL, path, query, or userinfo (which may carry an ntfy token) | `TestWebhookDeliveryLogsHostOnly` in [internal/services/webhook_delivery_test.go](internal/services/webhook_delivery_test.go) |
| Every delivered payload carries the mandatory medical-safety disclaimer | `TestNotifyDisclaimerPresentInEveryPayload` in [internal/services/webhook_notify_service_test.go](internal/services/webhook_notify_service_test.go) |
| The per-kind watermark advances only on a successful (2xx) delivery; a failed delivery leaves it unchanged so the next pass retries, and a second pass over an advanced watermark sends nothing (idempotency both directions) | `TestNotifyWatermarkAdvancesOnlyOnSuccess`, `TestNotifyIdempotentSecondPassSkips`, `TestNotifyRetriesAfterFailure` in [internal/services/webhook_notify_service_test.go](internal/services/webhook_notify_service_test.go); `TestUpdateWebhookWatermarkCanonicalizesToUTCMidnight`, `TestUpdateWebhookWatermarkScopedToUser` in [internal/db/user_repository_webhook_test.go](internal/db/user_repository_webhook_test.go) |
| A `webhook_url` that fails to decrypt (e.g. after `SECRET_KEY` rotation) fails safe: that owner is skipped, never delivered to a garbage target, and the pass continues | `TestNotifyDecryptFailureSkipsOwner` in [internal/services/webhook_notify_service_test.go](internal/services/webhook_notify_service_test.go) |
| `--dry-run` computes what would be sent but makes no outbound request and writes no watermark | `TestNotifyDryRunMakesNoRequestOrWatermark` in [internal/services/webhook_notify_service_test.go](internal/services/webhook_notify_service_test.go), `TestRunNotifyCommandDryRunPropagates` in [internal/cli/notify_test.go](internal/cli/notify_test.go) |

### Calendar Feed Subscription (.ics)

The read-only `.ics` feed lets a calendar client subscribe to an owner's upcoming predicted period/ovulation reminders. A calendar client sends no cookie, so the subscribe URL carries a bearer capability token in its path — the single sanctioned exception to *no usable secret in transport*. It is a capability token, not an auth credential (read-only, owner-scoped, revocable, does not bump `auth_session_version`), compensated by hashing-at-rest, 404-no-oracle, log-redaction, per-IP rate-limiting, and a write-only shown-once subscribe URL. Owners manage it from the settings page (`POST /api/v1/users/current/calendar-feed`, `POST …/calendar-feed/rotate`, `DELETE …/calendar-feed`).

| Claim | Enforced by |
| --- | --- |
| The verifier half of the token is stored only as a bcrypt hash — the DB row never holds the plaintext token | `TestGenerateCalendarFeedTokenHashAtRest` in [internal/services/calendar_feed_token_test.go](internal/services/calendar_feed_token_test.go), `TestCalendarFeedTokenHashAtRestInDBRow` in [internal/db/user_repository_calendar_feed_test.go](internal/db/user_repository_calendar_feed_test.go), `TestGenerateCalendarFeedTokenVerifierIsRealBcrypt` in [internal/services/calendar_feed_service_test.go](internal/services/calendar_feed_service_test.go) |
| A malformed token, unknown selector, wrong verifier, and disabled feed all return an identical bare 404 (no body, no oracle), timing-equalized by an unconditional bcrypt on the selector-miss path | `TestCalendarFeedReturnsBare404WithoutOracleForBadTokens` in [internal/api/calendar_feed_regressions_test.go](internal/api/calendar_feed_regressions_test.go), `TestResolveFeedIdenticalNotFoundForEveryBadToken`, `TestResolveFeedEqualizesTimingOnSelectorMiss` in [internal/services/calendar_feed_service_test.go](internal/services/calendar_feed_service_test.go) |
| The feed token value never reaches a request log — the matched-route template masks it, and the raw-path fallback masks an `<opaque-token>.ics` segment | `TestCalendarFeedTokenIsRedactedFromRequestLog`, `TestSanitizeRequestLogPathMasksRawTokenDotICSFallback` in [internal/api/calendar_feed_regressions_test.go](internal/api/calendar_feed_regressions_test.go) |
| The cookieless feed endpoint is per-IP rate-limited (429 once the budget is spent) | `TestCalendarFeedIsRateLimitedPerIP` in [internal/api/calendar_feed_regressions_test.go](internal/api/calendar_feed_regressions_test.go) |
| The subscribe URL is a write-only secret: generate/rotate reveal it exactly once via a sealed one-time cookie and it is never rendered into a later page; a JSON client receives only the reveal `next_path`, never the URL | `TestCalendarFeedGenerateRevealsURLOnceAndNeverAgain`, `TestCalendarFeedGenerateJSONReturnsRevealPathNotURL` in [internal/api/settings_calendar_feed_regressions_test.go](internal/api/settings_calendar_feed_regressions_test.go); the sealed reveal cookie is tamper/round-trip pinned in [internal/api/calendar_feed_reveal_cookie_test.go](internal/api/calendar_feed_reveal_cookie_test.go) |
| Rotate mints a new token so the previous URL dies immediately; revoke clears both columns so the feed 404s | `TestCalendarFeedRotateInvalidatesOldToken`, `TestCalendarFeedRevokeClearsColumns` in [internal/api/settings_calendar_feed_regressions_test.go](internal/api/settings_calendar_feed_regressions_test.go), `TestSaveCalendarFeedTokenRotationReplacesRow`, `TestClearCalendarFeedTokenRevokes` in [internal/db/user_repository_calendar_feed_test.go](internal/db/user_repository_calendar_feed_test.go) |
| The feed token is force-cleared in the SAME atomic update that bumps `auth_session_version` on recovery-code regeneration, password reset, forced operator reset, and clear-data; a routine password change keeps it — and a CAS rollback never leaves the feed armed with a stale version | `TestRecoveryCodeRegenForceClearsFeedAtomically`, `TestPasswordResetCASForceClearsFeedAtomically`, `TestForceOperatorResetForceClearsFeedAtomically`, `TestRoutinePasswordChangeDoesNotClearFeed`, `TestPasswordResetCASRollbackLeavesFeedArmedAndVersionUnchanged` in [internal/db/user_repository_calendar_feed_force_clear_test.go](internal/db/user_repository_calendar_feed_force_clear_test.go); `TestClearAllDataResetsCalendarFeedColumns` in [internal/db/user_repository_calendar_feed_test.go](internal/db/user_repository_calendar_feed_test.go) |
| The three settings endpoints are OwnerOnly + CSRF-protected and scoped to the session owner — one owner can never mint, rotate, or reveal another owner's feed | `TestCalendarFeedGenerateScopedToOwner`, `TestCalendarFeedRevealCrossOwnerCookieIgnored` in [internal/api/settings_calendar_feed_regressions_test.go](internal/api/settings_calendar_feed_regressions_test.go), `TestSaveCalendarFeedTokenScopedToUser` in [internal/db/user_repository_calendar_feed_test.go](internal/db/user_repository_calendar_feed_test.go); the `calendar-feed` routes are also walked by `TestUnsupportedRoleRejectedAcrossEveryAuthedV1Route` and `TestStateMutatingEndpointsRejectMissingCSRFToken` |
| One owner's token never resolves another owner's data; the feed is owner-scoped end to end | `TestResolveFeedCrossUserIsolation` in [internal/services/calendar_feed_service_test.go](internal/services/calendar_feed_service_test.go) |
| Every `.ics` event carries the medical-safety disclaimer and a neutral, contentless title (no cycle phase), and predictions are suppressed for pregnancy-pause / unpredictable cycles | `TestBuildCalendarFeedICSEmitsNeutralEventsWithDisclaimer`, `TestBuildCalendarFeedICSSuppressesForPregnancyPause`, `TestBuildCalendarFeedICSSuppressesForUnpredictableCycle` in [internal/services/calendar_feed_ics_test.go](internal/services/calendar_feed_ics_test.go); the resolve path passing the disclaimer into the feed body is pinned by `TestResolveFeedReturnsOwnersFeedForValidToken` in [internal/services/calendar_feed_service_test.go](internal/services/calendar_feed_service_test.go) |

### Session Invalidation on Credential Rotation

| Claim | Enforced by |
| --- | --- |
| Password change bumps `auth_session_version`; other devices sign out | `auth_password_change_session_regression_test.go` in `internal/api/` |
| Password reset via recovery code bumps `auth_session_version` | `TestAuthServiceResetPasswordAndRotateRecoveryCode` in [internal/services/auth_service_recovery_test.go](internal/services/auth_service_recovery_test.go) |
| Recovery-code regeneration bumps `auth_session_version`; originating session refreshed inline | `TestAuthServiceRegenerateRecoveryCode` in [internal/services/auth_service_recovery_test.go](internal/services/auth_service_recovery_test.go) |
| Forced `ovumcy reset-password` CLI bumps `auth_session_version` | `TestAuthServiceForceResetPasswordByEmail` in [internal/services/auth_service_recovery_test.go](internal/services/auth_service_recovery_test.go), `internal/cli/reset_test.go` |
| TOTP enable bumps `auth_session_version` and refreshes originating session | `handlers_settings_2fa_session_revocation_test.go` in `internal/api/` |
| TOTP disable bumps `auth_session_version` and refreshes originating session | `handlers_settings_2fa_session_revocation_test.go` in `internal/api/` |
| `clear-data` bumps `auth_session_version` atomically with the wipe | `settings_clear_data_session_revocation_test.go` in `internal/api/` |
| Opportunistic bcrypt-cost rehash on login rewrites `password_hash` without bumping `auth_session_version` (the authenticating session survives) | `TestUpdatePasswordHashOnlyPreservesSessionVersion` in [internal/db/user_repository_cas_test.go](internal/db/user_repository_cas_test.go), `TestAuthenticateCredentialsRehashesStaleCost` in [internal/services/auth_service_hash_cost_test.go](internal/services/auth_service_hash_cost_test.go) |
| `ovumcy_auth` cookie verifies against current `auth_session_version`; revoked sessions are rejected | `TestAuthMiddlewareRejectsRevokedAuthSessionCookieForAPI`, `TestAuthMiddlewareRejectsRevokedAuthSessionCookieAfterForcedResetForHTML` in [internal/api/auth_cookie_compat_regression_test.go](internal/api/auth_cookie_compat_regression_test.go) |

### Cookies

| Claim | Enforced by |
| --- | --- |
| Sealed cookies use the AES-GCM `v2.<...>` envelope | `TestSecureCookieCodecSealsWithVersion2Prefix` in [internal/api/secure_cookie_codec_rotation_test.go](internal/api/secure_cookie_codec_rotation_test.go) |
| Legacy `v1` cookie payloads are rejected | `TestSecureCookieCodecRejectsLegacyV1Payload` in [internal/api/secure_cookie_codec_rotation_test.go](internal/api/secure_cookie_codec_rotation_test.go) |
| Sealed cookies are AAD-bound to the cookie name; cross-cookie substitution fails | `secure_cookie_codec_security_test.go` in `internal/api/` |
| Golden sealed values from the pre-consolidation codec still open for all 11 purposes | `TestSecureCookieCodecOpensPreConsolidationGoldenValues` in [internal/api/secure_cookie_codec_golden_test.go](internal/api/secure_cookie_codec_golden_test.go) |
| `ovumcy_auth`, `ovumcy_register_pickup`, `ovumcy_recovery_code` all set `SameSite=Lax` | `cookie_security_enabled_test.go`, `cookie_security_default_test.go` in `internal/api/` |
| OIDC sign-in cookies (`ovumcy_oidc_auth`, `ovumcy_oidc_stepup`) require `Secure=true` to issue | `oidc_state_cookie_test.go`, `oidc_stepup_cookie_test.go` in `internal/api/` |
| Login issues a sealed `ovumcy_auth` cookie (not a legacy JWT) | `TestLoginSetsSealedAuthCookieValue`, `TestAuthMiddlewareRejectsLegacyJWTAuthCookieFallback` in [internal/api/auth_cookie_compat_regression_test.go](internal/api/auth_cookie_compat_regression_test.go) |
| Remember-me toggles cookie persistence | `TestLoginRememberMeControlsCookiePersistence` in [internal/api/auth_login_remember_me_regressions_test.go](internal/api/auth_login_remember_me_regressions_test.go) |
| State-changing endpoints require a valid CSRF token | `state_mutation_csrf_regression_test.go`, `auth_logout_csrf_regression_test.go`, `settings_security_csrf_regression_test.go`, `export_csrf_regression_test.go`, `language_switch_csrf_regression_test.go` in `internal/api/` |
| The CSRF middleware exempts exactly ONE mutating route — `POST /auth/oidc/callback` (POST-bound); the query-mode `GET` callback is a safe method outside CSRF, protected by the same sealed one-time state — and every other mutating route in the real app refuses a token-less request with 403 | `TestCSRFExemptionListIsExactlyOneRoute`, `TestCSRFDeniesEveryMutatingRouteWithoutToken` in [cmd/ovumcy/csrf_exemption_guard_test.go](cmd/ovumcy/csrf_exemption_guard_test.go) |
| `OIDC_RESPONSE_MODE=form_post` (default) pins `response_mode=form_post` on the authorize URL and reads the callback from the POST body; `query` omits the parameter, serves the callback over `GET`, and reads it from the URL query — a single source per mode, with the PKCE S256 challenge and nonce preserved in both. The PKCE verifier is never in transport (authorize URL nor callback), only in the sealed `HttpOnly` state cookie, so a query-mode code in the URL is inert | `TestAuthCodeURLResponseModeParam`, `TestOIDCVerifierNeverInTransport` in [internal/security/oidc_response_mode_test.go](internal/security/oidc_response_mode_test.go), `TestOIDCCallbackQueryMode*`, `TestOIDCCallbackFormPostMode*` in [internal/api/auth_oidc_response_mode_test.go](internal/api/auth_oidc_response_mode_test.go) |

### Retention and Deletion

| Claim | Enforced by |
| --- | --- |
| `clear-data` deletes daily_logs and user-defined symptoms and resets preferences | `settings_clear_data_flow_test.go` in `internal/api/` |
| `clear-data` does **not** touch email, password hash, recovery code hash, role, display name, OIDC identity links, TOTP state, or onboarding status | `TestClearDataPreservesAccountIdentityFields` in [internal/api/settings_clear_data_preservation_test.go](internal/api/settings_clear_data_preservation_test.go) **(added in this matrix pass)** |
| `delete-account` deletes daily_logs, all symptoms, and the users row | `settings_delete_account_regression_test.go` in `internal/api/`, `TestOperatorUserService*` in [internal/services/operator_user_service_test.go](internal/services/operator_user_service_test.go) |
| `delete-account` cascades to `oidc_identities` via `ON DELETE CASCADE` FK | Schema-enforced; migration `migrations/014_oidc_identities.sql` |
| Both danger-zone actions require the current password | `settings_clear_data_flow_test.go`, `settings_delete_account_regression_test.go` in `internal/api/` |

### Password & Auth Policy

| Claim | Enforced by |
| --- | --- |
| Passwords require ≥ 8 Unicode code points | `TestValidatePasswordStrength_RejectsWeakPasswords` (`"Short1"` case) in [internal/services/password_policy_test.go](internal/services/password_policy_test.go) |
| Passwords longer than 72 bytes are rejected at validation (bcrypt input limit) | `TestValidatePasswordStrength_EnforcesBcryptByteLimit` in [internal/services/password_policy_test.go](internal/services/password_policy_test.go) |
| Passwords require at least one uppercase, one lowercase, and one digit | `TestValidatePasswordStrength_RejectsWeakPasswords` (alllowercase / ALLUPPERCASE / NoDigitsHere cases) in [internal/services/password_policy_test.go](internal/services/password_policy_test.go) |
| Strong password passes | `TestValidatePasswordStrength_AcceptsStrongPassword` in [internal/services/password_policy_test.go](internal/services/password_policy_test.go) |
| Change-password rejects weak password and mismatch | `TestChangePasswordRejectsWeakNumericPassword`, `TestChangePasswordRejectsPasswordMismatch` in [internal/api/auth_change_password_validation_test.go](internal/api/auth_change_password_validation_test.go) |
| Recovery code is bcrypt-hashed at rest | `TestGenerateRecoveryCodeHash` in [internal/services/auth_reset_policy_test.go](internal/services/auth_reset_policy_test.go) |
| New password and recovery-code hashes are stamped at cost 12 (`passwordHashCost`, above `bcrypt.DefaultCost`) | `TestNewPasswordHashesUseConfiguredCost`, `TestPasswordHashCostIsAboveDefault` in [internal/services/auth_service_hash_cost_test.go](internal/services/auth_service_hash_cost_test.go) |
| TOTP secret is encrypted under a key derived from `SECRET_KEY`; stored value not equal to plaintext | `internal/security/field_crypto_test.go` (see Field-Level Encryption above) |
| TOTP code reuse within the same 30-second step is rejected | `totp_service_test.go`, `user_repository_totp_step_test.go`, `handlers_auth_2fa_test.go` |
| TOTP enrollment rejects 6-digit codes that do not match | `handlers_settings_2fa_test.go` |
| Forgotten password reset rejects wrong recovery code | `TestAuthServiceFindUserByEmailAndRecoveryCodeRejectsMismatch`, `TestAuthServiceFindUserByEmailAndRecoveryCodeRejectsMissingUser` in [internal/services/auth_service_recovery_test.go](internal/services/auth_service_recovery_test.go) |
| Reset token rejects expired / wrong-purpose / state-mismatched inputs | `TestParsePasswordResetTokenRejectsExpired`, `TestParsePasswordResetTokenRejectsWrongPurpose`, `TestAuthServiceResolveUserByResetTokenRejectsStateMismatch` in [internal/services/auth_reset_policy_test.go](internal/services/auth_reset_policy_test.go), [internal/services/auth_service_recovery_test.go](internal/services/auth_service_recovery_test.go) |
| Auth session token rejects expired / wrong-signature / wrong-algorithm | `TestParseAuthSessionTokenRejectsExpired`, `TestParseAuthSessionTokenRejectsInvalidSignature`, `TestParseAuthSessionTokenRejectsWrongAlgorithm` in [internal/services/auth_session_policy_test.go](internal/services/auth_session_policy_test.go) |

### Rate Limits

| Claim | Enforced by |
| --- | --- |
| Per-account rate-limit keys are HMAC-derived from `SECRET_KEY` (no raw identity persisted) | `TestAuthAttemptPolicyKeysUseScopedHMACFingerprint`, `TestAuthAttemptPolicyKeysOmitIdentityFingerprintForBlankIdentity` in [internal/services/auth_attempt_policy_test.go](internal/services/auth_attempt_policy_test.go) |
| Attempt limiter respects window and reset semantics | `TestAttemptLimiterWindowAndReset`, `TestAttemptLimiterMultiKeyOperations` in [internal/services/attempt_limiter_test.go](internal/services/attempt_limiter_test.go) |
| Logout endpoint is rate-limited per account | `auth_logout_rate_limit_regression_test.go` in `internal/api/` |
| OIDC link-confirm password challenge shares the login failure budget (correct password refused once exhausted) | `TestCompleteOIDCLinkConfirmationRateLimitsPasswordAttempts` in [internal/api/auth_oidc_regressions_test.go](internal/api/auth_oidc_regressions_test.go) |
| Attempt limiter memory is hard-capped under a fresh-key flood | `TestAttemptLimiterSizeCapUnderFreshKeyFlood`, `TestAttemptLimiterSizeCapEvictsColdestKeysFirst` in [internal/services/attempt_limiter_test.go](internal/services/attempt_limiter_test.go) |
| TOTP login rate-limited at 5 failures / 15 min | `TestVerifyTOTPLogin_RateLimited_HTMXReturns429` in `handlers_auth_2fa_test.go` |
| TOTP disable rate-limited at 5 failures / 15 min | `handlers_settings_2fa_test.go` |
| Rate-limit error response is sanitized and contains no PII | `TestAuthRateLimitHandlerLogsSecurityEventWithoutPII` in [cmd/ovumcy/main_test.go](cmd/ovumcy/main_test.go) |
| Edge rate limiters key on the real client IP (rightmost untrusted `X-Forwarded-For` hop); a spoofed prefix shares one bucket, and an untrusted peer / disabled proxy ignores the header | `TestRateLimitKeyGeneratorBucketing`, `TestRightmostUntrustedIP`, `TestTrustedProxyMatcher` in [cmd/ovumcy/rate_limit_keygen_test.go](cmd/ovumcy/rate_limit_keygen_test.go) |

### SECRET_KEY Usage Map

| Claim | Enforced by |
| --- | --- |
| Sealed cookies derive their key from `SECRET_KEY` via HKDF with versioned salt/info labels | `secure_cookie_codec_rotation_test.go`, `secure_cookie_codec_security_test.go` in `internal/api/` |
| Field encryption uses a distinct HKDF label set; AAD prevents cross-row swap | `TestDecryptField_RejectsWrongAAD` in [internal/security/field_crypto_test.go](internal/security/field_crypto_test.go) |
| Cookie and field label sets derive distinct keys; a payload sealed for one purpose cannot be opened as the other | `TestSealedCipherPurposeKeySeparation` in [internal/security/sealed_cipher_test.go](internal/security/sealed_cipher_test.go) |
| Rate-limit identity HMAC uses a distinct domain-separation label | `TestAuthAttemptPolicyKeysUseScopedHMACFingerprint` in [internal/services/auth_attempt_policy_test.go](internal/services/auth_attempt_policy_test.go) |
| Rotating `SECRET_KEY` breaks field-encrypted TOTP secrets (DecryptField with the new key fails) | `TestDecryptField_WrongKey` in [internal/security/field_crypto_test.go](internal/security/field_crypto_test.go) |

### Threat Model

| Claim | Enforced by |
| --- | --- |
| OIDC `end_session_endpoint` is host-pinned; cross-origin endpoint is dropped, logout falls back to local | `TestOIDC_RuntimePoC_HostPinRejectsCrossOriginEndSessionEndpoint`, `TestOIDC_RuntimePoC_HostPinAcceptsSameOriginEndSessionEndpoint` in [internal/security/oidc_runtime_poc_test.go](internal/security/oidc_runtime_poc_test.go) |
| OIDC `jwks_uri` is origin-pinned to the issuer; a cross-origin `jwks_uri` from discovery is rejected before any verifier is built | `TestOIDC_RuntimePoC_JWKSOriginPinRejectsCrossOrigin`, `TestOIDC_RuntimePoC_JWKSOriginPinAcceptsSameOrigin` in [internal/security/oidc_runtime_poc_test.go](internal/security/oidc_runtime_poc_test.go); `TestValidateDiscoveredJWKSURI` in [internal/security/oidc_test.go](internal/security/oidc_test.go) |
| OIDC `token_endpoint` is origin-pinned to the issuer; a cross-origin or non-https endpoint from discovery is rejected before any code exchange | `TestValidateDiscoveredTokenEndpoint` in [internal/security/oidc_test.go](internal/security/oidc_test.go) |
| OIDC HTTP client refuses redirects that leave the issuer origin (discovery/JWKS/token requests cannot be steered off-origin by a redirecting response) | `TestOIDC_RuntimePoC_DiscoveryRedirectCrossOriginRefused`, `TestOIDC_RuntimePoC_DiscoveryRedirectSameOriginFollowed` in [internal/security/oidc_runtime_poc_test.go](internal/security/oidc_runtime_poc_test.go); `TestOIDCRedirectPolicyPinsIssuerOrigin` in [internal/security/oidc_test.go](internal/security/oidc_test.go) |
| OIDC ID-token signing-algorithm allowlist rejects symmetric algorithms and `none` | `TestOIDC_RuntimePoC_AlgorithmConfusionRejected`, `TestOIDC_RuntimePoC_AlgorithmNoneRejected` in [internal/security/oidc_runtime_poc_test.go](internal/security/oidc_runtime_poc_test.go) |
| OIDC step-up reauth requires a matching `(issuer, subject)` identity for the current user | `auth_oidc_v2_regressions_test.go`, `settings_oidc_local_password_setup_test.go` in `internal/api/` |
| OIDC first-time link to a pre-existing local-auth account is refused without password confirmation (returns `ErrOIDCLinkRequiresConfirmation`; no identity row is created in the bypass path) | `TestOIDCLoginServiceAuthenticateRequiresConfirmationOnFirstLinkToExistingEmail` in [internal/services/oidc_login_service_test.go](internal/services/oidc_login_service_test.go) |
| OIDC link confirmation persists the identity link after a successful password confirmation | `TestOIDCLoginServiceConfirmAndLinkIdentityPersistsLink` in [internal/services/oidc_login_service_test.go](internal/services/oidc_login_service_test.go) |
| OIDC link confirmation refuses if the `(issuer, subject)` was concurrently claimed by a different user | `TestOIDCLoginServiceConfirmAndLinkIdentityRefusesCrossUserClaim` in [internal/services/oidc_login_service_test.go](internal/services/oidc_login_service_test.go) |
| HTMX error fragments are DOM-built, not assigned via `innerHTML` (defense-in-depth XSS) | `web/src/js/__tests__/` JS unit suite (`npm run test:unit`) |
| All other threat-model entries (operator-as-adversary, endpoint compromise, side-channel) are **policy-level** — Ovumcy explicitly declares these out of scope | Not test-enforceable |

### Logging Policy

| Claim | Enforced by |
| --- | --- |
| `AUDIT_LOG_ENABLED=false` (default) emits no `security event:` lines | `TestAuditLogDefaultOffSuppressesSecurityEvents` in [internal/api/security_event_logging_audit_flag_test.go](internal/api/security_event_logging_audit_flag_test.go) |
| `AUDIT_LOG_ENABLED=true` emits security-event lines | `TestAuditLogEnabledRestoresSecurityEvents` in [internal/api/security_event_logging_audit_flag_test.go](internal/api/security_event_logging_audit_flag_test.go) |
| When enabled, day-write logs the sanitized path `/api/v1/days/:date` (no concrete date) | `TestUpsertDayLogsSanitizedPathWithoutConcreteDate` in [internal/api/security_event_logging_mutation_regression_test.go](internal/api/security_event_logging_mutation_regression_test.go) |
| When enabled, symptom mutation log does not leak the user-supplied symptom name | `TestCreateSymptomLogsMutationWithoutLeakingUserInput` in [internal/api/security_event_logging_mutation_regression_test.go](internal/api/security_event_logging_mutation_regression_test.go) |
| CSRF middleware error path does not leak PII into the audit log | `TestCSRFMiddlewareErrorHandlerLogsSecurityEventWithoutPII` in [cmd/ovumcy/main_test.go](cmd/ovumcy/main_test.go) |
| Rate-limit handler does not leak PII into the audit log | `TestAuthRateLimitHandlerLogsSecurityEventWithoutPII` in [cmd/ovumcy/main_test.go](cmd/ovumcy/main_test.go) |
| The Fiber request log's error field is sanitized (emails → `:email`, opaque tokens → `:token`) | `TestSafeLogError` in [internal/api/request_logging_test.go](internal/api/request_logging_test.go) |

### Medical Safety Disclaimer

The backend HTML contract normally forbids asserting localized copy, but the persistent medical-safety disclaimer is a deliberate exception: its exact wording is the invariant, so these tests pin both the surface's stable `data-*` hook and the safety string.

| Claim | Enforced by |
| --- | --- |
| Every owner-facing prediction surface (dashboard, stats, calendar) renders the persistent "estimates, not medical advice or a method of contraception" disclaimer, so a template refactor cannot silently drop it from a health-prediction page | `TestDashboardRendersPredictionDisclaimer`, `TestStatsRendersPredictionDisclaimer`, `TestCalendarRendersPredictionDisclaimer` in [internal/api/dashboard_prediction_disclaimer_test.go](internal/api/dashboard_prediction_disclaimer_test.go) |
