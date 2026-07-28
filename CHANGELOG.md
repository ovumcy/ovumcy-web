# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **A readiness probe, `GET /readyz`, that actually checks storage.** The whole
  health-check chain — container `HEALTHCHECK`, `docker compose ps`, an operator
  `curl` — hung off `/healthz`, which never touches the database. Every one of
  them stayed green with the storage layer completely gone, so an app that could
  not serve a single request reported healthy. `/readyz` runs one trivial query
  against the configured engine and answers `200` when it succeeds, `503` when it
  does not. Both responses carry a fixed one-word body: the endpoint is
  unauthenticated, so it never reveals the engine, the database path, or the
  error. On the shell-free runtime image the matching `ovumcy readycheck`
  subcommand probes it in process, next to the existing `ovumcy healthcheck`.

  `/healthz` is unchanged and stays a pure liveness probe, and the container
  `HEALTHCHECK` deliberately stays pointed at it: a database that is slow for ten
  seconds, or a Postgres container restarting underneath the app, must not
  restart the app container. Point your load balancer's drain check at `/readyz`;
  leave the container health check where it is. See *Health Checks by Deployment
  Mode* in [docs/self-hosted.md](docs/self-hosted.md).

### Changed

- **The calendar feed verifies its subscribe token with a keyed MAC instead of
  bcrypt.** The verifier half of a feed token is 160 bits from `crypto/rand`, so
  a work factor bought no guessing resistance while costing roughly 265 ms of CPU
  on every request to an endpoint that needs no credential to reach. Verification
  now compares an `HMAC-SHA256` derived from `SECRET_KEY` (still keyed, so a
  database leak alone does not permit offline verifier guessing) and the
  selector-miss timing equalization moved with it, so an unknown selector still
  cannot be timed apart from a wrong verifier. Migration `032` adds
  `users.calendar_feed_verifier_mac`; the bcrypt column stays and is still
  written, so a rollback keeps working.

  Two operator-visible consequences:

  - **Rotating `SECRET_KEY` now disarms armed calendar feeds.** A stored MAC that
    no longer matches is refused outright and is deliberately not re-checked
    against the bcrypt hash. Subscribed calendar clients receive `404` until each
    owner generates a fresh subscribe URL from Settings — the same class of
    consequence rotation already had for 2FA secrets and stored webhook URLs.
    Feeds armed before this release, which a MAC mismatch cannot reach (their
    bcrypt hash never depended on the key), are disarmed by the boot-time check
    described under *Security* below.
  - **Existing subscriptions keep working across the upgrade.** A feed armed
    before this release has no MAC (it cannot be derived from a hash) and keeps
    verifying through bcrypt; its MAC is written in during the first request that
    presents the correct token, after which it takes the fast path. Only a
    `SECRET_KEY` rotation (or a mistyped key at boot) disarms them — see
    *Security* below.

- **A request whose headers overflow the read buffer now answers with a stable error
  key.** The transport-level `431` previously returned Fiber's bare English string;
  it now travels the same mapped-error path as the `413`, answering
  `request_headers_too_large`. A client that parsed the old response text sees a
  JSON envelope instead. The rejection is also logged explicitly, because the
  request logger records these as `404` with an empty path — the head never parsed,
  so nothing tied the user's `431` to the server side. Reachable only when another
  service on the same domain contributes cookies or headers; Ovumcy's own cookies
  are ~450 B on a normal signed-in request. See *Troubleshooting* in
  [docs/self-hosted.md](docs/self-hosted.md).

- **The container image is published only after the commit's own CI passes.** A
  push to `main` used to start the publish workflow alongside the test, e2e and
  scan workflows for the same commit, and the publish normally finished first:
  `ghcr.io/ovumcy/ovumcy-web:latest` was anonymously pullable before anything had
  judged that commit — including the image scan that could still fail it. Branch
  protection guards the merge, not the commit the merge produces, so nothing else
  covered this. Publication is now the last job of the CI run, behind `test`,
  `race`, `e2e`, `e2e-postgres-smoke` and `image-smoke`, and it scans the exact
  image it is about to push before the first byte reaches the registry.

  Operator-visible effect: `:latest` now trails a push to `main` by a full CI run
  instead of a couple of minutes, and stays on the previous commit whenever a
  check fails. Release tags publish from their own tag push as before, now with
  the same pre-publish scan, and the signer identity to verify with `cosign` is
  unchanged for both — the commands in
  [SECURITY.md](SECURITY.md#verifying-release-authenticity) still apply as
  written.

### Added

- **An account signed in through an identity provider can now erase its own
  data.** `Clear data` and `Delete account` require a fresh re-authentication,
  and that had exactly one accepted form: the current local password. An owner
  provisioned through OIDC has none, so both flows answered `403 local password
  required` — while `SECURITY.md` and the GDPR cross-reference described the
  right to erasure without qualification. The workaround (enrol a local password
  through the step-up flow, then erase) worked but was documented nowhere near
  the claim it qualified.

  Such an account now confirms an erasure where it already authenticates. The
  danger zone offers the action directly; confirming it starts the same
  purpose-bound step-up that gates local-password enrollment, and the erasure
  runs when the provider sends the browser back — never before. Which erasure
  was confirmed travels inside the sealed step-up state, not in the callback
  request, so nothing observable between the two can turn a data wipe into an
  account deletion.

  The re-authentication requirement itself is unchanged, and it does not
  downgrade: an account that **has** a local password is refused this route and
  keeps confirming with its password.

### Fixed

- **The language switch is rate-limited.** `POST /lang` is the one
  unauthenticated route outside `/api` that reads a request body, so the `/api`
  budget's path prefix never reached it and it was the only body-reading
  surface in the application with no volume control at all. CSRF keeps a
  cross-origin attacker out but is not a cap. It now carries the ordinary API
  budget — the endpoint costs a cookie write, so it needs no knob of its own —
  and the limiter sits ahead of the CSRF check, because a cap has to bound
  requests that never reach the handler.

- **Every rejection now answers in the app's own error format, not the web
  framework's.** The mapped envelope (`error` plus the structured
  `error_detail`, negotiated into a localized status fragment for the browser
  flows) was documented as an app-wide contract but reached only three statuses:
  the two pre-routing rejections, `413` and `431`, and the request-budget `503`.
  Every other explicit framework error fell through to a bare English string
  regardless of what the client asked for — which is what a CSRF refusal
  returned on **every** state-changing endpoint (`Forbidden`), what the language
  switcher returned on a blank submission (`Bad Request`), and what the calendar
  feed returned on an infrastructure failure (`Internal Server Error`). A client
  that had learned to parse the envelope met an unparseable body on the most
  common refusal in the app, and none of those strings could be localized or
  branched on.

  Rejections are now mapped from the HTTP status to a stable key —
  `bad_request`, `forbidden`, `method_not_allowed`, `unsupported_media_type`,
  `internal_error`, `service_unavailable`, with `request_rejected` /
  `internal_error` covering any status not named — and answered through the same
  negotiation as every other mapped error. `401`, `404`, and `429` deliberately
  reuse the keys those statuses already carried elsewhere in the app, so one
  status keeps one key whichever layer produced it, and the `413`/`431` keys are
  unchanged. The message an error carried internally is never echoed into the
  response body. Localized copy for the new keys ships in all six locales;
  `/healthz` and `/readyz` keep their fixed one-word bodies and are deliberately
  outside the envelope.

- **The audit stream now tags the two erasure actions as health-data
  mutations.** Clear-data and delete-account logged through the plain
  security-event path, so their lines carried the action name alone — no
  `domain="health_data"`, no `target` — while an ordinary day write or symptom
  edit carried both. An operator filtering an incident by domain to answer
  "was any tracked data destroyed?" therefore saw every routine edit and missed
  the two operations that erase everything. Both now declare a typed mutation
  kind, like every other audited mutation handler, so the tag cannot be dropped
  by writing the action out by hand at a call site.

  The `action` values are unchanged (`settings.clear_data`,
  `settings.delete_account`) and existing filters keep matching, but each of
  those lines gains two fields: `domain="health_data"` and a `target` naming the
  erased scope — `account_data` for the wipe, `account` for the deletion.
  Neither carries an identifier or any free text. The password-only pre-check
  behind the clear-data confirmation dialog (`settings.clear_data_validate`)
  mutates nothing and stays on the plain path, unchanged. Field reference:
  [docs/security/logging.md](docs/security/logging.md#logging-policy).

- **The TOTP enrollment cookie refuses to seal without a secret.** The reader
  has always rejected a setup payload whose secret is blank, but the writer
  sealed one without complaint, so a caller that lost the secret produced a
  well-formed, correctly-attributed cookie that could only fail later, at read
  time, in a different request. The writer now refuses a blank secret exactly
  as it refuses a missing owner id — the failure surfaces in the request that
  caused it, and no prior setup cookie is left behind.

- **Requests now carry a deadline, so work the caller abandoned stops.** Nothing
  in the request path was bounded: fiber v3 hands a handler `context.Background()`
  until something calls `SetContext`, so the ctx threaded handler → service →
  repository had neither a deadline nor a cancellation, and `database/sql` waited
  for a free connection indefinitely. fasthttp does not cancel a handler when the
  client disconnects either, so abandoned work kept running — a burst of 120
  concurrent day writes held an instance unready for **16.6 minutes** after the
  last client had given up, answering `500` with latencies past 14 minutes, while
  `/healthz` stayed green at 1 ms and the container kept reporting healthy. Every
  request is now bounded at 60 seconds (matching the server's write timeout, and
  far outside any legitimate request including a full-size import), and one that
  outlives its budget answers `503` with the stable key `request_timeout` instead
  of whatever internal `500` the domain that caught the expiry happened to map.

- **`ovumcy reset-password` can be run without an interactive terminal.** It is
  the operator's documented way back in for an owner locked out by a
  `SECRET_KEY` rotation, or for an OIDC-only account with no retained recovery
  code — but it prompted unconditionally, so the plain `docker exec` form the
  runbook shows for the other subcommands failed with `secure password prompt
  requires an interactive terminal`, and recovering several accounts at once
  could not be scripted. It now reads the password from piped stdin exactly as
  `users create` already did, keeping the interactive prompt when stdin is a
  terminal. The password still never travels in argv or the environment. The
  runbook gained a section showing both invocations against the shell-free
  image.

- **The startup banner can name the revision it is running.** `rev:` read the
  VCS stamp Go embeds during `go build`, and the Docker build context excludes
  `.git`, so every container image — including every published release — printed
  `rev: unknown`. The commit sha the image build already receives went only to
  the asset cache-bust token. The banner now falls back to that same stamp when
  no VCS revision is present, so an operator following the upgrade procedure can
  confirm which build actually booted instead of trusting the image tag alone. A
  VCS stamp still wins when there is one: it is the only source that can report
  a dirty tree, and a release stamp must not hide that.

- **The 2FA challenge now accepts the JSON body its published contract
  advertises.** `POST /api/v1/sessions/2fa-challenge` is documented in
  `docs/openapi.yaml` with a single request media type, `application/json`, but
  the handler read the code through `c.FormValue`, which never sees a JSON body.
  A client written against the published spec therefore got
  `401 totp invalid code` for every code it submitted — the same answer a wrong
  code gets, so the failure read as "the user is typing it wrong" rather than as
  an unimplemented contract. The browser flow posts a form and was unaffected,
  which is why nothing caught it: the only callers in the tree, the Playwright
  spec and the Go regression, both submit forms. The endpoint now reads the code
  from either transport, and one regression drives a valid code through both and
  asserts each documented outcome (`200` for JSON, `303` for the form). A sweep
  of all 28 request bodies in the spec against their handlers found this to be
  the only endpoint whose declared media type the implementation did not accept.

- **The request log now redacts short credentials, not just long ones.** The
  always-on Fiber request log sanitizes its error column so a handler error
  string cannot carry a secret into the log, but it recognized a secret only by
  length: a run of 24 or more token characters. The 48-character calendar-feed
  token and the 32-character TOTP secret cleared that floor; a recovery code
  (`OVUM-XXXX-XXXX-XXXX`, 19 characters) and a submitted six-digit code did not,
  and would have been written out verbatim. Both are now matched by shape and
  masked as `:code`. The length floor is deliberately unchanged — lowering it far
  enough to catch a six-digit code would also redact dates, status codes, route
  templates and ordinary identifiers, and this column is the only diagnostic
  signal an operator has for a failed request.

- **Re-running a migration whose `ADD COLUMN` sits behind its file's prose no
  longer aborts the boot.** SQLite has no `ADD COLUMN IF NOT EXISTS`, so a
  migration is safe to replay only because the runner skips an `ADD COLUMN` whose
  column already exists. That check ran against the raw statement chunk, and the
  chunk splitter keeps a file's leading comment block attached to the first
  statement, so the check never recognized an `ADD COLUMN` introduced by prose —
  and stopped protecting the first column of migrations `021`, `027`, `029`,
  `030` and `032`. On a database that carries the column while its
  `schema_migrations` row does not — a restore from a backup taken before that
  row was written, or a pruned migration table — the replay failed with
  `duplicate column name` and the application did not start. Detection now looks
  past leading comment lines. Which statements execute is unchanged, on both
  engines; only the already-exists skip sees more of them. A permanent guard
  walks the embedded migration set and fails if any `ADD COLUMN` is left
  unrecognized.

- **"Cancel" now actually cancels on four confirmation dialogs.** Rotating or
  revoking the calendar feed, hiding a custom symptom, and deleting a calendar
  day entry each asked for confirmation — but the request was already on its way
  before the dialog appeared, so cancelling still performed the action (and
  confirming performed it twice). Rotating a feed the owner had decided to keep
  invalidated a subscribe URL every subscribed calendar client was using. The
  dialog on these surfaces was decorative; it now gates the request, and a
  template-level guard fails the build if any future htmx-driven control is
  wired the same way.

- **Two-factor and SSO-link errors are localized again.** A wrong 2FA code showed
  every user the untranslated English string `totp invalid code`, regardless of
  the interface language, and the OIDC link-confirmation page did the same with
  its own error strings. The translations existed in all six locales the whole
  time; nothing connected them to the errors the pages were reporting. Affects
  displayed text only — no flow, status code, or rate limit changes.

- **Email sign-up and sign-in accept only a plain address, and legacy rows are
  repaired at boot.** The old input rule validated a full RFC 5322 form
  (`jane doe <jane@example.com>`) but stored the whole string verbatim, so such
  an account could never sign in with its plain address and a second account on
  the same mailbox could slip past the duplicate check. Input is now strictly
  the bare address (display names, comments, and quoted local parts are
  rejected as invalid). On the first start after upgrading, previously stored
  decorated rows are rewritten to their bare parsed address — the startup log
  reports the count; a row whose bare address is already another account's, or
  that cannot be reduced to a plain address, is left untouched for operator
  review. See *Troubleshooting* in [docs/self-hosted.md](docs/self-hosted.md).
- **Deleting an account no longer leaves an SSO logout reference behind for a
  week.** `oidc_logout_states` gained its `user_id` column in migration `031`, so
  rows written before that upgrade carry no owner and the account-deletion path
  could never match them. On an instance that ran OIDC before `031`, an
  `id_token_hint` minted for an account outlived that account's erasure and sat
  in the table until its own 7-day TTL expired. Migration `033` deletes every
  unattributed row on both engines, so erasure is complete when it is requested
  rather than up to a week later. Attributed rows are untouched, and a logout
  that finds no stored state still completes locally.

- **"Clear data" now also resets the stored timezone.** Every other preference
  returned to its default while `users.timezone` kept the last zone the owner's
  browser reported — a coarse location signal left standing by the one gesture
  meant to wipe the account clean. It now resets with the rest. Nothing else
  changes: the next request re-detects the zone and stores it again, so reminder
  scheduling is unaffected, and account identity (email, password, display name,
  2FA, SSO links) is preserved exactly as before.
- **A subscribed calendar could show predicted days shifted by a day.** The
  `.ics` feed decided which day was "today" from the timezone of the request
  that fetched it, but a calendar client sends neither the timezone header nor
  the cookie that chain reads, so every poll fell back to the server's timezone.
  An owner whose timezone differed from the server's could therefore see a
  predicted period or ovulation day appear or drop off a day early or late — and
  disagree with the webhook reminder for the very same prediction, which already
  used the owner's stored timezone. The feed now resolves "today" in the owner's
  stored timezone as well, falling back to the request/server zone only for an
  owner whose timezone has never been captured. See
  [docs/notifications.md](docs/notifications.md).
- **A failed database migration no longer leaves the database open behind it.**
  Opening the database connects first and applies the pending migrations second;
  when that second step failed, the error came back without the connection ever
  being closed. The caller receives no handle in that case and so has nothing it
  could close, which left the connection pool — and, on SQLite, the open database
  file — held for as long as the process lived. The web binary exits moments
  later, so a failed start was barely affected; an operator subcommand
  (`ovumcy users`, `reset-password`, `notify`, `webhook`) keeps running, so it
  held the database open for the rest of its run — which on Windows also blocks
  the file from being moved or deleted. The connection is now released on any
  failure that happens after it is open, and the error reported to the operator
  is still the migration failure that caused it.

- **`docs/openapi.yaml` promised input bounds wider than the ones the server
  actually enforces.** `ProfileSettings.display_name` declared `maxLength: 80`
  against a 64-rune cap; `CycleSettings.cycle_length` declared `minimum: 1`
  and no maximum against an accepted range of 15–90;
  `CycleSettings.period_length` declared no maximum against 1–14; and
  `SymptomPayload.name` and `.icon` declared no length bound at all against
  caps of 40 and 16 runes. The settings form has shipped the real limits as
  `maxlength` attributes all along — only the published document disagreed.

  A spec looser than the server blocks nothing, which is why this went
  unnoticed, but it is untrue in the direction a client cannot recover from:
  it cannot tell in advance that a value will be refused, so a 65-character
  display name or a 41-character symptom label validates cleanly against the
  published document and is only rejected on send. That is a round trip spent
  discovering a limit the spec was supposed to state, and for a form that
  submits a whole settings section at once it is a rejection a client has to
  explain after the fact rather than prevent.

  Each bound is now declared at the value its endpoint enforces, and the
  string bounds say what they are measured on: the server counts **runes**,
  not bytes, and counts them after normalization (trimming for a display name
  or icon, whitespace collapsing for a symptom name), so a client validating
  by character count agrees with it. Two constraints no numeric bound can
  express are stated in prose instead of left invisible — `period_length` must
  also be at most `cycle_length` minus 10, and the identically-named fields on
  `OnboardingStep2Request` are clamped into range rather than rejected, so
  those two schemas stay separate even where their numbers agree.
  Description-only: no validator and no accepted request changed.

  `TestOpenAPIDeclaredBoundsMatchTheServersOwnLimits` now sweeps every one of
  these bounds and restates none of them — it reads the number out of the spec
  and makes the endpoint judge it, requiring the declared bound to be the
  acceptance boundary itself rather than merely close to it. Each member was
  proven against its original defect: with the loose values restored the sweep
  fails naming the field, the keyword, and the direction of the drift.

### Security

- **A two-factor cookie the server refuses is now cleared instead of left in the
  browser.** `ovumcy_totp_pending` and `ovumcy_totp_setup` are both
  session-scoped: they carry no expiry attribute of their own, only a
  payload-bound five-minute validity the server checks on every read. That check
  never stopped working, so a stale value could not be used — but nothing
  retracted it either, and the browser kept sending it on every request until it
  closed. The enrollment cookie is the one that matters: its payload is the raw
  TOTP secret of an enrollment that was abandoned, and it stayed in transport
  long after the five minutes were up.

  Both readers now clear the cookie on every refusal — an envelope that will not
  open, a tampered value, a payload that is not the expected shape, one naming no
  account, one carrying no secret, one minted for another account, and an expired
  one — matching what the reset-password, recovery-code and OIDC-logout cookies
  already did. Nothing changes about what the response says: every one of these
  still answers the same "session expired" result, so the response cannot be used
  to tell an expiry apart from a payload minted elsewhere. A refusal about the
  submitted code is deliberately not a clear — a mistyped six-digit code leaves
  the challenge and the pending enrollment exactly where they were, and can be
  retried.

- **Signing out now retracts the pending two-factor enrollment cookie and the
  one-time calendar-feed reveal cookie.** Logout cleared the auth cookie, the
  provider-logout bridge, the recovery-code and reset-password handoffs and the
  2FA challenge cookie, but not `ovumcy_totp_setup` or `ovumcy_calendar_feed`.
  Both are scoped to `path: "/"`, so an owner who opened the two-factor setup
  page, abandoned the enrollment and signed out kept sending the sealed raw TOTP
  secret on every subsequent request for the rest of the browser session — that
  cookie has no browser-side expiry, only a TTL inside the sealed payload, so
  nothing dropped it until some later request happened to read and refuse it. The
  feed cookie carries the subscribe URL with its bearer token, which signing out
  does not revoke.

  Neither is a disclosure on its own: both are `HttpOnly` and AEAD-sealed, and
  both are already refused unless the session presenting them is the account they
  were minted for. What was missing is that ending a session should retract them
  outright rather than leave a live secret riding every request. The same helper
  covers logout, account deletion and the paths that reject a session, so all
  three retract the full set. Nothing consumes either cookie without an
  authenticated session, so no flow changes: reopening the two-factor page after
  signing back in issues a fresh secret exactly as before.

- **The pending two-factor enrollment cookie is now bound to the account that
  started the enrollment.** The sealed `ovumcy_totp_setup` cookie carries the raw
  TOTP secret across the enrollment form submission, and it was the only sealed
  cookie in the transport layer holding a secret with no owner id at all: the
  confirm step validated its shape and expiry, then enrolled whatever secret it
  carried against the signed-in account. On an instance hosting several
  independent owners, a setup cookie minted for one account and presented on
  another account's session would have enrolled the first account's pending
  secret as the second account's own credential. This is credential confusion,
  not disclosure — the cookie is `HttpOnly` and sealed, the confirm step already
  required a fresh password, and no ordinary flow reaches the state — but the
  binding was the only structural control missing.

  An owner id is now mandatory when the payload is sealed, so an unattributed one
  is never minted, and on read the id must match the session presenting it: a
  payload naming a different account, or none at all, is refused and the cookie
  is cleared rather than left presentable on a retry. The check reuses the same
  predicate as the one-time reveal surfaces, so the three cannot drift apart.

  **Upgrade consequence.** A setup cookie minted by the previous version carries
  no owner id and now fails closed. An enrollment that is in flight across the
  upgrade — the QR code on screen, the confirmation code not yet submitted — is
  refused with the usual "enrollment session expired" response; reload the
  two-factor settings page and start the enrollment again. Accounts with 2FA
  already enabled are unaffected, and nothing about sign-in changes.

- **A one-time reveal now refuses a sealed payload that names no owner, instead
  of skipping the owner check.** Both shown-once surfaces — the recovery code and
  the calendar-feed subscribe URL — compared the owner id sealed into the cookie
  against the signed-in account, but the comparison was written so that a payload
  carrying owner id `0` disabled it: with no id to match, the check was skipped
  and the secret rendered for whichever session presented the cookie. No caller
  could produce such a payload, so nothing was exposed in any released build;
  the defect was that only the callers stood between an unattributed payload and
  a reveal.

  An owner id is now mandatory when the payload is sealed, so an unattributed one
  is never minted, and a payload naming no owner is invalid on read rather than a
  check that does not apply — refused and cleared, on either surface, whoever
  presents it. Both surfaces share one predicate, so the two cannot drift apart
  again. Behavior for owners is unchanged: a reveal minted for an account still
  reveals to that account, exactly once.

- **A compressed request body is no longer decompressed on routes that never
  read one.** The transport guard that answers the mapped `413` for a body
  crossing the cap only once decompressed ran on every route, because its only
  skip condition was the absence of a `Content-Encoding` header. Probing *is*
  decompressing, so a small, highly compressible upload — 16 KB on the wire
  expanding a thousandfold, sized to stay just inside the cap and therefore
  answered `200` — was inflated in full on `/healthz`, `/readyz`, `/login`,
  `/register`, `/privacy` and `/favicon.ico`: unauthenticated `GET` routes whose
  handlers read no body and which no rate limiter covers. Twenty such requests
  cost 421 ms and 643 MiB of allocation against 0.5 ms and 2.2 MiB once the
  probe is skipped. The probe is now owed by request method, so it still runs on
  every method that can reach a body reader — including `DELETE`, whose body
  carries the confirmation password for account deletion and 2FA disable — and a
  route added later inherits it from its method rather than from a list. The
  `413` contract for body-reading endpoints is unchanged.

  Also in the guard: an over-limit body is now recognized by testing for the
  `413` itself rather than for a change of status, so a `413` already standing on
  the response can no longer short-circuit the check and pass the framework's
  substituted error string to the handler. Nothing upstream stamps one today;
  the guard no longer depends on that.

- **`ovumcy notify --dry-run` no longer prints predicted period and ovulation
  dates by default.** The preview printed one line per due reminder carrying the
  owner id, the reminder type, and the estimated date — a prediction about an
  identified owner — while the command, the report type, and
  [docs/notifications.md](docs/notifications.md) all stated that the output
  carried no health specific, and the same document showed a cron recipe
  redirecting that output into a log file. A scheduled dry run therefore wrote
  special-category health data into storage chosen on the strength of a promise
  the command did not keep. The default preview now names the owner, how many
  reminders are pending, and the destination host, and stops there. The new
  `--show-health-details` flag puts the type and estimated date back for an
  operator who asks for them on purpose; it applies only to `--dry-run`, and the
  documentation is explicit that its output does not belong in a log file. The
  scheduled delivery pass is unchanged and never had a preview. One related log
  line dropped its reminder type for the same reason: a failed watermark write
  after a successful send now records the owner id alone.

- **`ovumcy webhook set` refuses an endpoint URL supplied from two sources at
  once.** `OVUMCY_WEBHOOK_URL` was read before `--url-stdin` and returned
  immediately, so with the variable exported the piped URL was never read and
  nothing said so. A value left over from an operator profile, an earlier
  invocation, or a compose `env_file` inherited by `docker compose run` would
  arm that owner's reminders at an endpoint nobody typed — on a household
  instance plausibly another owner's topic, and the URL is a secret that can
  embed an ntfy or Gotify token. Supplying both is now an error naming each
  source, raised before the database is opened, so nothing is written either
  way. Either source on its own behaves exactly as before, and `--clear-url`
  still wins over an exported variable: removing an endpoint cannot arm the
  wrong one.

- **A compressed request body that only passes the 16 MiB cap once decompressed
  now answers with the standard `413 request_too_large` envelope.** The cap is
  applied to the decompressed stream inside the framework's body accessor, which
  reports the overflow by stamping `413` on the response and handing the handler
  a short internal error string in place of the payload — after the request has
  already been routed. Two things followed from that substitution: the JSON
  restore parsed the substituted string, failed, and answered `400 invalid import
  file`, blaming the owner's export for what was a size rejection; and any route
  that read the body without writing a response of its own leaked the framework's
  bare-text `413`, which the app-wide error-envelope contract forbids. The
  overflow is now detected once at the transport layer, before any handler runs,
  and answered through the same mapped, content-negotiated envelope as the
  wire-level `413`. No service ever receives the substituted bytes, so an
  oversized upload can no longer be reported as a malformed file. An uncompressed
  body is unaffected — its wire size and parsed size are the same number — and a
  compressed body inside the cap still restores normally. See
  [docs/export.md](docs/export.md) → Size limits.

- **A `SECRET_KEY` rotation now revokes calendar-feed subscriptions of every
  generation.** Rows armed before migration 032 carry only a bcrypt verifier
  hash, which does not depend on `SECRET_KEY` — a rotated key left those
  subscribe URLs verifying, and the first successful poll would even write a
  fresh keyed MAC under the new key, fully re-arming a leaked URL the rotation
  was meant to kill. The server now keeps an irreversible key-epoch marker in
  `app_state` and, on the first boot under a changed key (or a bumped feed-MAC
  label set), disarms every armed feed row that has no MAC before accepting
  requests, logging the count. The first boot after upgrading records the
  baseline without disarming anything; rotating in that same maintenance window
  is therefore not detectable — boot the upgrade once before rotating, or have
  owners revoke feeds manually (see [docs/self-hosted.md](docs/self-hosted.md)
  → Secret Handling and Rotation).

- **Clearing a cross-site OIDC cookie now forces `Secure`, matching the write
  path.** `ovumcy_oidc_auth` and `ovumcy_oidc_stepup` are `SameSite=None`, and
  the write path has always forced `Secure` regardless of `COOKIE_SECURE`
  (browsers drop a `SameSite=None` cookie without it); the clear path computed
  `Secure` from `COOKIE_SECURE` alone, so under `COOKIE_SECURE=false` it would
  have cleared them without `Secure` — a cookie the browser drops instead of
  expiring. Unreachable today for two reasons outside this code: the boot
  guard refuses `OIDC_ENABLED=true` with `COOKIE_SECURE=false`, and Fiber
  itself forces `Secure` on any `SameSite=None` cookie regardless of the input
  value. Closes a defense-in-depth gap where the invariant rested entirely on
  those two external guarantees instead of its own.

### Internal

- **Rendered copy in the browser suite is addressed through `data-*` hooks, and
  the per-language literal branches are gone.** Sixteen specs pinned localized
  sentences as their only address — `getByRole('link', { name: 'Today' })` ×6
  languages, `getByText('Stress')` for chips the test itself had seeded by key,
  `/Menstrual|Менструальная|Menstrual/i` (an alternation whose EN and ES arms are
  the same word, so it matched any phase in either language), and negated phrases
  such as `not.toContainText('Cannot be changed.')` that had been tautologies
  since the copy was removed from the templates. Each now addresses a hook and
  asserts state: `data-dashboard-phase` on the cycle hero, `data-fertility-badge`
  + variant/key, `data-cycle-factor` on every static factor chip,
  `data-symptom-pattern-day-start`/`-end` and `data-stat-card="cycle-range"` on
  the stats cards, `data-future-cycle-start-notice` shared by the two specs that
  used to pin the same notice two different ways, `data-symptom-create-error`
  next to the existing row-level error, `data-title-key` on every page `h1`, and
  `data-nav-link` on the three nav renderings. Nine explainer sites converged on
  `toHaveAttribute('data-explainer-key', …)`, and the `/disable/i` caption that
  was the sole discriminator in six 2FA tests became the form's own
  `hx-delete` endpoint. Template edits add attributes only — no markup, class, or
  copy changed.

  Where rendered copy is genuinely the subject (the language-switch spec, the
  registration enumeration scan), the expected strings now come from
  `internal/i18n/locales/*.json` through one helper (`e2e/support/locale-helpers.ts`)
  instead of being re-typed per language. That is what makes the enumeration scan
  cover all six locales rather than the two it happened to list: it derives the
  account-existence keys from the English catalogue and expands them across every
  shipped language, so a seventh locale is covered the day it lands. The two
  EN-US date-shape regexes are replaced by `Intl`-derived expectations that
  verify each fragment is a real calendar date in the app's display format —
  `/\w{3} \d{1,2}, \d{4}/` matched `Foo 99, 0000`.

- **Every remaining e2e wait now binds to a concrete signal instead of to a
  sleep or to page state that is already true.** The suite's last two fixed
  250 ms sleeps, which guarded the two XSS "no dialog fired" claims, are gone:
  the assertions they followed already prove the payload had its chance — the
  server rejected it and nothing rendered in one test, it is present as *text*
  rather than markup in the other, so no element exists to fire a deferred
  `onerror`. `completeOnboardingIfPresent`, which nearly every spec calls, no
  longer swallows a hung post-registration redirect: the wait stays tolerant of
  the redirect having already landed, but the settled path is now asserted, so
  "the redirect never happened" fails at the helper rather than resurfacing as
  an unrelated failure further down the caller. Calendar and BUG-06 month
  navigation assert the specific month each control points at, instead of a
  `?month=` shape the URL already matches before the click. The two
  "Cancel/Escape did not navigate" claims — the settings leave guard and the
  logout confirmation — now record what the page put on the wire, or where the
  main frame went, across the whole window and close it on a reload or on a
  state transition; the dashboard manual-cycle-start cancel joins them, having
  asserted only an empty error status, which also holds when the POST fired and
  succeeded. Two calendar `await request.response()` calls that discarded the
  status now assert it. Every replacement assertion was proven live by breaking
  what it guards — accepting the dialog it cancels, aiming the onboarding helper
  at a path that never arrives, naming the wrong month — and watching it fail
  while naming the escaped request, the navigation, or the stuck URL.

- **Three latent e2e-suite defects, each proven by an executed probe before the
  fix and re-proven closed after it.** The day-editor save helper
  (`saveDayEditorForm`) moved from a single spec into `e2e/support/stats-helpers.ts`
  and now backs the five save-then-navigate call sites that raced their own PUT
  — with 1.5 s of injected server latency the old form silently lost the save
  and the test failed on its own persistence check; the helper version survives
  the same probe. The two hand-rolled settings-language saves ride
  `saveSettingsLanguage` for the same reason. The 2FA invalid-code test now
  anchors on the rendered `error.totp_invalid_code` server error — a handler
  replaced by an unconditional bounce back to `/auth/2fa` used to pass both of
  its negative assertions, and now fails naming the missing control. The
  registration URL-leak test drives the server path directly with a weak-password
  `POST /api/v1/users` and pins the clean `303` redirect: the client-side
  validator swallows the UI submit before it reaches the network (an intercept
  counted zero requests), so the previous version never exercised the surface it
  was written for. Separately, every direct `page.request` call now sends an
  explicit `Origin` — the suite convention that keeps direct API calls valid
  under the HTTPS posture — and the GET-only export calls drop their dead
  `csrf_token` form bodies (CSRF gates state-changing methods only, and a GET
  should not carry a request body at all).

- **Negative-only and self-adapting e2e tests anchored, so a dead mechanism can
  no longer keep them green.** Six browser tests asserted only that something
  was absent, unchanged, or not created; each now proves the same surface in the
  state where it must appear, inside the same test — auto-fill markers rendered
  for a second owner with the toggle on, the populated day editor before it is
  cleared, an import that really moves the exported-day set, the cycle-warning
  block for a stale baseline, the factor hint once a tag exists, and the neutral
  `/login` landing the duplicate-registration decoy pickup produces. Three of the
  anchors were proven by executed probe: with onboarding auto-fill disabled, the
  day write silently dropped, and the import writer short-circuited, each test
  fails naming its control, while the negative half stays green — which is what
  the previous versions checked. Four adaptive branches lost the escape hatch
  that made them unfalsifiable: the onboarding reload now pins both halves of the
  behaviour it observed (submitted step-1 progress kept, unsaved step-2 slider
  reset to the persisted value), the stats BBT summary is asserted
  unconditionally, the OIDC hybrid-link fallback must prove the account it claims
  already existed, and the second rejected symptom-create binds to its own POST
  instead of re-reading the first attempt's error. Two weak assertions were
  corrected along the way: an irregular-cycle test named for a date range looked
  for an ASCII `" - "` that no branch emits (the range separator is an em dash,
  and the state under test renders no range at all — the name was wrong), and the
  suppressed cycle warnings are now addressed through the block's own `data-*`
  hook instead of a bare styling class.

- Browser coverage that cancelling a confirmation is inert now reaches deleting
  a calendar day entry — where the action has no undo — and hiding a custom
  symptom, instead of the calendar-feed settings flow alone. Each new test
  records what the page put on the wire while the dialog was dismissed, asserts
  nothing mutating was sent and the data survived a reload, and anchors that
  against the same control accepted, which must still perform the action. The
  dialog's own captions are asserted as rendered button text rather than only as
  declared attributes.

## [1.9.2] - 2026-07-24

Italian localization polish and a CI unblock. No database migrations; no
breaking changes.

### Changed

- **Italian translations de-anglicized for consistency.** The Italian locale now
  uses native terms across the whole catalog: "spotting" → "perdite" in the flow
  and symptom labels and the onboarding/dashboard tips that reference them, and
  "export" (noun) → "esportazione" in the import, privacy, and data copy — with a
  gender-agreement fix in the invalid-import message ("un'esportazione … valida").
  (#263, #264)

### Fixed

- **Merges unblocked in CI.** Dropped the `brace-expansion` override that had
  itself become the advisory finding scanned by `trivy-fs`, and the Codecov gate
  that was failing required checks. (#262)

## [1.9.1] - 2026-07-20

Follow-up remediations from the external audit: privacy and medical copy now
match actual behavior (opt-in egress, no diagnostic certainty), day-save
feedback explains the pregnancy-test pause, and a logged future period entry
renders as a recorded fact instead of a projection. No database migrations;
no breaking changes.

### Changed

- **Privacy copy matches actual egress behavior.** Onboarding and privacy-policy
  strings in all six locales no longer claim data "never" leaves the server:
  data leaves only through integrations the owner enables (webhook reminders,
  OIDC sign-in). The rights section states the real export scope (day-level
  health records; account profile and settings stay viewable in Settings)
  instead of promising a "full copy". `docs/security/data-handling.md` is now
  the canonical egress statement; README and `docs/gdpr.md` defer to it.
  `docs/gdpr.md` also states the positioning explicitly: built for personal and
  household self-hosting, not a turnkey GDPR solution for a public multi-user
  service. (#260)
- **Implantation-bleeding hint softened with red-flag guidance.** The dashboard
  hint no longer asserts a cause from timing alone and directs to medical care
  on pain, dizziness, or heavy flow. All six locales. (#260)
- **Saving a day under a pregnancy-test pause explains the pause.** With a
  positive pregnancy test as the latest fertility signal, the day-save feedback
  says predictions are paused and carries red-flag guidance, instead of a
  routine self-care/fertile message. (#260)
- **Prediction docs disclose the full heuristics.** `docs/cycle-prediction.md`
  documents the cervical-mucus "+1 day" ovulation rule and adds a BBT
  sensitivity caveat (indicative, not diagnostic). (#260)

### Fixed

- **A logged period entry dated in the future renders as a recorded fact, not a
  projection.** Auto-fill never writes rows past today, so every future period
  row is a manual log; the "projected period (auto-filled)" calendar style,
  legend entry, and locale key described a state no real data could produce and
  are removed. (#260)

## [1.9.0] - 2026-07-20

Reworks BBT ovulation detection into a single shared "3-over-6" coverline
detector with disturbance rejection, and lands a batch of external-audit
remediations across cycle logic, onboarding privacy copy, import performance,
and accessibility. No database migrations; no breaking changes.

### Changed

- **BBT ovulation detection unified on a "3-over-6" coverline with disturbance
  rejection.** One detector (`detectBBTShiftFirstHighDay`) now drives all three
  surfaces — luteal-phase inference, the calendar tentative-ovulation signal, and
  the stats BBT chart coverline + marker — which previously disagreed. The
  coverline is the max of the six immediately preceding *undisturbed* recorded
  temperatures; a shift is three calendar-consecutive days strictly above it with
  the third day ≥ coverline + 0.2 °C; ovulation is dated to the day before the
  first elevated day. Readings on days tagged `illness` / `sleep_disruption` are
  excluded from the detection series so a fever can neither inflate the coverline
  nor confirm a streak (display series unchanged). The chart draws the coverline
  only once a shift is confirmed; the accessibility summary gains a no-shift
  sentence; all six locales adopt coverline terminology. (#255, #250)
- **Age group is genuinely optional.** A "not specified" option replaces the
  silent `under_40` onboarding default. (#250)
- **Import writes days in one batch** — a single range read plus one chunked
  `CreateBatch` instead of lookup-and-insert per day (with a 20k-unique-date load
  test), speeding up large restores. (#250)

### Fixed

- **Settings section navigation stays pinned below the header while scrolling on
  desktop** (`>=640px`); on mobile it remains a static top-of-page index. (#252)
- **A period day with unset flow now reads "not specified" instead of "none"**,
  which had wrongly implied "no bleeding". (#250)
- **The calendar distinguishes projected (future / auto-filled) period days from
  logged facts**, which were previously indistinguishable. (#250)
- **Removed the false "we do not store identity data" onboarding claim** in all
  six locales — email and display name are stored. (#250)
- Accessibility: password-reveal and mobile-menu tap targets raised to 44px; the
  prediction disclaimer given a visible accent; the onboarding quick-pick group
  labelled. (#250)

### Security

- **Cloud/IMDS webhook guidance.** `docs/notifications.md` now documents that on a
  cloud host an owner-configured webhook URL can reach the instance metadata
  endpoint (`169.254.169.254`) when the private-address gate is off, and
  recommends `WEBHOOK_BLOCK_PRIVATE_ADDRESSES=true` for any cloud (non-LAN)
  deployment. The delivery envelope already discards the response body (no
  exfiltration path); the residual risk is blind internal-network probing by a
  co-tenant owner. Runtime behavior and defaults are unchanged.
- **Startup warning for the exposed webhook combination** — the server logs a boot
  warning when `REGISTRATION_MODE=open` is combined with
  `WEBHOOK_BLOCK_PRIVATE_ADDRESSES=false`, surfacing the multi-owner /
  publicly-reachable SSRF exposure. Defaults unchanged. (#250)

### Internal

- Oversized files split to cut merge/regression risk (#251, #253); the CLI command
  dispatcher and the `webhook set` argument parser decomposed below the gocyclo-15
  gate (#256).
- Test hygiene: onboarding i18n date-field tests collapsed into one table-driven
  test with Italian coverage (#257); round-3 mutation sweeps split from six theme
  aggregates into per-source files, dropping one duplicate (#258); v1.8.x
  mutation-hardening baseline (#246).
- A dependency-free route↔OpenAPI contract test and a `checkStyledControl` e2e
  helper centralizing 12 `force:true` clicks (#250); README contents / Quick-Start
  / digest-pin guidance and `.bin/` excluded from the Docker build context (#250);
  codecov PR-comment noise suppressed (#254); GitHub Actions and npm dev-dependency
  bumps (#247, #248).

## [1.8.0] - 2026-07-11

Adds three opt-in notification/subscription surfaces (webhook reminders, an in-process daily reminder scheduler, and a read-only calendar `.ics` feed), per-user timezone and week-start preferences, Italian localization, and an OIDC response-mode switch, alongside auth hardening. Six additive database migrations (026–031, SQLite and Postgres). The only breaking change is the `swelling` export-shape change noted under Changed.

### Added

- **Webhook reminder notifications (opt-in, off by default).** A new per-owner webhook subsystem delivers period/ovulation reminders to an owner-configured endpoint (migration `027`). The URL is a **write-only secret** — encrypted at rest and never rendered back into the settings page (only a configured/not-configured status and at most the hostname are shown). Delivery is request-free and owner-scoped — one owner's predicted health data never reaches another owner's endpoint — and the outbound envelope is hardened: bounded timeout, no keep-alives, zero redirects, `http`/`https` only, and a size-capped response read. Operators can opt into an off-by-default private-address block (`WEBHOOK_BLOCK_PRIVATE_ADDRESSES`) that resolves the host and refuses private/loopback/link-local targets — including RFC 6598 CGNAT (`100.64.0.0/10`) and the RFC 6052 NAT64 prefix (`64:ff9b::/96`) — and dials the validated IP to defeat DNS rebinding. Configurable from Settings or the `ovumcy webhook <show|set>` operator command. (#124)
- **Built-in daily reminder scheduler (opt-in, off by default).** An optional in-process pass (`REMINDER_SCHEDULER_ENABLED`, default `false`; `REMINDER_SCHEDULER_HOUR`, default `9`, in the server's `TZ`) evaluates upcoming period/ovulation reminders once a day. When disabled, no scheduler goroutine, timer, or outbound component exists in the running process at all. It is backed by a minimal app-level key/value store (migration `028`) that holds process runtime markers only — never per-owner health data. A per-owner "reminder lead days" control (0–14) and an in-app dashboard reminder banner surface the same window in the UI. (#123, #125)
- **Read-only calendar `.ics` feed (opt-in).** Each owner can generate an opaque, tokenized `.ics` subscription URL for external calendar clients. The feed token is stored hashed (migration `029`), has an explicit lifecycle in Settings, is one-click-revocable, and is force-cleared when the recovery code is regenerated. `VEVENT` `UID`s are a stable function of `(kind, date)`, and every feed carries the medical-safety disclaimer. (#181, #176, #184)
- **Per-user timezone.** An account's timezone is persisted from the request (migration `026`) and used for DST-safe cycle-day counting, predictions, reminders, and the `.ics` feed. (#159)
- **Per-user week-start preference.** The calendar grid and weekday header can start on Sunday or Monday (migration `030`, values `sunday`/`monday`). (#225)
- **Italian (it) localization.** Italian is added to the supported languages and the required-locale boot guard, with server-rendered dates localized. (#218)
- **`OIDC_RESPONSE_MODE` setting.** Selects the OIDC authorization response mode; defaults to `form_post`, with `query` available as an explicit opt-in (the query-mode Authorization Code is inert without the PKCE verifier held in the sealed state cookie). (#210)

### Changed

- **Removed the local pre-push patch-coverage hook.** `scripts/hooks/pre-push`, `scripts/setup-hooks.sh`, and `scripts/patch-coverage-local.sh` are removed — the hook reran the full test suite on every push with a `*.go` change, which made `git push` slow. Patch coverage is still enforced, exclusively by CI's `patch-coverage` job. `scripts/patchcov` (the gate CI calls directly) is unchanged.

- **Breaking (export shape):** the built-in `swelling` symptom (added 2026-03-09) now has its own `swelling` boolean flag in the JSON `symptoms` object and its own `Swelling` CSV column, instead of falling through to `other_symptoms`/`Other` indistinguishably from an owner-created custom symptom. The `Swelling` CSV column is inserted after `Constipation` to stay adjacent to the other symptom columns, so every column from `Cycle factors` onward shifts one position to the right in files generated after this change; consumers reading CSV columns by position (not by header name) must account for the shift. `docs/export.md` and `docs/openapi.yaml` updated; import accepts both the new flag and legacy files that still carry `swelling` only via `other_symptoms`.

### Fixed

- **SQLite writes now open with `BEGIN IMMEDIATE`**, ending intermittent `SQLITE_BUSY` 500s when two writes raced for the write lock. (#155)
- **Next-period projection uses the median (not mean) cycle length consistently** across the dashboard, webhook reminders, and the `.ics` feed, and calendar-day differences use a DST-safe `CalendarDaysBetween`. (#213, #212)
- Calendar cycle-start confirmation modal now opens in the summary view. (#238)
- `show_historical_phases` is reset when an owner clears their data. (#229)
- Day-note trimming no longer splits multi-byte UTF-8 runes. (#222)
- The request-free webhook `notify` pass sets the pass user's `Role` so the cycle baseline applies, and parses `WEBHOOK_BLOCK_PRIVATE_ADDRESSES` consistently on that path. (#214, #215)
- Accessibility: the medical disclaimer renders on the stats and calendar pages; mobile touch targets, tab-bar height, and heading order were tightened; and the language pills stay on a single line down to 390px.
- Postgres example stacks (`docs/examples/postgres`, `caddy-postgres`, `nginx-postgres`) mounted the data volume at `/var/lib/postgresql/data`, which the `postgres:18` image no longer uses as its data directory — database contents ended up in an anonymous volume and were lost when the container was recreated. The examples now mount `postgres_data:/var/lib/postgresql`. (#200)

**Migration for existing deployments:** if you deployed from one of these examples, dump your database while the old container is still running, before pulling this change: `docker compose exec postgres pg_dumpall -U $POSTGRES_USER > backup.sql`. Then apply the updated compose file, recreate the stack (`docker compose down && docker compose up -d`), and restore: `docker compose exec -T postgres psql -U $POSTGRES_USER -d postgres < backup.sql`. If the container was already recreated and the database looks empty, your data may still be in an orphaned anonymous volume — list candidates with `docker volume ls` and inspect them before deleting anything.

### Security

- **Go toolchain bumped to 1.26.5.** Clears GO-2026-5856 (Encrypted Client Hello privacy leak in `crypto/tls`, fixed upstream in go1.26.5), reachable via the outbound HTTP client used for webhook delivery and OIDC. The runtime image's builder stage moves to `golang:1.26.5-alpine3.24` (the `alpine3.22` variant of this patch release was not published); the final `alpine:3.24.1` runtime-assets stage is unchanged.
- **Daily-log writes are scoped to `user_id`**, tightening cross-owner isolation on the write path so a write can only land on the session owner's rows. (#239)
- **Symptom writes are scoped to `user_id`.** `SymptomRepository.Update` moved off a primary-key-only save to the same `user_id`-scoped update path as daily logs, so a mutated `UserID` can never reassign or overwrite another owner's symptom row (defense-in-depth; callers already load via `FindByIDForUser`). (#244)
- **Account erasure now clears `oidc_logout_states`.** These rows previously carried no `user_id`, so account deletion left them to age out via their ~7-day TTL — a right-to-erasure gap. A nullable `user_id` column (migration `031`) is populated on the OIDC login-callback and session-rotation paths and deleted inside the account-erasure transaction; rows minted before `031` keep a `NULL` `user_id` and still age out via TTL. (#244)
- **bcrypt work factor raised to cost 12**, with stale lower-cost hashes transparently rehashed on the next successful login; the timing-equalization placeholder hashes are aligned to the same cost so login timing does not distinguish existing from missing accounts.

### Internal

- **`go-118-fuzz-build` pinned instead of floating on `@latest`** in `.clusterfuzzlite/build.sh` (OpenSSF Scorecard "Pinned-Dependencies"). The module has no tagged releases, so it's pinned to a pseudo-version (`v0.0.0-20250520111509-a70c2aa677fa`) instead — same effect as a commit-SHA pin. `.clusterfuzzlite/Dockerfile`'s unpinned `base-builder-go` base image is unchanged (documented exception, now cross-referenced correctly in `docs/SECURITY_INVARIANTS.md → CI`).

- **Continuous fuzzing via ClusterFuzzLite** on GitHub Actions, a `gitleaks` secret-scanning gate with a fixture allowlist, and a `golangci-lint` v2 configuration with curated linters were added; the unreachable GO-2026-5932 (`x/crypto/openpgp`) is suppressed via `osv-scanner.toml`.

- **CI wall-clock reduced without changing coverage.** The `test` job splits into parallel `test-go` (staticcheck, golangci-lint, vet, `go test` + coverage) and `test-frontend` (lint, unit tests, build, stale-bundle guard) jobs — they never depended on each other's output, so running them in parallel instead of as sequential steps in one job cuts wall-clock roughly in half. A thin `test` job (`needs: [test-go, test-frontend]`) keeps the branch-protection required-check name intact. `e2e`, `e2e-postgres-smoke`, and `e2e-cross-browser` now cache Playwright's downloaded browser binaries (`~/.cache/ms-playwright`, keyed by `package-lock.json`) across runs; on a cache hit only the (fast, apt-based) OS dependencies are reinstalled, skipping the multi-hundred-MB browser download. `golangci-lint`'s own analysis cache (`~/.cache/golangci-lint`, previously uncached) is now restored across runs too, as is `npm`'s package cache in `e2e`/`e2e-postgres-smoke`/`e2e-cross-browser` (`test-frontend` already had it; the three e2e jobs' own `npm ci` calls didn't). `ci.yml`/`security.yml` gained a `concurrency` group so a rapid string of fixup pushes on the same PR cancels superseded runs instead of queuing every one to completion (push to `main`/`release`/`workflow_dispatch` runs are never cancelled). Every check still runs the same commands against the same code — nothing skipped, nothing narrowed.

- Redundant dashboard `daily_logs`/`symptom_types` reads were removed on the dashboard render path, and the cycle-prediction multi-bool tuples were replaced with named structs.

### Dependencies

- The Go `go-minor-patch` group, the GitHub Actions group, the `compose-images` group, `eslint`/npm dev-dependency minor-patch bumps, and a Tailwind CSS 4.3.2 bundle rebuild.

## [1.7.0] - 2026-07-03

### Security

- **Web framework upgraded to Fiber v3.** `gofiber/fiber` moves from v2.52.13 to v3.4.0, removing GHSA-gcfq-8gqf-4876 / CVE-2026-45045 (X-Real-IP spoofing in fiber's proxy middleware) from the dependency graph entirely — the vulnerable module was never compiled into Ovumcy, and `govulncheck` now reports no known vulnerabilities. Routes, JSON shapes, cookie attributes, security headers, and rate limiting are preserved; the CSRF token lifetime is pinned explicitly to the previous 1-hour value (Fiber v3's new default would otherwise shorten it to 30 minutes).
- **Upgrade note (HTTPS reverse proxies):** Fiber v3's CSRF layer validates the browser `Origin` against the app-observed scheme/host on every mutating request. The documented deployment posture already satisfies this — the bundled Caddy/nginx examples send `X-Forwarded-Proto` and set `TRUST_PROXY_ENABLED=true`. An HTTPS-terminating proxy in front of an instance still running `TRUST_PROXY_ENABLED=false` (previously only a rate-limit-keying warning) will now see browser form submissions rejected with 403 until trust is enabled.

## [1.6.0] - 2026-07-03

### Added

- **Single-binary packaging.** HTML templates, locales, and `web/static` are embedded into the binary via `go:embed`; the runtime image ships only the binary (`FROM scratch`) and the app runs from any working directory. Static asset URLs are cache-busted with `?v=<build revision>` and served with `Cache-Control: public, max-age=3600`, so a release invalidates stale JS/CSS without operator action.

### Changed

- **`HSTS_ENABLED` switch.** `Strict-Transport-Security` is now governed by an explicit `HSTS_ENABLED` environment variable (default inherits `COOKIE_SECURE`, exact prior behavior). Operators can keep secure cookies without pinning browsers to HTTPS for a year, or opt in explicitly; enabling it logs a startup note about the one-year pin.
- **BBT stored as nullable.** `daily_logs.bbt` drops `NOT NULL DEFAULT 0` (migration 024, SQLite and Postgres; existing `0` rows are migrated to `NULL`). `nil` now means "not measured", retiring the 0°C sentinel.
- **Breaking (export shape):** `bbt` (basal body temperature) is now omitted from JSON export entries on days without a measurement, instead of always being present as `0`. Consumers parsing the export must treat a missing `bbt` key as "not measured". Restore stays fully backward-compatible — import reads an absent key, an explicit `null`, and the legacy `0` all as "not measured" (`docs/export.md` and `docs/openapi.yaml` updated).

### Fixed

- **Light-theme accent contrast raised to WCAG AA.** The two colours behind the only axe colour-contrast violation (2.96:1 against the hero gradient) now clear 4.5:1; hue preserved, dark theme untouched.

### Removed

- **Breaking:** removed the query-string form of the day-delete endpoint (`DELETE /api/v1/days?date=YYYY-MM-DD`). Use `DELETE /api/v1/days/{date}` instead (the optional `source` selector, if used, moves from the `date`-bearing query string to a plain `?source=` query param on the path form). `docs/openapi.yaml` and the browser UI have been updated accordingly.
- **Redundant `daily_logs` date index dropped** (migration 025, both dialects). Every query path is user-scoped and already served by the `(user_id, date)` index; the bare-date index only added write amplification.

### Security

- **OIDC HTTP redirects are origin-pinned.** The HTTP client used for OIDC discovery, the JWKS key fetch, and the authorization-code exchange now refuses to follow any redirect that leaves the configured issuer origin, extending the existing `jwks_uri` / `token_endpoint` / `end_session_endpoint` origin pins to the HTTP requests themselves. A provider that redirects these requests cross-origin now fails closed at sign-in; same-origin redirects (path normalization) keep working.
- **Request bodies are bounded (16 MiB).** An explicit `BodyLimit` sized to the documented JSON-restore maximum; the transport-level 413 now returns the mapped, localized `request_too_large` envelope instead of fasthttp's bare text.

### Internal

- Lifted the transitive SQLite (`modernc.org/sqlite`) and `fasthttp` engines to current upstream.
- Split `base.html` into a components library under `internal/templates/components/` (byte-identical defines; `base.html` keeps only the page skeleton).
- Retired the sentinel→enum error-classification layer across all domains; error mapping now switches directly on service sentinels.

## [1.5.0] - 2026-07-01

### Added

- **Restore from JSON backup.** `Settings → Restore from backup` (and `POST /api/v1/imports/json`) re-imports a prior Ovumcy JSON export into the current account. The restore is additive — only days the account does not already have are created; existing days are never overwritten or deleted, so no password re-authentication is required. Every field of the (untrusted) file is re-validated and scoped to the session owner; malformed or duplicate records are skipped and reported in the result counts. Importing from other trackers (e.g. Drip) remains out of scope — see issue #116.

## [1.4.0] - 2026-07-01

Adds declarative owner provisioning from the operator CLI and clarifies the
product as owner-role-only with per-account isolation, enabling household
self-hosting. No database migrations; no breaking API changes.

### Added

- **`ovumcy users create <email>` operator CLI command.** Provisions an owner account declaratively — for example from a YunoHost install script — without the open-register-then-close workaround. The password is read from stdin (for automation) or an interactive no-echo prompt, never from argv or the environment; `--show-recovery-code` prints the one-time recovery code on demand; `--skip-if-exists` makes re-runs idempotent.
- **Household self-hosting.** A single instance may host several independent owner accounts, each the sole owner of its own data and isolated from the others. The privacy model is documented as owner-role-only in `SECURITY.md` and `docs/SECURITY_INVARIANTS.md`, with cross-owner isolation pinned by regression tests.

### Internal

- Removed the never-shipped non-owner "viewer" sanitization path; the day-read service now returns owner data directly. The role-integrity guard (`ValidateSupportedWebUser`) is retained.

## [1.3.0] - 2026-06-13

A large security-and-quality release: a multi-phase security audit follow-up
across auth, sessions, OIDC, privacy, and accessibility; medically-aligned
cycle prediction; a full ru/es/de/fr localization pass; major frontend
dependency migrations (htmx 2, Tailwind 4); and substantial test, CI, and
build-hardening work. No database migrations; no breaking API changes.

### Security

- **Storage and proxy hardening.** SQLite now boots with foreign-key enforcement and WAL mode; the per-request rate-limit keying behind a trusted proxy was corrected so limits key on the real client, not the proxy hop.
- **Auth, sessions, and OIDC hardening.** Reworked per-IP / per-identity rate-limit key generation; origin-pinned the OIDC discovery and token endpoints against SSRF; consolidated the AEAD sealed-cookie path; threaded request-scoped `context.Context` through the data layer; and folded the reset-token compare-and-swap into `AuthUserRepository`, dropping its dead fallback. Behavior-preserving where it counts, defense-in-depth elsewhere.
- **Robustness & auth hardening (audit phase 3).** Additional hardening of auth and recovery flows, with the security-claim test matrix kept in step.
- **Contract & privacy fixes (audit phase 2).** Auth/settings validation errors stay in flash/session state instead of URL query parameters; registration and recovery wording was made enumeration-safe.
- **CSRF tokens are kept out of GET request URLs.** Token transport moved off the query string so it cannot leak via browser history, logs, or `Referer`.
- **Security-policy & docs corrections** carried through the audit follow-ups (claim matrix, documented headers/rate-limits) to keep `SECURITY.md` true to the code.

### Changed

- **Cycle predictions now use the median cycle length, not the mean**, and the current cycle day is counted on a DST-immune calendar basis. Medical docs and on-screen labeling were aligned to match.
- **Full localization pass across ru/es/de/fr.** Count-bearing stats strings use CLDR plural categories; Russian copy uses the consistent formal «Вы» register; terminology unified (e.g. ru «панель»/«Аналитика»/«Базовая линия»/«БТТ», es/de/fr canon). Walkthrough findings additionally fixed an i18n gap, a settings toggle bug, a dashboard label, and a short-cycle note.
- **Accessibility hardening (audit phase 4).** Focus management/restoration for the confirm modal, `aria-live` status and toast regions, and a skip-to-content link.
- **Operator docs:** documented `REGISTRATION_MODE=closed` as the recommended default for public deployments.

### Dependencies

- **htmx 1.9.12 → 2.0.10** (major) and **Tailwind CSS 3.4.19 → 4.3.0 → 4.3.1** (major) frontend migrations, with the committed `web/static` bundle rebuilt against the new toolchains.
- Go: `golang.org/x/net` 0.55 → 0.56 and the `go-minor-patch` group (6 updates).
- Tooling/base: Alpine 3.22.3 → 3.24.0, the GitHub Actions group (14 updates), `eslint` 10.4.1 → 10.5.0, `globals` 15.15.0 → 17.6.0, and other npm dev-dep minor/patch bumps.

### Internal

- **Hermetic asset builds + CI stale-bundle guard** — committed `web/static` must match a fresh `npm run build`, enforced in CI (audit #2).
- **Refactors:** shared dependency wiring extracted into `internal/bootstrap`; the secure-cookie codec deduplicated via a lazy `Handler.cookieCodec()`; maintainability-debt cleanup (audit phase 6); internal document-collision elimination.
- **Test quality:** a coverage quality pass (dead-symbol removal, vacuous-test fixes, meaningful gap-filling, browser a11y), structural `data-*` test rewrites + TOTP codec coverage, and a round-3 mutation-hardening pass adding per-mutant-verified tests for service-layer survivors introduced by the audit work.
- **CI:** patch-coverage enforced in-CI via `scripts/patchcov`; browser e2e skipped on docs-only pull requests; OpenSSF Scorecard fix — renamed `.mutation/security.md` so it no longer shadows `SECURITY.md` in the basename-matched Security-Policy check.

## [1.2.0] - 2026-06-09

### Added

- **Pregnancy test day field.** An always-shown day field (`none` / `negative` / `positive`) in the dashboard and calendar day editors. A positive test with no later cycle start pauses cycle predictions until a new period is logged. The field is part of the `/api/v1/days` payload (`docs/openapi.yaml`) and the owner CSV and JSON exports (`docs/export.md`); the CSV column is appended at the end so existing column positions stay stable.

### Security

- **All `/api/v1` read endpoints are now owner-gated.** `GET /users/current`, `/days`, `/days/:date`, and `/stats/overview` chain `handler.OwnerOnly` after `AuthRequired`, matching every mutation. Behavior-neutral for the single-role (owner) product — `AuthRequired` already rejects any non-owner role — closing a defense-in-depth uniformity gap.
- **Security documentation corrected to match the code.** Recovery codes are 12 base32-style characters (~60 bits of entropy), not "12 hex / 48 bits"; the documented CSP now includes `manifest-src 'self'`; the `/auth/oidc` rate-limit row and the companion security headers (COOP, X-Frame-Options, nosniff, Referrer-Policy, Permissions-Policy, HSTS) are documented; the web product is clarified as single-role (owner).

### Internal

- Raised `internal/security` OIDC config-validation and `internal/db` repository test coverage (token/state TTL, daily-log read whitelist, symptom owner-scoping).

## [1.1.1] - 2026-06-07

### Security

- **Recovery-code normalization is now rune-safe.** `NormalizeRecoveryCode` used a byte-length check and byte slicing, which produced unstable, non-idempotent output for non-ASCII / invalid-UTF-8 input. Reformatting is now gated on a strict 12-character ASCII-alphanumeric body and is fuzz-guarded. No exploitable bypass existed — downstream validation rejected non-canonical input — but the path is now structurally sound.
- **Go toolchain and dependencies bumped to clear advisories.** Go 1.25.10 → 1.26.4 and `golang.org/x/crypto`, `x/net`, `x/sys`, `x/text` to current releases; `govulncheck` confirms zero reachable vulnerabilities.
- **Release supply-chain verification is documented.** Published images are keyless-signed (cosign), carry SLSA build provenance and an SBOM attestation, and `SECURITY.md` now explains how to verify them with `cosign verify` and `gh attestation verify`.
- Hardened defaults, atomic day writes, UTC cycle math, and flash-message PII removal landed in the unreleased window.

### Changed

- The README now leads with the product story (demo and screenshots above the fold) and a plain-language "How Predictions Work" section; deployment details moved below.
- The cycle-prediction algorithm is fully documented in `docs/cycle-prediction.md`, with worked examples pinned 1:1 by reference tests.
- Test files are excluded from the runtime image build context, `govulncheck` runs as a call-graph reachability gate, and Docker Hub pulls are authenticated to avoid anonymous rate-limit flakiness.

### Internal

- Added mutation testing (gremlins), native fuzzing, property-based and reference-vector tests, cycle-math benchmarks, and a SQLite backup/restore integrity test. Mutation efficacy on `internal/services` is ~94%; see `TESTING.md`.

## [1.1.0] - 2026-05-23

### Security

- **TOTP step-up on `/auth/oidc/link-confirm`.** When the target local-auth account has TOTP enabled, the link-confirmation submission must additionally carry a valid 6-digit `totp_code` form field. The handler runs `TOTPService.CheckRateLimit` and `ValidateCode` with the same per-`(client_ip, user_id)` failure counter and replay rejection (`ErrTOTPReplayed`) as `POST /api/v1/sessions/2fa-challenge`. Previously the handler called `AuthService.AuthenticateCredentials` (password only) and went straight to `setAuthCookie`, allowing an attacker with the victim's password plus a malicious or sloppy upstream IdP to obtain a session and persist a linked OIDC identity without ever holding the second factor.
- **AEAD codec coverage** for `ovumcy_oidc_link_pending` adds the canonical four-invariant lock (round-trip, cross-purpose AAD, tampered byte, rotated key) plus payload expiry and builder field validation. The cookie landed in v1.0.0 without dedicated codec regressions.
- **CSRF route-level locks** for `POST /auth/oidc/link-confirm` and `DELETE /api/v1/days/:date` with the real CSRF middleware enabled, closing the SECURITY.md route-coverage requirement for both endpoints.
- **Cross-user privacy regressions**: Owner B's `GET /api/v1/symptoms` must not surface Owner A's custom symptoms; Owner B's `DELETE /api/v1/days/:date` must not remove Owner A's row on the same calendar day.
- **Sensitive-field leak guard** on `GET /api/v1/users/current` (explicit deny-list assertion on `password_hash`, `recovery_code_hash`, `totp_secret`).

### Fixed

- **`days`** (issue #64): `UpsertDayEntry` was re-applying `DayRange` to a value already canonicalized to UTC-midnight by the caller. For UTC-minus locales `DateAtLocation` shifted the lookup window one day backward, so a second `PUT /api/v1/days/{date}` for the same calendar day missed the existing row and the follow-up `Create` collided with the `uidx_user_date` unique index. Replaced with direct `[dayStart, dayStart+24h)` bounds plus a defensive UTC-midnight normalization at the function entry.

### Changed

- **Link-confirm template** conditionally surfaces a TOTP input when `TOTPRequired=true` (computed in `ShowOIDCLinkConfirmPage` from the target user's `TOTPEnabled`), reusing the `auth.2fa.code_label` / `auth.2fa.code_placeholder` i18n keys.
- **`SECURITY.md`**: new `### OIDC Account Linking` section in the Test Enforcement Matrix links every claim about the link-confirm flow to the specific Go test. Threat-model bullet on malicious-IdP account takeover extended to mention the TOTP gate.
- **`DeleteDay` handler** gains an idempotency lock: `DELETE /api/v1/days/{date}` on a day that has no row returns 204 explicitly.
- **Rate-limit responder coverage** now exercises both the JSON envelope and the HTML fallback path on `RespondAPIRateLimited`.

## [1.0.0] - 2026-05-19

### Changed

- **BREAKING**: The entire HTTP surface moves under `/api/v1/*` and ships as the stable third-party contract. The legacy `/api/*` (non-v1) and the page-route mutators at `/settings/cycle` and `/onboarding/*` are removed.

  Full mapping (canonical REST verbs):

  | Legacy | Canonical `/api/v1/*` |
  | --- | --- |
  | `POST /api/auth/register` | `POST /api/v1/users` |
  | `POST /api/auth/login` | `POST /api/v1/sessions` |
  | `POST /api/auth/logout` | `DELETE /api/v1/sessions/current` |
  | `POST /api/auth/2fa` | `POST /api/v1/sessions/2fa-challenge` |
  | `POST /api/auth/forgot-password` | `POST /api/v1/password-resets` |
  | `POST /api/auth/reset-password` | `POST /api/v1/password-resets/redeem` |
  | `GET /api/days` | `GET /api/v1/days` |
  | `GET /api/days/{date}` | `GET /api/v1/days/{date}` |
  | `GET /api/days/{date}/exists` | `HEAD /api/v1/days/{date}` |
  | `POST /api/days/{date}` (upsert) | `PUT /api/v1/days/{date}` |
  | `DELETE /api/days/{date}` | `DELETE /api/v1/days/{date}` |
  | `POST /api/days/{date}/cycle-start` | `POST /api/v1/days/{date}/cycle-start` |
  | `DELETE /api/log/delete?date=` | `DELETE /api/v1/days?date=` |
  | `GET /api/symptoms` | `GET /api/v1/symptoms` |
  | `POST /api/symptoms` | `POST /api/v1/symptoms` |
  | `POST /api/symptoms/{id}` (update) | `PATCH /api/v1/symptoms/{id}` |
  | `POST /api/symptoms/{id}/archive` & `DELETE /api/symptoms/{id}` | `DELETE /api/v1/symptoms/{id}` (single canonical path) |
  | `POST /api/symptoms/{id}/restore` | `POST /api/v1/symptoms/{id}/restore` |
  | `GET /api/stats/overview` | `GET /api/v1/stats/overview` |
  | `POST /api/export/{summary,csv,json}` | `GET /api/v1/exports/{summary,csv,json}?from=&to=` |
  | `POST /api/settings/profile` | `PATCH /api/v1/users/current/profile` |
  | `POST /api/settings/interface` | `PATCH /api/v1/users/current/interface` |
  | `POST /api/settings/tracking` | `PATCH /api/v1/users/current/tracking` |
  | `POST /settings/cycle` (page route) | `PATCH /api/v1/users/current/cycle` |
  | `POST /api/settings/change-password` | `PUT /api/v1/users/current/password` |
  | `POST /api/settings/start-local-password-setup` | `POST /api/v1/users/current/password/step-up` |
  | `POST /api/settings/regenerate-recovery-code` | `POST /api/v1/users/current/recovery-code` |
  | `POST /api/settings/2fa/verify` | `PUT /api/v1/users/current/2fa` |
  | `POST /api/settings/2fa/disable` | `DELETE /api/v1/users/current/2fa` |
  | `POST /api/settings/clear-data/validate` | `POST /api/v1/users/current/data-wipe/validate` |
  | `POST /api/settings/clear-data` | `POST /api/v1/users/current/data-wipe` |
  | `DELETE /api/settings/delete-account` | `DELETE /api/v1/users/current` |
  | `POST /onboarding/step1` (page route) | `POST /api/v1/onboarding/steps/1` |
  | `POST /onboarding/step2` (page route) | `POST /api/v1/onboarding/steps/2` |
  | `POST /onboarding/complete` (page route) | `POST /api/v1/onboarding/complete` |

### Added

- `GET /api/v1/users/current` ("whoami"): returns the minimum representation of the session subject — id, email, display_name, role, and lifecycle flags (`onboarding_completed`, `local_auth_enabled`, `must_change_password`). Sensitive fields (password/recovery hashes, TOTP secret) are never included.
- Browser regression for the privacy page (`e2e/privacy.spec.ts`) that asserts the rendered copy of every privacy section (zero-collection, your-data, hidden-sections, third-parties, open-source) plus the authenticated breadcrumb/back link contract. The matching backend regression in `internal/api/privacy_route_regressions_test.go` is reduced to structural smoke (section presence and back-link `href` only) so copy edits no longer trigger backend churn.
- GDPR consent control on the public registration form (`/register`). The browser checkbox is wired to a new `consent` field on the registration payload; the backend refuses any registration where `consent` is not truthy and surfaces `auth.error.consent_required` through the same flash/JSON channel used by other auth errors. Localized labels and error copy are added across `en`, `ru`, `es`, `fr`, and `de`. Coverage: `internal/api/auth_register_consent_regression_test.go`.
- Three new privacy-page sections — `data-privacy-section="your-rights"`, `"retention"`, `"predictions"` — render the GDPR Art. 13/15-22 disclosures alongside the existing data-protection summary, with full translations across all five UI locales. Coverage: extended `internal/api/privacy_route_regressions_test.go` plus `e2e/privacy.spec.ts`.
- Forward-looking role-boundary coverage matrix `TestUnsupportedRoleRejectedAcrossEveryAuthedV1Route` in `internal/api/owner_only_coverage_regression_test.go`. The test discovers every registered `/api/v1/*` route through `app.GetRoutes()`, filters out the public auth endpoints, and asserts each remaining route rejects an unsupported-role auth cookie with `403` + a cleared `ovumcy_auth` cookie. New state-mutating endpoints inherit this coverage automatically.
- `docs/gdpr.md` operator-facing GDPR compliance guide. Maps each in-scope GDPR obligation (Art. 6, 9, 13, 15-22, 30, 32, 33) onto the technical control plus operator action (encryption at rest via LUKS/BitLocker, backup hygiene, `SECRET_KEY` separation, DSAR fulfilment through export, audit log retention, breach notification runbook). Referenced from `SECURITY.md → GDPR Cross-Reference`.
- `docs/SECURITY_INVARIANTS.md` — a single canonical list of the security-critical invariants the codebase enforces. Contributors can read the layering, role-boundary, AEAD, CSP, GDPR, and CI rules in one place instead of inferring them from the code and the test suite.

### Security

- **Onboarding endpoints now require `handler.OwnerOnly`.** `POST /api/v1/onboarding/steps/1`, `POST /api/v1/onboarding/steps/2`, and `POST /api/v1/onboarding/complete` were previously only guarded by `handler.AuthRequired`, which transitively rejects unsupported roles through `ErrAuthUnsupportedRole`. The explicit `OwnerOnly` middleware closes the defense-in-depth gap so any future regression of `AuthRequired` cannot expose onboarding mutations. Coverage: `TestUnsupportedLegacyRoleOnboardingMutationsAreRejected` in `internal/api/smoke_unsupported_role_flow_test.go` plus the route-discovery matrix above.

### Changed

- Backend HTML regressions for shared explainer/empty-state/warning blocks now assert stable `data-*` hooks instead of exact UI copy. The dashboard, calendar, and stats templates expose `data-explainer-key="<i18n-key>"` (and `data-explainer-primary-key`/`data-explainer-secondary-key` for the multi-line calendar variant) on their prediction-explainer containers, so backend tests verify the policy-selected key against the service-layer contract rather than the localized phrase. Stats empty-state coverage moves to `data-stats-empty-state` + `data-stats-completed-cycles`, dashboard stale-cycle coverage moves to `data-dashboard-cycle-warnings`/`data-dashboard-stale-warning`/`data-dashboard-phase`, and privacy sections expose `data-privacy-section="..."` anchors.
- Flash/error/subtitle regressions on auth and settings pages now assert i18n keys through stable `data-flash-key`/`data-flash-status`/`data-error-key`/`data-subtitle-key` attributes. The settings page renders the success/error banner with `data-flash-key="<key>"` plus a `data-flash-target="change_password"` qualifier for change-password errors; auth pages (login, register, forgot-password, reset-password, 2FA, recovery) carry `data-error-key="<key>"` on every `data-auth-server-error` block; the forgot-password subtitle exposes `data-subtitle-key` plus `data-forgot-step`; the 404 page exposes `data-not-found-title`/`data-title-key`; and the dashboard usage-goal panel exposes `data-usage-goal-label-key`/`data-usage-goal-summary-key`. The shared HTMX status wrapper (`httpx.StatusErrorMarkup`) now accepts an optional error key and emits the same `data-flash-key`/`data-flash-status="error"` attributes so HTMX inline errors share the same contract.
- Frontend npm devDependencies in `package.json` are now exact-pinned (`htmx.org`, `tailwindcss`, `@playwright/test`, `eslint`, `jsdom`, `otplib`, `globals`, `@eslint/js`). Matches the existing `go.mod` exact-pinning baseline and removes the residual supply-chain surface where a `npm install` outside `npm ci` could pull a minor/patch update without a corresponding lockfile bump.

## [0.9.5] - 2026-05-15

### Added
- Runtime proof-of-concept regression tests for the two OIDC contracts hardened in v0.9.5 (`internal/security/oidc_runtime_poc_test.go`). The suite stands up a controlled OIDC provider via `httptest.NewUnstartedServer` with a real TLS leaf signed by a per-test CA, configures `security.OIDCClient` against that issuer through the production `OIDC_CA_FILE` path, and exercises four contracts end-to-end without booting Ovumcy: (1) a malicious `end_session_endpoint` on a different host is stripped from the metadata so logout falls back to local-only, (2) a same-origin `end_session_endpoint` flows through, (3) an ID token signed with HS256 using the JWKS RSA public key as the HMAC secret is refused by the verifier (algorithm-confusion downgrade), and (4) an unsigned `alg=none` token is refused.
- Frontend JavaScript unit-test suite under `web/src/js/__tests__/`, executed via `npm run test:unit` (Node's built-in test runner + jsdom). Twenty-seven tests cover the four security-sensitive client-side surfaces previously only exercised indirectly through Playwright e2e: CSRF token injection on the `htmx:configRequest` hook, the safe-by-construction DOM swap on `htmx:responseError` (the Sprint 3 #9 contract), the `isSafeClientTimezone` validator backing the `ovumcy_tz` cookie write, and the `navigator.clipboard` → `document.execCommand("copy")` fallback used by the recovery-code copy UI. The suite is wired into the CI workflow alongside `lint:js`.
- Subprocess smoke test for the operator CLI (`go test ./cmd/ovumcy -run TestCLISubprocessSmoke`). The previous CLI test surface exercised the dispatch helpers in-process; the new test builds the real `ovumcy` binary into a temp directory and runs `users list`, an intentional usage-error invocation, and a placeholder-secret invocation as subprocesses, so argv parsing, env-var pickup, and exit codes are no longer invisible to the suite. The test is skipped under `go test -short` so the day-to-day suite stays fast and is run by CI without `-short`.

### Security
- `POST /api/settings/clear-data` now bumps `users.auth_session_version` atomically with the data wipe. Any auth cookie that existed before the clear is invalidated on its next request, and the originating device is refreshed inline so the user that triggered the wipe stays signed in. Closes a defense-in-depth gap where a "panic clear" gesture left other sessions authenticated to the freshly-empty account.
- HTMX error responses are no longer assigned through `innerHTML` on the client. The status-error fragment returned by the server is parsed with `DOMParser` and re-built with `document.createElement` + `textContent` before being inserted via `replaceChildren`. Server-rendered error templates already escape user-supplied values, so today this is purely defense-in-depth; any future regression that lets unescaped HTML into an error response would otherwise become an instant DOM-XSS sink.
- Encrypted TOTP secrets are now bound to the owner's user id via AES-GCM additional-authenticated-data (`ovumcy.field.totp_secret:<userID>`). A database-level swap of one user's encrypted secret into another user's row no longer opens under the second user's id, so an attacker with database write privilege cannot pass 2FA for another account by lifting the ciphertext. `DecryptField` keeps a legacy fallback for pre-aad ciphertexts and lazily re-encrypts them under the new aad-bound format on the next successful 2FA login, without bumping `auth_session_version` (the user did not just change their security posture).
- Register-pickup cookie is now single-use server-side. `POST /api/auth/register` persists an opaque nonce in a new `register_pickup_tokens` table, and `GET /register/welcome` atomically consumes it in the same UPDATE. A captured sealed `ovumcy_register_pickup` cookie can no longer be replayed within the 5-minute TTL to mint a second auth session — the second consume returns "already used" and falls through to the same neutral `/login` redirect as a decoy or expired pickup. Migration `022_register_pickup_tokens.sql`.
- 2FA enable and disable now bump `users.auth_session_version` atomically with the TOTP-field update. Every other auth cookie issued before the toggle is invalidated on its next request; the originating session is refreshed inline so the user that performed the toggle stays signed in on their current device. Matches the existing contract for password change, password reset, and recovery-code regeneration.
- OIDC `end_session_endpoint` is now host-pinned to the configured issuer. Discovery metadata that advertises an endpoint on a different host is rejected at provider load time, falling back to local-only logout. Closes a defense-in-depth gap where a compromised or look-alike discovery document could redirect the logout flow (including any `id_token_hint` carried in the URL) to an attacker-controlled host.
- OIDC ID-token verifier now carries an explicit `SupportedSigningAlgs` allowlist (`RS256`, `RS384`, `RS512`, `ES256`, `ES384`, `ES512`, `PS256`, `PS384`, `PS512`, `EdDSA`). Symmetric algorithms and `none` cannot be negotiated even if the upstream provider advertises them, closing an algorithm-confusion downgrade lane.

## [0.9.4] - 2026-05-13

### Added
- TOTP-based two-factor authentication. Owners can enable TOTP 2FA in Settings → Security. Login prompts for the 6-digit TOTP code when 2FA is active. A step-up re-authentication challenge is issued when an OIDC session requires verification.

### Security
- Sealed register pickup cookie closes the per-request Set-Cookie enumeration oracle on `POST /api/auth/register`. The endpoint returns an identical status, body, and single sealed `ovumcy_register_pickup` cookie for both new and duplicate emails; `GET /register/welcome` silently issues a decoy pickup for duplicate addresses and redirects to `/login` with a neutral flash message. The residual two-step timing oracle is documented in `SECURITY.md`.
- TOTP replay protection: step counter validated to reject codes already used within the same 30-second window.
- Login timing side-channel: constant-time bcrypt invocation now applies uniformly to OIDC-only accounts and missing-user paths, preventing user-enumeration via response-time differences.
- OIDC step-up re-authentication: expired OIDC sessions trigger a re-authentication challenge instead of relying solely on the upstream provider's session state.
- Strengthened `Strict-Transport-Security` header with `includeSubDomains` directive.
- Expanded `Permissions-Policy` header to explicitly deny `accelerometer`, `gyroscope`, `payment`, `usb`, `interest-cohort`, and `ambient-light-sensor`.
- Added `Cross-Origin-Opener-Policy: same-origin` to prevent cross-window opener attacks.
- Rate limiting for `/api/auth/register` (8 requests per 15 minutes by default) closes the register enumeration probe surface.
- Per-account rate limit on `/api/auth/logout` (60 requests per 15 minutes by default) to prevent session-disruption attacks.
- Active sessions are atomically revoked when the owner regenerates a recovery code; the originating request receives a fresh auth cookie so the current device stays signed in while all other devices are signed out.

### Fixed
- `DailyLog.Date` and `User.LastPeriodStart` are now canonicalized to UTC midnight on write. A one-time migration backfill corrects existing rows with non-canonical timestamps; observable calendar behavior is unchanged.
- Docker `HEALTHCHECK` no longer relies on `wget`/`curl`, which are absent from the scratch-based runtime image. The binary now ships an `ovumcy healthcheck` subcommand that performs the `/healthz` probe in-process; the `Dockerfile` and all bundled compose examples invoke it directly. Without this fix the container was reported as `unhealthy`.

### Changed
- Updated `github.com/gofiber/fiber/v2` dependency.

## [0.9.3] - 2026-04-30

### Fixed
- Calendar period highlight and dashboard cycle day no longer shift one calendar day earlier for viewers in UTC-minus timezones (e.g. America/Toronto) when daily logs or `users.last_period_start` were persisted with a UTC-based time.Time. Date-only stored values are now read through a new location-agnostic `services.CalendarDay`/`CalendarDayKey` path that takes calendar components from the stored value as-is, instead of running them through `In(location)` which silently moved a UTC-midnight stamp into the previous day in negative-offset locales. Closes #48.

## [0.9.2] - 2026-04-15

### Changed
- Replaced DOM-provided recovery confirmation redirect paths with trusted continue-target tokens, while keeping short-lived recovery cookies backward-compatible during the transition.
- Fixed the Docker image publish workflow parsing failure so the release image pipeline can run again on `main` and on version tags.
- Official compose files, quick-start examples, and README references now pin `ghcr.io/ovumcy/ovumcy-web:v0.9.2`.

### Security
- This patch release hardens the browser recovery-code confirmation sink that CodeQL continued to flag after `v0.9.1` by ensuring the client follows only fixed same-app routes (`/dashboard`, `/onboarding`, `/settings`) selected from trusted tokens rather than DOM text.

## [0.9.1] - 2026-04-15

### Changed
- Tightened the browser recovery-code confirmation flow so client-side continue redirects now allow only the expected same-origin app routes instead of trusting arbitrary DOM-provided paths.
- Reduced helper/test complexity in password-change, recovery transport, migration bootstrap, cycle-hero, and TLS-certificate coverage without changing runtime behavior.
- Official compose files and quick-start examples now pin `ghcr.io/ovumcy/ovumcy-web:v0.9.1`.

### Security
- This patch release closes the CodeQL-reported recovery confirmation redirect sink by forcing recovery continue navigation onto the small allowlisted app-route set (`/dashboard`, `/onboarding`, `/settings`).

## [0.9.0] - 2026-04-15

### Added
- Owner visibility controls for dashboard and calendar entry forms, letting owners hide advanced tracking sections from new entries without removing historical values from private history or exports.
- A segmented dashboard cycle-overview hero with phase cards and browser regressions that keep the hero aligned with calendar predictions and conservative fallback states.

### Changed
- The supported browser product path is now owner-only; legacy non-owner roles are denied before page or API access.
- Recovery-code confirmation, settings, and prediction surfaces were polished to keep clean redirects, localized inline validation, and dashboard/calendar prediction consistency.
- Official compose files and quick-start examples now pin `ghcr.io/ovumcy/ovumcy-web:v0.9.0`, and the README links the public project site at `https://ovumcy.com`.

### Security
- Tracking, export, auth, and recovery flows keep sensitive state out of user-visible URLs while tightening owner-only visibility boundaries.
- The shipped runtime image remains shell-free and package-manager-free, and CI security automation now isolates Codecov OIDC into a least-privilege follow-up job while scanning CI-executed npm dependencies with Trivy.

## [0.8.5] - 2026-03-29

### Changed
- Reduced OIDC-related code complexity without changing runtime behavior by splitting `OIDCConfig.Validate` and `OIDCLoginService.Authenticate` into focused helpers and compacting the OIDC config runtime tests into table-driven coverage.

### Security
- This patch release keeps the hardened OIDC/login/logout contract unchanged while making the security-sensitive validation and linking paths easier to review and maintain.

## [0.8.4] - 2026-03-29

### Changed
- Reissued the patch release on the correct release-packaging commit after the `v0.8.3` tag was created from the previous `main` commit. The runtime feature set is unchanged from the fully green `main` branch.

### Security
- `v0.8.4` is the public patch tag that combines the final CodeQL-driven OIDC helper hardening with the matching release notes and pinned deployment references on the correct tagged commit.

## [0.8.3] - 2026-03-29

### Changed
- Reissued the patch release on the correct release-packaging commit after the `v0.8.2` tag was created from the previous `main` commit. The runtime feature set is unchanged from the fully green `main` branch.

### Security
- `v0.8.3` is the public patch tag that combines the final CodeQL-driven OIDC helper hardening with the matching release notes and pinned deployment references.

## [0.8.2] - 2026-03-29

### Changed
- Removed the last reflected callback-markup pattern from the local OIDC browser-test runtime helper by switching the `form_post` bridge to a constant HTML shell plus a one-time JSON payload endpoint.

### Security
- This patch release supersedes `v0.8.1` for public rollout: it keeps the same OIDC feature set and release packaging, while adding the final CodeQL-driven hardening needed to clear the remaining reflected-XSS alert in the local OIDC test harness.

## [0.8.1] - 2026-03-29

### Changed
- Hardened the local OIDC browser-test runtime helper so it no longer reflects unvalidated transport values, echoes internal error messages in JSON, or accepts arbitrary post-logout redirects.

### Security
- This patch release keeps the `v0.8.0` OIDC feature set but removes the remaining CodeQL warnings from the local OIDC test harness before the public release tag.

## [0.8.0] - 2026-03-29

### Added
- Optional OpenID Connect sign-in for self-hosted deployments, including `hybrid` and `oidc_only` login modes, first login by verified email, stored `(issuer, subject)` links, and operator-facing OIDC documentation.
- OIDC auto-provision for owner accounts when registration is open and the configured allowlist permits the provider email domain.
- Provider logout support through a same-origin bridge together with local-password enablement for OIDC-only accounts.

### Changed
- OIDC provider logout state now stays server-side and is keyed by the auth-session `sid`, which prevents oversized auth headers and keeps raw provider logout parameters out of long-lived cookies.
- Auth and recovery browser coverage now uses cross-browser-portable assertions and validates the full OIDC browser matrix on Chromium, Firefox, and WebKit.

### Security
- Ovumcy keeps the hardened HTML OIDC model: auth/provider-sensitive callback data does not appear in user-visible URLs, and unsupported providers that require query-string callbacks remain excluded from the documented support matrix.

## [0.7.2] - 2026-03-24

### Added
- README now documents how `ovumcy-web`, `ovumcy-app`, and `ovumcy-sync-community` fit together as one product family.

### Changed
- `SECRET_KEY_FILE` now preserves operator-managed path semantics, so absolute secret file paths keep working after the runtime hardening change and startup errors still show the original unreadable path.
- README restores the Go Report Card badge for `github.com/ovumcy/ovumcy-web`.
- Official compose files and quick-start examples now pin `ghcr.io/ovumcy/ovumcy-web:v0.7.2`.

### Security
- No auth/session, privacy-boundary, export-data, or role-access contract was weakened in this release.

## [0.7.1] - 2026-03-22

### Added
- First-party French and German UI localization across server-rendered pages, language switching, and onboarding date accessibility labels.
- Supported `SECRET_KEY_FILE` as a file-backed runtime secret source for self-hosted deployments, together with regression coverage and operator-facing documentation.

### Changed
- Public repository, badge, and documentation links now point to `github.com/ovumcy/ovumcy-web`.
- Official compose files and quick-start examples now pin `ghcr.io/ovumcy/ovumcy-web:v0.7.1`, matching the post-transfer GHCR namespace for tagged releases.
- CI now treats Codecov upload failures on `push` as non-blocking external errors so downstream smoke lanes still run when Codecov ingest is unavailable.

### Security
- The runtime image and Go toolchain are now pinned to Go `1.25.8`, removing the vulnerable stdlib version previously flagged by Trivy.
- The transitive `flatted` dependency is updated to `3.4.2`.
- No auth/session, privacy-boundary, export-data, or role-access contract was weakened in this release.

## [0.7.0] - 2026-03-16

### Added
- Cross-browser smoke coverage for core owner flows across Chromium, Firefox, and WebKit.
- Additional calendar prediction regressions for the shared facts-only explanation in unpredictable cycle mode.

### Changed
- Prediction explanation copy is now aligned across dashboard, calendar, and stats through one shared owner-only service policy.
- Cycle-factor explanations now stay anchored to the most recent known cycle start so newer onboarding or settings baselines do not get overridden by older manual starts.
- Settings now explain advanced tracking toggles and custom symptom empty, active, and archived states more clearly.
- Local Playwright runs now choose a free app port automatically when no explicit override is provided, which prevents parallel local runs from colliding on a fixed port.
- CI workflows now use Node 24-ready pinned GitHub Actions, while the full browser suite remains on Chromium and the new cross-browser lane stays focused on stable smoke coverage.

### Security
- No auth/session, privacy-boundary, export-data, or prediction-formula contract was weakened in this release.

## [0.6.1] - 2026-03-15

### Changed
- Secure-cookie deployments now emit `Strict-Transport-Security` at the app layer, and self-hosted proxy examples were aligned so they do not add a conflicting second HSTS policy.
- Self-hosted Docker defaults now pin concrete Ovumcy release tags and more specific runtime image versions instead of relying on floating image tags.
- Transport-level API error rendering was tightened by co-locating the shared helper with centralized error mapping and adding a focused regression for JSON, HTMX, and flash redirect branches.
- Spanish navigation and stats labels now use `Análisis` consistently instead of leaving the insights entry in English.

### Security
- Security workflow scanning now uses a digest-pinned Trivy image, and the runtime Dockerfile now ships from Alpine `3.22.3` so the published image no longer carries the vulnerable OpenSSL packages flagged by Trivy.
- No auth/session, privacy-boundary, or export-data contract was weakened in this release.

## [0.6.0] - 2026-03-15

### Added
- Owner-only cycle factor tracking for daily logs, exports, and conservative stats context (`stress`, `illness`, `travel`, `sleep disruption`, `medication changes`).
- A privacy-safe hero demo asset pack, including the mobile install prompt capture contract and refreshed demo documentation.

### Changed
- Stats now stay more conservative with sparse data: basic insights unlock later, reliability messaging is clearer, and early-cycle empty states are simpler.
- Dashboard and settings owner flows were refined to reduce redundancy, improve day logging clarity, and better align destructive copy with actual behavior.
- Irregular-cycle prediction copy now avoids implying a precise ovulation date when recent data is sparse and prefers more cautious wording.
- HTML regression coverage and Codecov publication were tightened so patch-status checks remain reliable in CI.

### Security
- No auth/session or privacy-boundary contract was weakened in this release; owner-only cycle factors remain sanitized outside the supported owner browser path.

## [0.5.0] - 2026-03-15

### Added
- PDF export with embedded fonts for printable cycle summaries alongside the existing CSV and JSON exports.
- Advanced owner tracking controls and richer phase/context insights, including BBT and cervical mucus tracking surfaces.
- Runtime-gated public registration via `REGISTRATION_MODE=open|closed` for operator-restricted self-hosted instances.
- Local operator CLI commands for account audit and removal (`users list`, `users delete <email>`).

### Changed
- Registration now acknowledges recovery codes inline after sign-up, and login/register flows preserve safer client UX without storing passwords in browser storage.
- Auth, logout, and destructive settings flows were hardened with broader cookie cleanup, sanitized request/security logging, and tighter browser/API regressions.
- Dashboard, calendar, onboarding, and settings owner flows were simplified and polished across desktop and mobile, including safer fixed-tabbar spacing and lower-friction daily logging.
- Base self-hosted compose defaults now bind to loopback by default, and operator docs were updated to reflect the local/private baseline versus dedicated public reverse-proxy stacks.
- Browser and backend regression coverage was expanded and refactored around more stable behavior contracts for auth, settings, export, onboarding, and mobile layout flows.

### Security
- Public sign-up can now be disabled without introducing a browser admin surface, reducing exposure for internet-facing operator-managed instances.
- Request and security event logging now avoid raw health-date paths and clear all auth-related cookies consistently on logout and account deletion.

## [0.4.1] - 2026-03-10

### Added
- Full Spanish first-party UI localization alongside English and Russian.
- Localized segmented date fields for onboarding, settings cycle, and export flows so day/month/year labels and picker controls remain accessible across supported locales.

### Changed
- Language switching, locale-aware server/browser date formatting, and related regression coverage were extended to cover Spanish across backend and Playwright checks.
- Chromium-owned native date input labels were replaced in affected flows while preserving the existing ISO `YYYY-MM-DD` transport contract.
- README now documents the supported UI languages and `DEFAULT_LANGUAGE` values for self-hosted operators.

## [0.4.0] - 2026-03-09

### Added
- Owner-managed custom symptom lifecycle with create, rename, hide, and restore flows that preserve historical logs, exports, and stats.
- Focused backend and browser regressions for owner-only symptom routes, archived-symptom behavior, request-local onboarding/settings dates, and simplified settings symptom controls.

### Changed
- Settings and onboarding now keep request-local cycle dates stable through the raw `ovumcy_tz` IANA cookie contract plus an onboarding `client_timezone` fallback.
- Custom symptom validation now blocks duplicate, built-in, markup-like, and over-limit names with row-local HTMX feedback instead of silent failures.
- Settings custom symptom controls were simplified to name-and-icon management; color remains a stored compatibility field with default-on-create and preserve-on-update behavior.
- Danger-zone clear-data flow now removes owner custom symptoms together with daily logs and cycle settings while preserving built-in symptom definitions.
- Settings, dashboard, and calendar symptom UI was tightened to reduce overflow, hide empty custom-symptom groups, and keep compact chips readable.

## [0.3.2] - 2026-03-08

### Changed
- Frontend runtime was prepared for strict CSP by removing Alpine and inline script dependencies from shared templates and client-side flows.
- Default HTTP responses now include a first-party Content-Security-Policy, and HTMX is configured in CSP-safe mode.
- Browser and API regressions were updated to use stable data hooks instead of Alpine-specific selectors and inline state.
- The web app manifest is now served with the correct `application/manifest+json` content type.

## [0.3.1] - 2026-03-07

### Changed
- Rate-limit responses now flow through shared API error mapping instead of hand-rolled middleware transport branches.
- Recovery-code issuance page is now single-view transport and clears its page cookie after the first successful render.
- Auth and recovery regression coverage was updated to keep secrets out of JSON/URLs and to align browser smoke tests with the single-view recovery flow.
- Several API regression tests were simplified to focus on stable outcomes instead of brittle Alpine/HTMX/template wiring details.
- Manual quick-start documentation now includes a PowerShell `SECRET_KEY` example.

## [0.3.0] - 2026-03-07

### Added
- Mobile PWA install support with a web app manifest, home-screen icons, and a shared install prompt for supported mobile browsers.
- Regression coverage for the shared mobile install banner and native install-prompt wiring.
- Baseline browser hardening headers on HTTP responses (`X-Content-Type-Options`, `Referrer-Policy`, `Permissions-Policy`, `X-Frame-Options`).

### Changed
- Mobile PWA support is currently install-only; offline mode and service workers remain intentionally deferred pending privacy review.
- Code scanning and security automation were expanded with dedicated CodeQL, gosec, Trivy filesystem/image scans, CycloneDX SBOM generation, and Codecov coverage reporting in CI.
- HTMX not-found responses now flow through centralized error mapping.
- Backend complexity was reduced and regression coverage increased across startup/bootstrap, API regression tests, and cycle/export services.
- Startup logging was hardened to avoid exposing forgot-password rate-limit details.
- README and public project documentation were refreshed to better explain product scope and self-hosted positioning.

## [0.2.5] - 2026-03-07

### Added
- Optional Postgres runtime support for advanced self-hosted deployments.
- Official local/private bundled Postgres compose stack under `docs/examples/postgres/`.
- Official public self-hosted Postgres reverse-proxy examples for Caddy and Nginx.
- Dedicated Postgres browser smoke lane in CI.

### Changed
- Auth/session handling was hardened so sealed auth cookies are enforced and forced password resets revoke stale sessions.
- SQL tracing was hardened to keep bind values out of warn/error logs.
- Self-hosted documentation now covers baseline operations, backup/restore, configuration profiles, and both SQLite and Postgres deployment paths.
- Docker-backed Postgres tests and CI coverage were stabilized for cold GitHub runners.

## [0.2.0] - 2026-03-04

### Added
- Security policy in `SECURITY.md`.
- Contribution guidelines in `CONTRIBUTING.md`.
- Code of conduct in `CODE_OF_CONDUCT.md`.
- Public brand assets (`web/static/brand/*`) and SVG favicon.
- Mobile quick navigation tab bar for faster section switching.
- Dark mode with persistent client-side preference (`ovumcy_theme`) and localized theme toggle labels.
- Playwright smoke coverage for theme persistence across reload and secondary page in one browser context.
- Register page client-validation hooks for password-mismatch UX.

### Changed
- Date validation was hardened in onboarding step 1 and settings cycle start bounds.
- Dashboard cycle-day calculation is now bounded by cycle length, and stale-cycle detection uses owner cycle anchor (`last_period_start`) to avoid misleading stale data.
- Dashboard predictions are projected into upcoming cycles, and stale baseline dates now show explicit warning/unknown states.
- Date formatting is locale-aware in dashboard and settings export summaries (RU/EN consistency).
- Settings cycle warnings now render contextually instead of keeping all variants visible in DOM.
- Settings export range uses native `type="date"` inputs with min/max bounds where supported.
- Calendar opens today's editor by default when `/calendar` has no `day`/`month` query parameters.
- Calendar/day-editor mobile layout was tightened to prevent clipped badges and reduce form footprint on narrow screens.
- Day editor now uses explicit `Save` action; field-change auto-save was removed.
- Symptoms are grouped into logical panels across dashboard and day-editor layouts.
- Stats cards and chart captions now show explicit no-data states; trend/symptom panels reserve stable height on large screens.
- Stats current-phase card follows stale-cycle logic and shows unknown/stale hints when baseline is outdated.
- Profile save supports inline HTMX success feedback; success statuses are dismissible with explicit close controls.
- Desktop nav user block styling was refined: user identity is metadata (not tab-like), logout has clear destructive affordance, and profile-name hinting was simplified.
- Navbar current-user label typography was softened (no all-caps emphasis).
- Light-theme range slider thumbs have improved contrast.
- Register password mismatch now shows inline validation before submit and keeps both password fields intact.
- Privacy breadcrumb naming was aligned with authenticated navigation labels (`Dashboard`/`Панель`).
- Russian copy was polished for consistent use of `надёжный`.
- Language switch active state styling was hardened for mobile with explicit `aria-current` behavior.

## [0.1.0] - 2026-02-23

### Added
- Initial public release of Ovumcy.
- Privacy-first menstrual cycle tracking with:
  - daily logs (period day, flow, symptoms, notes),
  - cycle predictions (next period, ovulation, fertile window),
  - calendar and statistics views,
  - CSV/JSON export,
  - Russian/English localization.

[Unreleased]: https://github.com/ovumcy/ovumcy-web/compare/v1.9.0...HEAD
[1.9.0]: https://github.com/ovumcy/ovumcy-web/compare/v1.8.0...v1.9.0
[1.8.0]: https://github.com/ovumcy/ovumcy-web/compare/v1.7.0...v1.8.0
[1.7.0]: https://github.com/ovumcy/ovumcy-web/compare/v1.6.0...v1.7.0
[1.6.0]: https://github.com/ovumcy/ovumcy-web/compare/v1.5.0...v1.6.0
[1.5.0]: https://github.com/ovumcy/ovumcy-web/compare/v1.4.0...v1.5.0
[1.4.0]: https://github.com/ovumcy/ovumcy-web/compare/v1.3.0...v1.4.0
[1.3.0]: https://github.com/ovumcy/ovumcy-web/compare/v1.2.0...v1.3.0
[1.2.0]: https://github.com/ovumcy/ovumcy-web/compare/v1.1.1...v1.2.0
[1.1.1]: https://github.com/ovumcy/ovumcy-web/compare/v1.1.0...v1.1.1
[1.1.0]: https://github.com/ovumcy/ovumcy-web/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/ovumcy/ovumcy-web/compare/v0.9.5...v1.0.0
[0.9.5]: https://github.com/ovumcy/ovumcy-web/compare/v0.9.4...v0.9.5
[0.9.4]: https://github.com/ovumcy/ovumcy-web/compare/v0.9.3...v0.9.4
[0.9.3]: https://github.com/ovumcy/ovumcy-web/compare/v0.9.2...v0.9.3
[0.9.2]: https://github.com/ovumcy/ovumcy-web/compare/v0.9.1...v0.9.2
[0.9.1]: https://github.com/ovumcy/ovumcy-web/compare/v0.9.0...v0.9.1
[0.9.0]: https://github.com/ovumcy/ovumcy-web/compare/v0.8.5...v0.9.0
[0.8.5]: https://github.com/ovumcy/ovumcy-web/compare/v0.8.4...v0.8.5
[0.8.4]: https://github.com/ovumcy/ovumcy-web/compare/v0.8.3...v0.8.4
[0.8.3]: https://github.com/ovumcy/ovumcy-web/compare/v0.8.2...v0.8.3
[0.8.2]: https://github.com/ovumcy/ovumcy-web/compare/v0.8.1...v0.8.2
[0.8.1]: https://github.com/ovumcy/ovumcy-web/compare/v0.8.0...v0.8.1
[0.8.0]: https://github.com/ovumcy/ovumcy-web/compare/v0.7.2...v0.8.0
[0.7.2]: https://github.com/ovumcy/ovumcy-web/compare/v0.7.1...v0.7.2
[0.7.1]: https://github.com/ovumcy/ovumcy-web/compare/v0.7.0...v0.7.1
[0.7.0]: https://github.com/ovumcy/ovumcy-web/compare/v0.6.1...v0.7.0
[0.6.1]: https://github.com/ovumcy/ovumcy-web/compare/v0.6.0...v0.6.1
[0.6.0]: https://github.com/ovumcy/ovumcy-web/compare/v0.5.0...v0.6.0
[0.5.0]: https://github.com/ovumcy/ovumcy-web/compare/v0.4.1...v0.5.0
[0.4.1]: https://github.com/ovumcy/ovumcy-web/compare/v0.4.0...v0.4.1
[0.4.0]: https://github.com/ovumcy/ovumcy-web/compare/v0.3.2...v0.4.0
[0.3.2]: https://github.com/ovumcy/ovumcy-web/compare/v0.3.1...v0.3.2
[0.3.1]: https://github.com/ovumcy/ovumcy-web/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/ovumcy/ovumcy-web/compare/v0.2.5...v0.3.0
[0.2.5]: https://github.com/ovumcy/ovumcy-web/compare/v0.2.0...v0.2.5
[0.2.0]: https://github.com/ovumcy/ovumcy-web/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/ovumcy/ovumcy-web/releases/tag/v0.1.0
