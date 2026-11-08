# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

*New entries accumulate as per-pull-request fragments in [changelog.d/](changelog.d/) and are assembled here at release.*

## [2.0.0] - 2026-11-06

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

- **`PATCH /api/v1/days/{date}` updates only the fields a request names.** `PUT` on the same path
  replaces the whole day, so a client that sent `{"mood": 3}` to a period day also erased the period,
  the flow, the temperature and the notes. Because the day was no longer a period day, it also lost
  its cycle start, and the cycle statistics moved. `PATCH` merges instead: a field the body omits keeps
  its stored value, the cycle start included, and an explicit `null` clears a field. The rules that
  follow from the period flag still apply. Stating `is_period: false` clears the flow and the cycle
  start, exactly as it does under `PUT`. `PATCH` has the same owner, CSRF, rate-limit and validation
  rules as `PUT`. Two concurrent `PATCH` requests on the same day that name different fields both
  keep their field, on PostgreSQL as on SQLite, including when the day had no entry yet. An
  `ack_period_tip` sent with a `PATCH` that leaves `is_period` out counts when the saved day is a
  period day. A field the account hides is never changed through a form body. A form-encoded day
  write, `PUT` or `PATCH`, reads its fields from the request body only, never from the URL query
  string. `PUT` stays a full replace; `docs/openapi.yaml` now warns about this and points clients
  that write only some fields at `PATCH`. The bundled UI still saves with `PUT`. On PostgreSQL, a
  `PUT`, a manual cycle-start mark or the period autofill that runs at the same time as another
  write of the same day no longer reverts that write's fields, a field the account hides included.

- **The calendar feed can name what each event is, if you ask it to.** Every `.ics` event used to
  carry the same neutral title, `Ovumcy: reminder (estimate)`, so a subscribed calendar could not
  tell an estimated period from an estimated ovulation day (#1159). Generating or rotating the feed
  link now offers a checkbox, off by default, that switches the titles to `Period (estimate)` and
  `Ovulation (estimate)` in your interface language. The choice belongs to that link: rotating
  asks again, pre-filled with the current choice.

  Leaving the box unticked keeps the feed byte-for-byte as it was. Ticked, only the titles
  change: the same estimated days are published, every title keeps the estimate qualifier, each
  event still carries the medical-safety disclaimer, and every case where the app withholds
  predictions still empties the feed. The settings card warns that your calendar provider, and
  anyone you share that calendar with, will see the titles, and the egress ledger names the mode
  your current link sends. Upgrading applies migration 043, which adds the opt-in column with every
  existing feed left neutral.

- **The baseline `docker-compose.yml` now lists the five settings it was omitting.**
  `RATE_LIMIT_CALENDAR_FEED_MAX` (`20`), `RATE_LIMIT_CALENDAR_FEED_WINDOW` (`1m`),
  `WEBHOOK_BLOCK_PRIVATE_ADDRESSES` (`false`), `REMINDER_SCHEDULER_ENABLED` (`false`) and
  `REMINDER_SCHEDULER_HOUR` (`9`) are read at boot and documented as supported knobs, but reached
  the container only through `.env`, unlike every other knob the file spells out with its default.
  Each new entry carries that same loader default, so a stack started from the baseline behaves
  exactly as before.

- **The theme gains a "System" option that follows the browser.** Settings offers it beside Light
  and Dark. It is resolved through `prefers-color-scheme` at apply time and keeps following it
  live, so a system that switches to dark at sunset reaches a page that is already open, not only
  the next one. `html[data-theme]` keeps carrying the resolved `light`/`dark`, so every rule
  written for those two still applies, and an owner who already saved Light or Dark keeps it.

- **The BBT chart reads back.** Pointing at the chart on Insights draws a hairline at the cycle day
  under the pointer and names it: the day, its date, and the reading with its unit — or that the day
  has no reading, rather than borrowing a neighbour's. It works by tap as well as by hover, and
  closes on Escape, on a tap elsewhere, or when the pointer leaves.
- **Every reading is also text.** A disclosure under the chart opens a table of the same series —
  cycle day, date, temperature — so no value is reachable by hovering only. The drawing itself stays
  hidden from assistive technology; the table is its equivalent.

- **The runtime image is now mirrored to Docker Hub, and the README carries its pull count.** The
  image published to `ghcr.io/ovumcy/ovumcy-web` is copied to `docker.io/ovumcy/ovumcy-web` under
  the same tags, so an operator whose environment pulls from Docker Hub can run the same release
  without a registry mirror of their own. The copy is content-addressed: it moves the manifest
  bytes and the Cosign signature that already covers them, never a second build, so both registries
  answer with the one digest this project signed. The README and the security policy now name the
  Docker Hub reference beside the GHCR one and say that every verification command takes either,
  because the build provenance is issued against the digest rather than against a registry and the
  signature is copied along with the manifest it covers.

  Nothing reaches Docker Hub until the GHCR release is finished — signed, attested, read back, and
  every public GHCR tag resolved anonymously to the signed digest — and the mirror is not reported
  published until every mirrored tag has been resolved anonymously to that same digest and the
  signature has been found on Docker Hub beside it. A failure in the mirror cannot affect the GHCR
  release, which by then already exists in full.

  The mirror uses its own publishing credential and skips entirely when that credential is absent,
  so a fork or a clone publishes to GHCR exactly as before. The credential that authenticates
  base-image pulls is deliberately not reused: it is handed to jobs that run on every pull request,
  and write access to a public registry does not belong there.

- **The calendar feed carries the temperature-confirmed ovulation day.** Once a basal body
  temperature shift confirms ovulation in the current cycle, the `.ics` feed publishes that day,
  exactly when the dashboard, the calendar and the JSON API show it — including after a cycle has
  run so long that projected dates are paused. It keeps the feed's neutral title and the
  medical-safety disclaimer, and it shares the ovulation event's identifier, so a calendar app that
  already holds a projection for the same day updates it rather than adding a second one. It is
  withheld in irregular (unpredictable) cycle mode, during a pregnancy pause and before the first
  completed cycle, and earlier cycles are never included. Webhook reminders are unchanged: they
  announce only days still ahead.

- **Webhook reminders can be delivered in ntfy's native shape.** A webhook URL ending in
  `?format=ntfy` (or carrying `format=ntfy` among its other query parameters) now receives the
  reminder title in `X-Title`, an emoji tag per reminder kind in `X-Tags`, and a `text/plain` body of
  the message followed by the disclaimer, instead of the JSON envelope an ntfy topic would show as a
  raw blob. The disclaimer is sent on every notification in this format too. Other query parameters
  such as `?auth=` pass through unchanged, and a title carrying control characters drops the header
  rather than failing the delivery. URLs without `format=ntfy` keep the JSON envelope byte for byte;
  timeouts, the no-redirect rule, the response caps and host-only logging are identical in both
  formats.

- **The Insights cycle stack now says when it had to cut a row short.** The
  stack draws every cycle against one shared scale, and that scale stops at
  sixty days, so a completed cycle longer than that had its band end at the
  edge rather than at the cycle's own end — two such rows were drawn the same
  width whatever their lengths, with nothing on the page saying so. The band
  of a cut row is now marked at its trailing edge, and a line under the stack
  explains that a cycle longer than the scale is cut off and that such a
  row's length is read from the number beside it. The number itself is
  unchanged: every row has always printed its true length.

- **Insights states what changed.** The page carries a statements section built from the account's
  own records: how its cycle length moved between the earlier and the recent half of its completed
  cycles, and which symptom keeps returning in the same cycle phase ("logged in 4 of 5 recorded
  phases"). Statements are descriptive — they name no cause and read no projected date, so they stay
  on the page for an owner whose predictions are suppressed. The cycle-length statement needs three
  completed cycles, the recurrence statement three recorded phase occurrences and a majority of
  them; below that nothing renders rather than a hedged version.

- **`GET /api/v1/stats/overview` gains `ovulation_confirmed`.** It names the substitution above
  directly, mirroring the dashboard's own confirmed/exact distinction: `ovulation_exact` keeps its
  original meaning (an exact per-owner luteal-phase fit vs. the clamped 14-day fallback) and
  `ovulation_confirmed` reports separately whether `ovulation_date` is a day inferred from the
  owner's own temperature shift — not a measurement of the ovulation itself — rather than a model
  projection. The two can differ, and a fallback-luteal account's confirmed cycle is exactly
  that case. Suppression is unaffected: a confirmed observation changes which day is named, never
  whether one may be named at all.

- **A pull-count badge on the README.** A daily job reads the published image's download counter from its package page and republishes it as a badge document, so the count is visible without opening that page. It counts image pulls — upgrades and CI included — not installations, and the service itself still reports nothing.

- **Link an additional OIDC identity from Settings, or via the operator CLI.** Attaching a new
  sign-in identity to an existing account is now authorised by a live authenticated session plus a
  fresh provider re-authentication (Settings → *Connected sign-in identity*), or by the operator
  running `ovumcy link-oidc-identity <email>|--id <id> --issuer <issuer> --subject <subject>` for an
  account with no working sign-in at all.

- **`ovumcy users set-email --id <id> <email>` — a repair for an account that cannot sign in and
  cannot be named by its address.** Sign-in normalization is strict about the stored email, and the
  first boot after upgrading rewrites the legacy decorated rows to their bare address. Two kinds of
  row it cannot rewrite are left standing, and both are locked out: two accounts on one mailbox
  (the older one keeps the address), and a value that reduces to no plain address at all. The
  runbook told operators to review such a leftover and "remove or re-home it" — but there was no
  re-home command, and the address form of `users delete` could not reach the row either: the
  stored string is refused as invalid input, while the bare address inside it belongs to the *other*
  account, so a delete typed that way erased the wrong account's entire health record.

  `set-email` addresses the account by the id `users list` prints. The new address is validated
  under the same rule a sign-in input is normalized under, refused if another account already
  answers to it, and written under a compare-and-set on the value the operator was shown, so a row
  that changed in between is reported rather than overwritten. The health record is untouched — the
  whole point of having a repair that is not a deletion — and every session of that account is
  revoked in the same write, because the address is the login identity. `users delete` gains the
  same `--id` form, whose confirmation quotes the exact stored address, id and role. The
  self-hosted runbook now walks the non-destructive path and says how to verify it.

- **`GET /api/v1/stats/overview` gains `luteal_phase_personalised`.** It reports whether
  `luteal_phase` was inferred from the owner's own logs (`InferUserLutealPhase`'s refined return)
  rather than the 14-day model constant — the signal `ovulation_exact` was wrongly described as
  carrying. The two are independent: a personalised luteal phase can still be clamped
  (`ovulation_exact = false`), and the 14-day default can still fit a cycle exactly
  (`ovulation_exact = true`). `false` means the owner's logs did not yet support an inference, not
  that a personalised value was tried and rejected. Cleared to `false` under fertility suppression,
  alongside `ovulation_date` and `ovulation_exact`, so it never asserts personalisation about a
  projection this tier has already withheld.

- **`ovumcy reset-password` gains the same `--id <id>` addressing `users delete` and `users set-email`
  already have.** The operator repair for an account locked out under a legacy stored email — two
  accounts on one mailbox, or a value that no longer parses to a plain address — reached everything
  except its password: `set-email` could re-home such a row and `delete` could remove it, but
  `reset-password` still took only a bare address, which either refused outright for the locked-out
  row or, on a shared mailbox, quietly resolved to the *other* account that already holds the bare
  address — resetting a stranger's password instead of the one the operator meant.

  `reset-password <email>|--id <id>` mirrors the sibling commands' exact convention: mutually
  exclusive, exactly one required, same flag spelling and the same `users list`-pointing error
  wording. As a second line of defense, the email form now also refuses — naming every matching id
  — rather than silently resolving to whichever row a lookup happens to return first, if an address
  is ever ambiguous. The same refusal was lifted into the shared lookup `users delete <email>` and
  `webhook show|set <email>` already resolve accounts through, so all three inherit it uniformly
  instead of only the one command this fix started on.

- **Settings now accounts for everything that can leave the instance, and for what each route can
  actually prove.** The webhook and calendar-feed sections have merged into one owner-only card,
  *Data leaving this instance*, and the two of them used to answer a weaker question than the one
  an owner was asking. Each said "configured" or "not configured", which is a fact about a stored
  value rather than about what the instance can do with it — a webhook endpoint the instance can no
  longer decrypt read as "configured", and a calendar link read as "active" whether or not it still
  resolved. Neither said a word about what the reminders and the feed actually carry.

  Each route now renders one state that the code can be shown wrong about. For webhook reminders
  those are: no endpoint stored; an endpoint stored that this instance can no longer read; an
  endpoint that names no host; this instance runs no reminder pass; delivery switched off; delivery
  on with no reminder type chosen; and delivery armed. Readability is decided before any toggle, so
  a broken application secret can no longer hide behind "delivery is off". For the calendar link,
  the word *active* is gone: what is knowable is which key signed the link, so the card says the
  link was issued under the key this instance runs now — the one case in which withdrawing it here
  cuts it off — or under a key it no longer runs, or before that was recorded at all.

  Beneath each state sits the one timestamp that route can prove, and nothing else. For webhook
  reminders that is the last delivery the receiving endpoint **accepted**, which is recorded after
  the fact and not when the send was decided. For the calendar link it is the one-time reveal of the
  subscribe URL being used. There is deliberately no count of fetches, no "last opened", no "in
  use" and no rotation history for the calendar feed: the feed's polls are not audited, by design,
  and no field exists that could answer a question about them.

  The card also lists, per route, exactly which fields leave — including the predicted date and the
  medical-safety sentence a reminder carries, and the event identifier an `.ics` entry builds from
  the reminder type and its date. The lists are pinned against what is really sent, so a change to
  either payload cannot quietly leave the page describing the old shape.

- **A webhook endpoint can be withdrawn on its own**, from a control beside its state, without
  submitting the whole settings form. Withdrawing it switches delivery off and clears the recorded
  delivery, while leaving your chosen reminder types and lead time exactly where they were. It works
  on an endpoint this instance can no longer read, which the previous route could not do: that path
  had to decrypt and re-save the URL, so the one row most in need of withdrawing was the one row it
  refused to act on.

- **`/privacy` links signed-in readers to that card.** The page itself stays unauthenticated and
  says nothing about how this instance is configured.

- **A webhook endpoint this instance can no longer read is no longer deleted by accident.** Leaving
  the URL field blank has always meant "keep the stored endpoint". When the stored value could not
  be decrypted — after the application secret is rotated — there was nothing to keep, and the save
  quietly stored an empty endpoint instead, but only when you were switching delivery off. So the
  destructive outcome arrived through the least alarming action on the page. The endpoint is now
  left exactly where it is: your reminder settings still save, delivery cannot be switched on over
  it, and removing it takes the withdraw control.

  All six locales now carry the egress section fully translated — German, Spanish, French, Italian,
  and Russian no longer fall back to the English placeholder copy seeded when the card shipped.

- **`DATABASE_URL_FILE` and `OIDC_CLIENT_SECRET_FILE` join `SECRET_KEY_FILE` as Docker Swarm/Compose
  secrets routes.** The runtime image ships shell-free, so the usual `sh -c 'export
  X=$(cat …)'` workaround to read a mounted secret file has no shell to run it in — `SECRET_KEY_FILE`
  was already the sanctioned way around that for the application secret, and the Postgres DSN and the
  OIDC client secret are the two other operator-supplied values that carry a live credential. Both
  read through the same bounded local-file helper as `SECRET_KEY_FILE` (rejects directories and
  special files, caps the read, trims a trailing newline) and the same precedence: the plain
  variable wins when both are set. Unlike a wrong `SECRET_KEY`, which fails loudly at first use, a
  wrong `DATABASE_URL` just points at the wrong database with no error — so that precedence is no
  longer silent: boot now logs one line per resolved value naming which variable supplied it, and
  names the `_FILE` variable specifically when it was set but ignored (never the value itself). A
  sqlite instance carrying a stale or dangling `DATABASE_URL_FILE` it never reads boots as before —
  the file is only opened when `DB_DRIVER=postgres` actually consumes it. The bundled and example
  compose stacks and `.env.example` now carry both variables alongside their existing `_FILE`
  sibling.

Insights gains a cycle stack: the last four completed cycles drawn one under
another on a shared scale, so a longer cycle is visibly longer instead of being
a number to compare against two other numbers. Each row carries its start date,
its recorded period days and its length; a day holding an entry keeps its mark.
Inferred phases for those past cycles follow the same "show historical phases"
preference the calendar already has — with it off, the stack shows only what
was recorded.

- `PATCH /api/v1/days/{date}` accepts an optional `base_<field>` for each day field and answers
  `409` when a based field was changed concurrently; a field sent without a base is written as
  before.

- The settings page now shows the day a calendar last checked the .ics feed link: "Last checked
  by a calendar on \<date\>". The date advances at most once per owner calendar day, on a
  successful poll only, and clears whenever the link is revoked, rotated, or cleared along with
  the rest of the account's data.

- The database now records when a webhook delivery was actually accepted, and which key
  regime a calendar-feed link was issued under. Both are written only when the event
  happens — a delivery mark appears after the endpoint answers, never when the send is
  attempted — and both start empty on upgrade rather than being guessed from existing
  data. Nothing displays them yet.

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
  check fails. Release tags no longer publish from their own tag push alone:
  the tagged commit must be contained in `main` with the same checks `latest`
  waits on already passed there, and the image gets the same pre-publish scan
  before it is pushed. The signer identity to verify with `cosign` is
  unchanged for both — the commands in
  [SECURITY.md](SECURITY.md#verifying-release-authenticity) still apply as
  written.

- **The temperature-shift documentation now says what the detector is and what it is not.**
  `docs/cycle-prediction.md` lists every surface the one detector feeds and spells out where the
  applied "3-over-6" rule departs from the full symptothermal protocol on purpose: no second
  indicator cross-checks a shift, a slow rise is never rescued by a fourth day, a day tagged
  `illness` or `sleep_disruption` is dropped whole, and the six values behind the coverline are
  recordings rather than calendar days, so skipped mornings lengthen the window backwards. The
  confirmed day stays an estimate inferred from a signal, and no accuracy interval is promised for
  it.

- **The medical-safety disclaimer is one sentence, written once.** The dashboard,
  Insights, the calendar, the calendar-feed panels and the webhook-reminders
  section used to spell "estimates, not medical advice or a method of
  contraception" out in three separate catalogue entries that had drifted apart.
  They now all render the same string, and every surface that showed the
  qualifier still shows it — the webhook section carries it as its own line
  instead of hiding it at the end of a paragraph.
- **The login screen uses one verb.** The heading, the submit button and the SSO
  button said "Log in", "Login" and "Sign in" in English, and mixed a noun with a
  verb in Spanish and French. All three now read as the same action in every
  language.
- **The dashboard says which dates its next-period range covers.** The status
  line rendered the predicted start window and the predicted period days with the
  same bare "date — date" string, so the two could not be told apart. Each now
  names itself, in the same words the calendar legend uses.
- **Warnings and save messages carry no emoji.** The future-date notice, the
  short-gap cycle-start confirmation and the self-care save message had a glyph
  baked into the translated sentence; the future-date notice now shows the
  regular alert icon and the other two are plain sentences.

- **Changelog entries now accumulate as per-pull-request fragments.** Each pull
  request adds one file under `changelog.d/` instead of editing the
  `[Unreleased]` section of `CHANGELOG.md`; a CI check enforces the fragment and
  `go run ./scripts/changelogd assemble` folds the accumulated fragments into
  `CHANGELOG.md` when a release is cut. Entries written before this change stay
  under `[Unreleased]` until the next release consumes them.

- **Cycle lengths are drawn as a dot plot instead of bars.** Bar length encodes magnitude, which
  obliges the axis to start at zero — and on a zero-based axis a 28-day cycle and a 29-day one are
  the same full-height bar. The values now sit as connected points on a day axis around the data, so
  a one-day difference is visible; the average stays as a reference line, with its label inset inside
  the plot area instead of pressed against its right edge.

- **A failed day-entry save reads as a connection problem, not as a health warning.** The failure
  notice uses a neutral surface instead of the red error block a health screen shares with real
  findings, and carries calm copy in all six UI languages ("Couldn't save — check your connection.
  Your entry is still here."). When the server did answer with its own message, that message is
  shown instead, on the same neutral surface and with the same retry.

- **On the calendar, editing a day is now the primary action and "Today" is compact navigation.**
  The month row's "Today" jump carried the filled primary button while "Edit entry" — the reason the
  calendar screen is opened at all — sat in an outline secondary, so the loudest control on the page
  was the one that only moves the view. "Edit entry" now renders in the solid primary fill inside the
  day panel, and "Today" joins the previous/next month controls as a smaller secondary pill. Both
  controls keep their wording, their position and their keyboard reachability; the compact pill keeps
  the shared 44px tap-target height, and the newly-primary action was measured against the WCAG AA
  4.5:1 text-contrast bar in both themes.

- **The install offer is one row on a phone, and a permanent entry in settings.** The "Install
  Ovumcy" block on mobile carried a kicker, an icon, a heading, a per-platform paragraph and two
  stacked full-width buttons, which took roughly half of the first screen. It is now a single row —
  the install action plus a dismiss control — shown only while the browser has a native prompt
  pending, and the settings interface section carries a quiet "Install Ovumcy" entry that describes
  the path for this device (native prompt, iOS share sheet, browser menu, or already installed) and
  starts the install where the browser supports it. Dismissing the row therefore no longer hides
  the offer for good, and the dismissal is remembered in the browser, not on the server. Nothing
  about the progressive-web-app surface itself changed: still a manifest and the install prompt,
  with no service worker and no offline cache.

- **Onboarding step 2 stops asking for an age bracket, and the mode question is now skippable in
  one visible step.** The step collected two answers it described as optional; only one of them
  frames the product. The age question leaves onboarding entirely — it stays available under
  Settings → Cycle, and onboarding no longer writes that column at all, so a bracket chosen in
  settings can never be overwritten by finishing the flow. The mode question ("Avoid pregnancy",
  "Trying to conceive", "Track my health") stays on the screen, keeps "Track my health"
  preselected as the neutral default, and gains a plain "Skip this question" button next to the
  choices instead of a footnote. Skipping submits no answer and completes onboarding on the
  neutral default. The three-line note explaining that the prediction algorithm is identical no
  longer appears during onboarding; the dashboard and stats pages still carry it where the mode is
  displayed.
- **The current mode can be changed from the dashboard.** The "Current mode" panel now offers the
  two modes the account is not in as one-click chips, so switching no longer means a trip into
  settings. A chip saves only the mode — every other cycle setting is left exactly as it stands —
  and the page re-renders in the new framing. The mode is never changed for the owner: nothing
  logged on a day, a positive pregnancy test included, rewrites it.

- **Prediction reliability now reads as context on the prediction, not as a fourth
  metric.** On Insights it moves out of the KPI row — which is back to a stable
  three-card layout — and appears as a compact line beside the prediction
  explainer, keeping the same wording: the reliability label, how many cycles it
  rests on, its hint, and the cycle-factor chips. It still appears in exactly the
  same situations as before.

- **A phone now has one navigation, not two.** The header menu repeated Today, Calendar, Insights
  and Settings — the same four destinations the bottom tab bar already offers — so opening it
  pushed the page down to show a second copy of the bar that stayed visible underneath. The menu
  is now the account menu: the profile entry and sign-out, nothing else. Section switching belongs
  to the bottom bar, and the desktop navigation is unchanged.

- **The desktop header now offers one route to settings instead of two.** A gear icon sat beside
  the account chip in the same header navigation and both opened the settings page, so the row
  asked twice for the same destination and neither control said which one to use. The gear is
  gone; the account chip stays, because it also shows the account name (or invites you to add
  one) and it lands directly on the account section of settings. The phone layout is untouched —
  the bottom tab bar keeps its settings tab, and the header menu keeps the same account entries.

- **Every tracking toggle in settings now reads "Show ...".** The intimacy, cycle-factor and notes
  rows used to be phrased as "Hide ...", so an enabled switch meant *visible* on some rows and
  *hidden* on others. All three are now positive and start checked, matching the fields that are
  visible by default; their saved state is unchanged by the upgrade, and the copy is updated in all
  six interface languages. The settings form posts the new `show_sex_chip`, `show_cycle_factors` and
  `show_notes_field` fields; the `/api/v1/users/current/tracking` JSON body keeps its published
  `hide_*` fields, so scripted callers need no change.

- **A cycle running past its expected end now states a fact instead of a verdict.** Past the
  expected end plus a week the dashboard used to answer "Cycle data may be outdated. Update your
  last period start date." — advice about the app, at the moment the question is about the body.
  It now names what was measured: "This cycle is 6 days longer than your usual range." when that
  range exists, "Still inside your recorded range of 24 to 45 days." when the running cycle has
  not left it, and — for an account with no completed cycles, exactly one, or an unpredictable
  rhythm — nothing but the cycle day, because a "usual range" that has never been observed is an
  invented fact. Which of the three appears is decided by the same completed-cycle signal the
  Insights "Prediction reliability … based on N completed cycles" card already reads, so the two
  screens cannot promise different amounts of confidence. The notice carries the logging actions
  that fit beside it — mark a new cycle start, a pregnancy test, symptoms — and predictions stay
  estimates, with the existing qualifiers and the medical disclaimer untouched.

- **Marking a period day now asks about the cycle start in place.** On a day where the existing
  cycle-start policy would suggest a new cycle, turning the period toggle on reveals one calm
  question next to it — "Start a new cycle from this day?" with yes and no as small controls —
  instead of leaving the answer to the separate "Mark new cycle start" action further down the
  page. The answer rides the same save as the entry: nothing is recalculated or written before
  that save, an untouched question writes nothing, and declining leaves a plain period day. The
  separate control stays where it is for corrections, and the hint that used to point at it steps
  aside while the question is on screen.

- **The pregnancy-test field stops treating "did not test" as an answer.** It used to render three
  identical segments with "None" filled at full weight, so every day in the app looked like a day
  with a test result on it. An untested day is now absent data: the field offers the two results
  only, nothing is filled, and it says so in words — "No result recorded for this day." The two
  results carry the same neutral tokens as every other day picker — no green, no red, no
  celebration, no emoji — and a selected one is confirmed by a filled ring as well as by its fill,
  so the state survives a colour-blind reading. Because "did not test" is no longer a segment
  anyone can tap, a saved result comes with an explicit secondary **Remove result** action next to
  it, which clears the day back to no result on the next save; a mis-tap is correctable before
  saving by choosing the other result, and after saving by removing it. Both day-entry surfaces —
  the dashboard journal and the calendar day editor — render the same control. Nothing else
  changes: the stored values stay `none`, `negative` and `positive`, a positive test still only
  pauses cycle predictions until a new period is logged (with the same neutral save message and
  the same medical guidance), and no test result of any kind rewrites the tracking goal, the
  predictions, or the tone of the product.

- **The dashboard journal now names the day it is writing to, and offers Today / Yesterday next to
  it.** Evening entries are routinely made after midnight, and the journal filed them under the new
  day with nothing on screen to say so — a health record silently gains a day. The header keeps the
  date visible while the form is filled, in the interface language and in the timezone the request
  resolves to, and the Today / Yesterday pair beside it switches the entry to that day. The
  Yesterday jump is no longer hidden once yesterday already holds an entry, which is exactly when a
  misfiled late-night entry has to be moved. Both are plain links to the screens that already edit
  those days, so switching writes nothing and never carries half-filled values from one day to
  another; the accessible name of the pair ships in all six interface languages.

- **The dashboard journal saves itself, and a save can be taken back.** The
  "Update entry" button is gone: the day form has one save model, the auto-save
  that was already running behind it. The row under the form stays silent while
  there is nothing to report, says "Saved" once the server has answered, and
  offers a single step back beside it — Undo restores the state the day held
  before that save and sends it the same way, or clears the day again when the
  save was the first one on an empty day. Nothing is remembered outside the open
  page. A save that does not land now reuses the day editor's neutral notice
  with its Try again control instead of a bare state change, on the autosave and
  the HTMX paths alike, and everything typed stays on screen. The calendar day
  editor keeps its explicit Save.

- **The dashboard save status states the fact instead of praising the
  mechanism.** While a change is on its way the journal reads "Saving…", and
  once the server has answered it reads "Saved" — in every language — rather
  than announcing that the save happened automatically.
- **The cycle-factors helper line is gone from the journal and the calendar day
  editor.** It restated the "Cycle factors" label, and nothing on this screen
  has to be filled in, so the fieldset now speaks for itself.

- **A new account no longer sees a fertile window invented from its setup
  defaults.** Until one cycle has been completed, the dashboard status header
  shows no fertility status and no ovulation estimate: with nothing recorded to
  measure, both were the onboarding cycle-length slider projected forward. The
  cycle day, the phase and the next-period estimate are unchanged, and an
  account tracking to conceive reads one line naming when its fertile window
  arrives instead of a date. Once a first cycle is completed, the header renders
  as before.

- **The dashboard journal shows what a day usually needs and folds the rest
  away.** Symptom chips stand two per row on a phone, at full tap size, so the
  picker no longer pushes the form off the first screen; the period toggle,
  flow, mood and symptoms stay in the open. Intimacy, cervical mucus, the
  pregnancy test, BBT, cycle factors and the note now wait behind a single
  "More details" disclosure, which opens by itself whenever the day already
  holds one of them, and any link pointing at a field inside it opens it too.
  Fields the tracking settings hide stay absent as before. Removing a saved
  pregnancy-test result is now a button standing after the two results instead
  of a third option inside them, so the choice reads — and is announced — as the
  two results it is.

- **On Insights, a card is as tall as what it holds, and section titles share one scale.** The
  "Top symptoms in your last cycle" card was stretched to the height of the BBT chart beside it, so
  a single "no data yet" line sat in 435px of empty card — the card now ends where its content ends
  (measured in the browser: same row, same top edge, its own height). The page also carried three
  competing title treatments: the small uppercase stat label, the section heading, and the form
  field label pressed into service as a heading. Section titles are now one treatment — "Recent
  cycle context" becomes a real subheading under "Recent cycle factors" — the uppercase label is
  left to the stat cards it belongs to, and the labels inside pattern, phase and cycle cards read
  as emphasized text rather than as a fourth heading level. No wording changed, and the page keeps
  its single `h1` with valid heading nesting.

- **The dashboard opens with a compact status header, and the journal comes
  straight after it.** The tall cycle hero, its four textbook phase cards, the
  separate status line, the reminder panel and the standalone mode panel are
  replaced by one header that always renders: a small cycle ring with the cycle
  day inside, a single line with phase, fertility status and the next-period
  estimate, and the usage-goal mode as a chip that opens the same two-mode quick
  switch. Late-cycle, stale-baseline and reminder notices, the prediction
  explainer, the "not medical advice" disclaimer and the cycle-factor hint keep
  their place under it, and the fill-missed-days link now sits below the day
  entry instead of above it. The Insights page no longer repeats the mode panel;
  the mode explanation now sits beside the goal control in Settings.

- **Mobile layout density: a whole month on one screen, insight cards two across.** On a phone the
  calendar grid now shows the full month with its header without scrolling — day cells are compact
  (about 45×51 px at 390 px width) while staying 44 px tap targets, and their day numbers, stripes
  and markers keep their sizes. The insights KPI cards sit two across from the smallest width
  instead of one per row, so the first chart is reachable without a long scroll.

- **The settings page is easier to navigate and its controls are sized to their job.** The section
  index now lists every section on the page — custom symptoms, your data, restore from backup and
  the danger zone were missing from it, so clearing data and deleting the account could only be
  reached by scrolling; on a phone the index stays one row that scrolls sideways. The cycle-length
  and period-length sliders no longer stretch across the whole card, and each shows its current
  value right beside the handle. The day/month/year date fields are sized to the two and four
  digits they hold instead of taking a third of the card each.

- **The dashboard frames timing for the goal that is about it.** An account
  whose usage goal is trying to conceive now reads the ovulation estimate in the
  status line beside the next-period one, and logs the morning temperature from
  the journal's visible tier instead of opening "More details" for it — the two
  things that goal is about. The estimate keeps the wording and the disclaimer
  every prediction surface carries, and follows the next-period gating exactly:
  an unpredictable cycle or a paused prediction suppresses it as before. The
  other goals read the page unchanged, and the disclosure stops counting a
  temperature that is no longer behind it.

- **The README now says what a refused publish leaves in GHCR.** The release workflow pushes the
  image by digest, scans every platform, and only then signs and tags it, so a refused scan leaves
  an untagged, unsigned digest in the public package. The image-verification section now states that
  this digest can exist, why it is safe to ignore (the Cosign check fails on it), and why it is not
  deleted: that needs delete rights over the whole package inside the publish job, which would let a
  compromised publish step delete signed releases too.

- **`LICENSE` now carries the full text of the GNU Affero General Public
  License v3.** The file previously held only the short notice header — the
  paragraph meant for the top of a source file — so the license text that
  notice points at ("you should have received a copy of the GNU Affero General
  Public License along with this program") was never actually distributed with
  the program, and license scanners reported the project as carrying no
  recognisable license. The copyright line moves to the README, which also now
  states in one sentence what section 13 requires of a modified network
  service.

- **The backup and restore helper in `docs/self-hosted.md` pins the same Alpine release as the
  runtime image again.** Both were moved to `alpine:3.22.3` in one commit, to clear the OpenSSL
  findings that failed the image scan; the Dockerfile has since been carried to `alpine:3.24.1` by
  the automated base-image updates, which read `Dockerfile` and compose manifests but not a fenced
  command in a Markdown runbook, so the documented helper stayed three minors behind on an image
  pinned for a security patch. The commands themselves — BusyBox `sh`, `tar czf`/`tar xzf` — need
  nothing from either release, so the older pin bought an operator nothing but the unpatched
  packages it was originally raised to escape.

- **The interface language is now part of the account, not just the browser.**
  A language chosen in `Settings` is stored on the account as well as in the
  `ovumcy_lang` cookie, and every sign-in — password, second factor, SSO,
  post-registration pickup — re-issues the cookie from it. Signing in from a new
  browser, another device, or after clearing cookies now shows the language the
  owner picked instead of falling back to the browser's `Accept-Language` or the
  operator's `DEFAULT_LANGUAGE`. An account that never chose a language keeps
  the previous behaviour exactly, and the stored language survives
  `Settings → Clear data`, which erases health records rather than the language
  the interface is read in.

- **The calendar feed is described as carrying estimated days.** The settings card, the subscribe
  page and the privacy notice said the feed carries predicted days; they now say estimated, in all
  six languages, since the feed can also carry a day confirmed by temperature readings.
- **The onboarding cycle-length hint quotes a common range of about 24-38 days**, the FIGO normal
  range for cycle frequency, instead of 21-35 days, in all six languages. The documentation now
  marks which cycle thresholds are clinical (the 24-day short-cycle boundary) and which are
  engineering heuristics. No prediction behaviour changes.

- **The mood scale names its steps.** The mood picker on the dashboard journal and in the calendar
  day editor kept its faces and gained names: the caption under the row names the two ends of the
  scale before anything is chosen and the chosen step afterwards, and every face carries the same
  name as its accessible name and its tooltip instead of the "3/5" position it announced before. A
  logged mood is read back in the day summary by name rather than as a fraction.

- **Settings sections fold on a phone.** Each card is a disclosure below 640px and opens on a
  tap; the section index still jumps straight into a folded one. On a wider screen, and with
  no JavaScript, the page is unchanged. A section holding unsaved edits never folds. Measured
  at 390px: the page went from 8856 px of scrolling to 1460.
- **The additional-tracking toggles no longer repeat themselves.** Five helper lines restated
  their own toggle's label and then repeated the section's own promise that turning a field
  off never removes what is already logged. The toggle that shades past cycles keeps its
  hint: those phases are derived, and its label does not say from what.

- **A button now picks a role, not a colour.** Primary, secondary and destructive are defined
  once as tokens on top of the semantic layer, together with the single inactive state they
  share, and every button reads them. The primary action paints one fill everywhere: the
  dashboard editor no longer re-tints it per cycle phase, so "Update entry" stops turning green
  on a fertile day. The quiet variant keeps its own accent ink, which now clears WCAG AA hovered
  as well as at rest (it measured 4.34:1 hovered before), and a destructive action finally has a
  hover fill in the dark theme.
- **The two remaining blues in the product are gone.** The calendar's logged-entry marker and
  the ring around the day being viewed were the only blue surfaces anywhere; both move to the
  warm accent ink, which carries its own dark value, and stay separable from the amber today
  ring — 5.29:1 light and 8.67:1 dark on a plain day cell, and 3.14:1 / 4.05:1 at worst over
  the phase washes, above the 3:1 floor a non-text indicator answers to.
- **An enabled setting no longer looks like a warning.** The settings toggles painted their
  on-state in the period rose, so "Auto-fill period days" read as an alert when it was simply
  on; the on-state now uses the affirmative warm accent every other selected control uses, on a
  flat fill.

- **The calendar legend leads the grid and names concepts instead of CSS states.** It sat under
  the month in the faintest text on the page, listing nine entries — one per shading variant — so
  it was off screen exactly while the grid was being read. It now renders above the weekday row,
  inside the same panel, as seven grouped entries: recorded period, predicted period days,
  predicted start window, fertile window, ovulation, today, logged entry. Each swatch is painted
  from the same definition as the day cell it explains, so the two can no longer drift apart, and
  the redundant non-colour encoding on the day cells (markers, dashes, borders) is unchanged.
- **A predicted period start is drawn differently from a predicted period day.** The dashboard's
  "Next period: 25 Aug — 27 Aug" is a range of possible START dates, while the grid shades the
  projected bleeding days; both used one shading, so a reader saw two different quantities in one
  visual language. Texture now separates fact from estimate: a recorded period day is solid, a
  projected period day is hatched, and the days the next period may start on carry a graded fill
  that fades across the window. The window is the range the dashboard already computes, so the two
  surfaces cannot disagree, and it appears only where a prediction is shown at all — an
  unpredictable cycle, a pregnancy pause or a cycle past its reference length still paints nothing.
  Day-number contrast stays above WCAG AA on every state in both themes (worst measured stop
  5.04:1).

- **Chips and cards are now one component each.** Nine names for the same selectable token
  (`check-chip`, `choice-chip`, `mood-chip`, `role-chip`, `sex-chip`, `radio-tile` and their size
  variants) collapse into `chip` plus shape, size and state variants; ten card names collapse into
  `card` plus a quiet level, a hero, a density step, a grid-fit step, a modal size and two danger
  emphases. Both read the theme from tokens, so neither carries a dark-theme block of its own.
  Rendering is unchanged except where the duplication was hiding an inconsistency: the account chip
  in the navigation now shows the pointer cursor over its link, the "sex logged" summary chip reads
  as selected in the dark theme instead of only in the light one, and the pregnancy-test row centres
  its two options on a phone like the sibling day pickers already did.

- **Settings, the journal and Insights are shorter to read.** Related controls now share one
  surface instead of each getting its own block: the cycle length, period duration and last-period
  date sit together, the three cycle switches carry their explanation inside the control the way the
  tracking switches already did, age group and usage goal sit side by side, and the reminder lead
  time is one row with its own save. Block headings inside a section are a real step larger than the
  labels beneath them, so a section can be scanned instead of read. Helper lines that only said their
  label back are no longer rendered — the "currently visible/hidden" line under every tracking
  switch, the first-day-of-the-week hint, three webhook reminder hints, and the wrapper headings
  around the account's password and recovery-code blocks. The account email, the new password and its
  confirmation, and the re-authentication field beside its destructive button each share a row with
  what they belong to. On the dashboard the journal's title carries its date on the same line and the
  quick actions sit beside the period switch. Measured at 1280 px on a fresh account: settings
  8049 → 6407 px, the journal region 1000 → 872 px, Insights 825 → 793 px.

- **The danger zone has a surface of its own.** It has never painted one: the rule read an
  undefined token, so the section rendered as bare page. It is now a quiet warm neutral in both
  themes, with the danger signal carried by the border and the heading rather than by an alarm-red
  field, and it stays where the section index points at it.

- **Insights no longer repeats itself where a section is empty.** The cycle-trend card printed
  "Not enough cycle data yet." twice — once as its caption and once as its body — and the per-section
  empty hints were panel-sized boxes. The caption now names the window only when there is a chart to
  name it for, and an empty section shows a single-line hint; the whole-page empty state, with its
  illustration, is still the one card an account with nothing logged sees.

- **The interface now has a design system instead of per-component decisions.** Semantic
  surface/ink/border/accent tokens carry both themes, type sizes follow one scale with a real
  display size, radii collapse to a card radius, a control radius and the pill, shadows to three
  elevation levels, and every transition runs on one of three durations with one shared easing.
  Sizes and corner radii shift by a pixel or two in places; a selected but disabled chip in the
  dark theme no longer paints a light-theme cream fill under light text.

- **Interface glyphs are drawn by a first-party icon set instead of emoji.** Navigation,
  section headings, toggles, quick actions, banners and the phase chip now render inline
  SVG icons drawn in one hand — one stroke weight, round corners, `currentColor`, so they
  follow the text colour and the theme. The sprite is inlined in the page, so nothing is
  fetched and the strict content security policy is unaffected. The icons are decorative
  and stay out of the accessibility tree: a screen reader no longer reads "shield",
  "hourglass" or "crystal ball" as page content, and the icons look the same on every
  platform instead of being whatever the system emoji font draws. Mood faces and symptom
  icons are logged data, not chrome, and are unchanged.

- **The usage-goal chooser now leads with "track my health".** Onboarding, the
  settings cycle section and the dashboard goal switch list the neutral default
  first, followed by the two alternative modes (avoid pregnancy, trying to
  conceive). Display order only: the default mode, what a submitted answer
  means, and skipping the onboarding question are all unchanged.

- **Automatic period fill starts off on a new account.** `SECURITY.md` has stated auto-period-fill
  as an off-by-default privacy control since the GDPR cross-reference was written, while every
  producer set it on: the column default, both account constructors, and the reset that runs inside
  clear-data. A fresh account now records only the days its owner entered, and the onboarding toggle
  is where inference gets turned on. **Existing accounts are not migrated** — no stored value is
  rewritten, so an owner who is using auto-fill keeps it; the change governs accounts created from
  here on, plus the two paths below.

- **Calmer wording around the cycle map and the temperature range.** The BBT range hint and its
  out-of-range error name whole bounds instead of padded decimals (34-43 °C, 93.2-109.4 °F), and the
  Russian estimate qualifier under the cycle map says the map follows how the cycle usually goes
  rather than naming baseline parameters.

- **The settings page reads the owner's entries once per render.** The export panel's figures — the
  selectable date range and the default window's totals — were two separate reads of every
  `daily_logs` row, each walked in Go. They now come from one read, so the slowest page in a long
  history costs half of what it did.

- **Breaking: the login, registration and password-reset rate limits are held to 30 requests per
  minute.** Each `RATE_LIMIT_{LOGIN,REGISTER,FORGOT_PASSWORD}_MAX` / `*_WINDOW` pair must now satisfy
  `MAX × 1 minute ≤ 30 × WINDOW`. The count ceiling of 100 alone still let 100 over a one-second
  window through: about 6000 bcrypt compares a minute from one address, where the ceiling now allows
  about half a compare a second. A pair above the rate is logged at boot, naming the allowed rate
  and an allowed pair (`100` with `200s`), and both halves fall back to their defaults (8 per 15
  minutes; 8 per hour for password reset). An operator who widened a credential budget by
  shortening its window gets the defaults from this release. Set a pair within the rate instead.
  The per-account login and recovery budgets read the same validated pairs. The one-second window
  floor is unchanged, and the e2e harness now runs at 100 per 200 s. Regressions:
  `TestCredentialRateLimitPairsHaveARateCeiling`,
  `TestCredentialRateLimitRefusalLeavesTheOtherPairsAlone`,
  `TestCredentialMaxCeilingIsReadOnlyThroughTheRateCheck`.

- **Breaking (API shape), JSON API:** when an identity unlink (`DELETE /api/v1/users/current/oidc/identities/:id`) or a
  data wipe (`POST /api/v1/users/current/data-wipe`) has committed but its session could not be
  re-issued, the response is now `401` with category `unauthorized` instead of `500`. The error
  key is `identity change applied sign in again` for the unlink and `data cleared sign in again`
  for the wipe, replacing `failed to create session`; both mean the change took effect and the
  client must sign in again. A refusal that changed nothing keeps its previous status and key.
  The unlink route is new in this release; for a client of v1.9.2 the break is the data wipe's
  `500`.

- **Breaking (API shape), JSON API:** when a password change (`PUT /api/v1/users/current/password`) or a two-factor
  enable/disable (`PUT`/`DELETE /api/v1/users/current/2fa`) has committed but its session could
  not be re-issued, the response is now `401` with category `unauthorized` instead of `500`. The
  error key is `password changed sign in again`, `two factor enabled sign in again`, or
  `two factor disabled sign in again`, replacing `failed to create session`; each means the change
  took effect and the client must sign in again (with the new password, for the password case). A
  refusal that changed nothing keeps its previous status and key.

**Breaking (API shape): a future-day cycle start no longer replaces the saved start.**
Marking a day after today as the cycle start, inside the period whose saved
start it would replace, is now refused with `cycle start mark on the day`
(400) whatever `replace_existing` says, and the calendar's day editor shows a
"mark it on the day" notice instead of the cycle-start button. Marking that
day once it arrives works as before, and a future mark next to any other
logged start keeps the ordinary replace confirmation.

- **`latest` now names the newest stable release, not the tip of `main`.** `ghcr.io/ovumcy/ovumcy-web:latest`
  and its Docker Hub mirror used to be rewritten by every green commit on `main`, so an operator who
  pulled the floating tag ran an unreleased build. A push to `main` now publishes only `:main` and
  `sha-<short>`; `:main` is the moving pre-release channel. `:latest` moves only when a release tag
  is published, and only for a stable `vMAJOR.MINOR.PATCH` tag that is the highest one in the
  repository: a pre-release such as `v1.10.0-rc.1`, a backported older patch, or an older tag pushed
  again never moves it. The image metadata step no longer adds `latest` on its own for any tag event,
  and the publish run asks again whether its tag is still the highest right before it writes `latest`
  on GHCR and on Docker Hub, so a higher release tagged during the build keeps the alias. It is
  written by the same promotion, after the same signature check, as every other tag of the release,
  so the signing and verification commands in `SECURITY.md` apply to it unchanged.

  A manually dispatched `promote-latest` workflow points `latest` at an existing stable release
  without a rebuild, and only at the highest stable tag: a lower tag is refused, with no override. It
  resolves the release's digest, verifies the cosign signature and the provenance attestation issued
  over that digest (pinned to the release's own workflow and tag ref), and, when the Docker Hub
  mirror lacks the release, copies it there byte for byte — the index, platform manifests, blobs and
  the release's own signature bundle, so the digest is unchanged and the copy verifies under the
  release's signer identity; nothing is re-signed. The copy is idempotent and runs on every
  dispatch, so a mirror left with the index but without the signature bundle by an earlier attempt
  is completed rather than wedged. The copy is verified on Docker Hub before any tag moves on either
  registry. Only then are the release tag (if the mirror lacked it) and `latest` written — on Docker
  Hub first and on GHCR last, so a mirror failure leaves GHCR's `latest` where it was. Docker Hub's
  rate limit is asked again within a bounded budget at each of its calls; any other refusal ends the
  step. A failed run prints where `latest` points on each registry.

  Pin a release tag in `OVUMCY_IMAGE` as before; `:main` is not for production.

- **The 10–20 day luteal window is now documented as the outlier filter it is.**
  `docs/cycle-prediction.md` no longer calls the window physiological or treats a sample outside it
  as a bad reading: it filters *inferred* lengths before they are averaged, a luteal phase at or
  below its floor is ordinary rather than a fault, and a dropped sample says nothing about the
  owner's body. The doc also states that the 14-day value left standing when too few samples
  survive is the model's constant and must not be shown as a personalised luteal phase. The filter
  itself is unchanged.

- **One rule now decides where a cycle starts, on every surface.** A cycle opens on two or more
  consecutive non-spotting period days, on a non-spotting day marked as a cycle start, or on a lone
  non-spotting period day dated today or yesterday (the period may still be running). Spotting never
  opens a cycle, even when marked, and the onboarding start is a boundary of its own. The completed-cycle
  count, cycle lengths, last period start, dashboard anchor, calendar "recorded" day, stats insights and
  the luteal-phase estimate (day save, restore and the boot recompute alike) all read it, so a single
  bleeding day after a gap no longer counts as a new cycle in the stats while the anchor and the
  next-period estimate stay on the old one.
- **A history that logs each period as one unmarked day loses those cycles.** A lone unmarked
  bleeding day older than yesterday no longer starts a cycle. If you recorded each period as a single
  day without marking it, those periods no longer count as cycles: the completed-cycle count, the cycle
  lengths and the predictions built on them shrink. To restore them, open each such day and mark it as
  a cycle start (or log the second day of that period).
- **Un-ticking the period on the onboarding day withdraws it.** Turning a period day into a
  non-period day on the onboarding start date, or deleting that day, clears that start, so it no
  longer opens a cycle and the calendar no longer paints it. When the start day has no entry (onboarding
  without auto-fill), the day editor and the dashboard's Today form show its period ticked, and
  un-ticking it in either form withdraws the start the same way. Saving a mood with the box still
  ticked, adding only a mood or a symptom to a non-period entry on that date, or a JSON save of that
  date without the period leaves the start in place. A partial save (`PATCH`) that leaves the period
  out never withdraws the start; one that states no period over the start's period day withdraws it
  like a full save.
- **Moving the last period start in Settings takes the auto-filled days along.** Onboarding with
  auto-fill writes the first days of the period; under the new rule they form a cycle start of their
  own, so moving the start left a phantom short cycle behind and kept the dashboard on the old date.
  Saving a new start now writes the new start's days the way onboarding would under your auto-fill
  setting and period length, stopping at today like onboarding's own fill, and marks the new start
  day as a period day if it was logged without one.
  It removes old days only while auto-fill is on in your stored settings, and only when the save
  corrects the old date: the old start still opens your newest cycle, and the new one is earlier or
  less than a shortest cycle (15 days) later. Even then it removes only a fill written as one cohort
  of at least two days: the run onboarding's fill wrote in one go, from the old start up to the first
  day that is missing, that you edited, or that you ticked yourself. A period day you ticked by hand is
  never deleted, whatever the auto-fill setting or period length was when you ticked it, and a period
  length of 1 is never cleared. Installs onboarded before this release keep their old days on a move,
  as they did before. Otherwise the old days stay recorded. Clearing the start moves nothing.
- **The pregnancy pause lifts on a cycle start the rule counts.** A positive test pauses predictions
  until a cycle starts after it. "A cycle starts" is now the same rule as everywhere else, without
  the today-or-yesterday allowance: a marked start or an unmarked two-day bleed after the test lifts
  the pause, while a single bleeding day (even today's), a spotting day or an uncertain mark does not.
  A start on the day of the positive test still keeps the pause.
- **The JSON export carries the onboarding start, and the CSV marks it.** The JSON export gains an
  optional top-level `last_period_start` (`YYYY-MM-DD`), absent when the account has none and also
  absent when the date lies outside a requested export range; the CSV marks `Cycle start` on that date,
  adding a bare row when no day was logged on it, under the same range rule. Restore reads the field
  and sets it only on an account that holds none; an export without it still imports. The field is
  additive: `required` in the schema is unchanged.

- The period-off question now travels with a stable locale key (`X-Ovumcy-Period-Off-Confirm-Key`
  beside the question header, `data-period-off-confirm-key` beside the toggle's question), and the
  component that renders it is parsed once at startup instead of being taken from an arbitrary page
  template.

- **Un-marking a period day now says what else it changes.** Turning the period off on the day a
  cycle starts used to move that start, and with automatic period fill also clear the period from
  the following days, without a word — only the statistics shifted. The dashboard and the
  calendar's day editor now ask first, naming the effect ("moves the start of your cycle", "clears
  the period from 4 following days") and that your statistics will be recalculated. Cancel keeps
  the day as it was; an un-mark that changes nothing beyond the day itself asks nothing.

- **The privacy notice names the calendar feed among the paths data can leave by.** The
  Third Parties section of `/privacy` enumerated two owner-enabled integrations — webhook
  reminders and identity-provider sign-in — while the security documents count the owner-armed
  `.ics` calendar feed as one of exactly two standing paths that carry health-derived data to a
  third party: the calendar app the owner adds it to (often a hosted third-party service) polls
  their predicted period and ovulation days. The notice now names the feed with that consequence
  and the rotate/revoke remedy — in the same period-and-ovulation terms the settings page uses —
  and the Predictions section's integration example mentions it beside webhook reminders, in all
  six locales. Notice copy only; no behaviour changes.

- **Docs: the trusted address for a proxy outside the stack.** The self-hosting
  guide and `.env.example` now say which address `TRUSTED_PROXIES` must hold
  when the reverse proxy runs outside the app's compose stack: the address its
  requests arrive from inside the container (for a proxy on the Docker host,
  the bridge gateway), not the host's LAN IP. Trusting the wrong address made
  every sign-in fail with "Forbidden" and `reason="invalid referer"`.

- **README restructured around getting started.** Quick Start now follows the intro instead of
  sitting behind ~250 lines of prose; the badge wall is cut to the signals a reader can act on; the
  six screenshots sit in a two-column grid; image verification is collapsed under Quick Start; and
  the configuration block, upgrade note, OIDC and rate-limit detail defer to `.env.example`,
  `docs/self-hosted.md`, `docs/oidc.md` and `SECURITY.md`, where each already lives in full. No
  contract text changed: the release-tag, Go-version and Docker Hub sites the guards read, the
  unsigned-digest explanation the publish workflow points at, and the CLI reference stay.

- **Breaking (API shape): `GET /api/v1/stats/overview` no longer reports `fertile` as a
  `current_phase`.** v1.9.2 returned `current_phase: "fertile"` on fertile-window days. From
  2.0.0 the phase is one of `menstrual`, `follicular`, `ovulation`, `luteal` or `unknown`, and
  whether today falls inside the estimated fertile window is the separate field
  `current_fertility` (`fertile`, `outside_estimated_window` or `unknown`). A client that
  switches on `current_phase == "fertile"` never matches again; read `current_fertility`
  instead. (Added to this release's notes after it was published: the change shipped in 2.0.0
  but was listed without a **Breaking** marker.)

- **Breaking (API shape): `GET /api/v1/stats/overview` now publishes an explicit payload, and it
  withholds every prediction the app itself withholds.** The endpoint used to serialize the internal
  statistics object as it stood, so the ovulation date, the fertile window and the next-period
  estimate went out over JSON even in the states where Ovumcy has decided it must not show them — an
  unpredictable-cycle account, an active pregnancy pause, a cycle running more than a week past its
  own reference length, or an account with no completed cycle yet, where the fertility figures come
  from the onboarding cycle-length setting rather than from anything recorded. The pages and the
  calendar feed already withheld them; the JSON view did not, and a suppression one surface stands
  in front of is not a suppression.

  What changes for a consumer of that endpoint:

  - Dates are calendar days (`YYYY-MM-DD`), no longer timestamps with a time and an offset. A date
    that is withheld or unknown is `null` rather than a zero date.
  - A new `suppression` object says whether predictions are withheld (`predictions`), whether the
    fertility half specifically is (`fertility`), and why (`reasons`: `unpredictable_cycle`,
    `pregnancy_pause`, `cycle_overdue`, `awaiting_first_cycle`). A withheld date is a decision the
    payload names, not a gap to guess at.
  - `disclaimer` carries the medical-safety line in the account's language, with `disclaimer_key`
    beside it for clients that would rather branch on a stable key than on translated text.
  - The field set is now fixed and documented in `docs/openapi.yaml`, which lists every field
    instead of declaring the payload open-ended. A field added to the internal object no longer
    reaches the API on its own.

  Recorded history — the observed cycle lengths, the last period start, the current cycle day — is
  published in every state, exactly as before: it is fact rather than projection, and nothing
  suppresses it.

- **Breaking (API shape): `POST /api/v1/password-resets` requires the account password alongside the
  recovery code, and now says so to a client that omits it.** The password operand shipped earlier as
  a security fix — a recovery code stands in for the second factor, never for the first, so a single
  captured secret could no longer take over an account — but the stable `/api/v1` surface gained a
  required member without a major version, and a caller still sending the v1.9.2 body of
  `(email, recovery_code)` was answered "invalid recovery code". The code was not invalid; the
  contract had moved, and the answer sent integrators looking for a fault in the wrong place. A JSON
  body that carries no `password` member at all is now refused with its own key,
  `recovery reset requires the account password`, decided before any account is read. A wrong or
  empty password is untouched: it stays inside the deliberately uniform credential refusal, which
  reveals nothing about whether the address, the code or the password was the wrong one. Form
  submissions keep that uniform refusal too — in form encoding an omitted field and an empty one are
  the same thing on the wire, so the question cannot honestly be asked there.

- **Breaking (API shape): onboarding step 2 no longer accepts `age_group`, and refuses a body that
  still sends it.** The step stopped asking for the age bracket; the column is written by
  `PATCH /api/v1/users/current/cycle` alone. Until now a client submitting the v1.9.2 body received
  `200` with the field quietly dropped, which is indistinguishable from a save that worked —
  the one answer a removed field must never give. `POST /api/v1/onboarding/steps/2` now answers
  `400` with the key `onboarding does not accept an age group`, on both the JSON and the form
  transport, and writes nothing. Clients that stopped sending the field are unaffected; those that
  still send it should drop it and set the bracket from the cycle-settings endpoint or Settings.

The image publish workflow now reads the SBOM attestation back off the pushed
digest before any public tag is written: every platform the release ships must
carry an SPDX document with packages, or the run fails with the image still
untagged. Until now CI verified the signature and the build provenance but
never the SBOM that README.md tells operators to inspect.

The dashboard's cycle ring becomes a cycle ribbon: one cell per day of the
cycle, carrying the phase along the band, the fertile window as a strip under
it with the ovulation day as its peak, a mark on every day that holds an entry,
and the days the next period may start on as a graded tail the ring had no way
to draw. Every suppression the status header obeys the ribbon obeys with it, so
unpredictable mode, a pregnancy pause, an overdue cycle or stale data leave the
band undrawn rather than guessed at.

- **An unparseable `AUDIT_LOG_ENABLED` or `OIDC_ENABLED` now refuses the boot.** Both join
  `COOKIE_SECURE`, `HSTS_ENABLED`, `TRUST_PROXY_ENABLED` and `WEBHOOK_BLOCK_PRIVATE_ADDRESSES`: a
  typo such as `AUDIT_LOG_ENABLED=ture` used to start the instance with the audit stream off, and
  `OIDC_ENABLED=ture` with OIDC off — which also skipped every OIDC check and, under
  `OIDC_LOGIN_MODE=oidc_only`, quietly brought password sign-in back — each with only one warning
  in the boot log. Both now exit with an error naming the key and the value. The accepted spellings
  are unchanged (`1`/`true`/`yes`/`on`, `0`/`false`/`no`/`off`), and an unset key still means off.
  **Upgrade note:** a value that only produced that warning before now stops the start — check the
  boot log of the running version for `invalid AUDIT_LOG_ENABLED` or `invalid OIDC_ENABLED` before
  upgrading. A common source is a quoted value passed through `docker run --env-file`, which keeps
  the quotes (`"false"`).

- **The API stability policy now matches how v2.0.0 ships.** `CONTRIBUTING.md` and the
  `Stability` block of `docs/openapi.yaml` used to promise that a breaking change would arrive
  under a parallel `/api/v2/*` prefix. There is no `/api/v2/*` and none is planned: a breaking
  change ships in a new major release on the same `/api/v1/*` prefix, with a **Breaking** entry
  in this changelog naming what a client has to change. A correction that makes the spec describe
  what the server already does is listed under **Fixed**, not as a break. Every `/api/v1/*` break
  v2.0.0 makes in place now carries a **Breaking** marker in this changelog, and `info.version` of
  the spec is now `2.0.0`.

- **The login, registration, forgot-password, 2FA challenge and password-reset redeem rate-limit
  windows now start at one minute.** `RATE_LIMIT_{LOGIN,REGISTER,FORGOT_PASSWORD,TOTP_CHALLENGE,PASSWORD_RESET_REDEEM}_WINDOW`
  used to accept anything from one second; a value below `1m` is now out of range like any other
  out-of-range `RATE_LIMIT_*` value. A refused window (below `1m`, above a day, unparseable) is
logged at boot and the pair falls back to BOTH
  defaults (8 per 15 minutes; 8 per hour for forgot-password), as it does above the per-minute
  rate ceiling, which is unchanged. An operator who ran one of these five windows at seconds
  gets the default pair from this release; set `1m` or longer instead. Every other
  window (logout, API, calendar page and feed) keeps its one-second floor, and both logout rows
  are unchanged. Regression: `TestCredentialRateLimitWindowsHaveAOneMinuteFloor`.

Finishing setup now logs the start date you entered as a real cycle start: a
period day carrying the cycle-start mark, whether or not auto period fill is on.
The calendar, the CSV and JSON exports (`Period = Yes`, an entry in `entries`)
and the observed-cycle count now agree on that day. Accounts set up earlier are
not changed.

Correcting the start date in Settings now moves that logged mark too, with auto
period fill on or off: the old date loses the cycle start (a day left with
nothing else is removed, a day with notes or other entries keeps them) and the
new date becomes a period day carrying it, unless it is still in the future.
Marking another day as the cycle start in the same period now also moves the
stored start when the replaced mark was the stored one and the new day is not
in the future.

- **A refused SSO sign-in points existing account holders to Settings.** The message now says
  that anyone who already has an account can sign in with their usual method and link the
  provider from Settings. It reads the same whether or not the email matches an account.

- **API:** `GET /api/v1/stats/overview`: `suppression.reasons` can carry a new value,
  `cycle_past_projection_length`, on the days described above (`cycle_data_stale` stays false there).
  Clients that reject unknown enum values need to accept it.

- **Upgrading to 2.0.0 from v1.9.x — read before the first start.** The full list is in
  [docs/self-hosted.md → Upgrading to 2.0.0](docs/self-hosted.md#upgrading-to-200).
  - Every calendar feed armed under v1.9.2 or earlier is disarmed on the first start: those
    tokens predate the keyed verifier and nothing records which `SECRET_KEY` minted them.
    Owners generate a new subscribe URL from `Settings → Calendar feed`.
  - Replace the compose file with this release's. Without the `ovumcy_fence` volume the start
    is refused; `CALENDAR_FEED_FENCE_OPTOUT=true` boots anyway, disarming every feed on every
    start and leaving a restored backup free to bring back sessions and webhook endpoints.
  - A `.env` v1.9.2 accepted can stop the start: `COOKIE_SECURE`, `HSTS_ENABLED`,
    `TRUST_PROXY_ENABLED`, `WEBHOOK_BLOCK_PRIVATE_ADDRESSES`, `AUDIT_LOG_ENABLED` and
    `OIDC_ENABLED` refuse an unreadable value instead of using the default, `TRUSTED_PROXIES`
    entries must be valid and canonical, and `CALENDAR_FEED_FENCE_PATH` must be absolute.
  - `RATE_LIMIT_*` values outside the new ranges are logged and replaced by the defaults.
  - JSON clients of `GET /api/v1/stats/overview` that switch on `current_phase == "fertile"`
    read `current_fertility` instead: `fertile` is no longer a phase.
  - Downgrading to v1.9.x has new caveats (stale auto-fill marks, the feed's last-polled date):
    [Downgrade Caveats](docs/self-hosted.md#downgrade-caveats).

- **Self-hosting names two fence setups it does not support.** Two or more server replicas against
  one database, and an operator command whose fence advance interleaves with the server's own, can
  each leave the restore fence's two halves disagreeing, which the next start reads as a restore.
  `docs/self-hosted.md` now says so and asks for one server process per database.

- **Concurrent sign-outs share one restore-fence write.** Every sign-out records itself in the
  fence; sign-outs that arrive while one is being recorded now wait for a single shared write
  instead of queuing one synced file write each.

- **Choosing "avoid pregnancy" now shows a contraception warning.** Onboarding and the settings
  cycle section show, under the goal options and only while that goal is selected, that Ovumcy is
  not a method of contraception, that its dates are calendar estimates that are less reliable after
  giving birth, while breastfeeding, after starting or stopping hormonal contraception and with
  medication that affects the cycle, and that avoiding pregnancy calls for a contraceptive method and
  a healthcare professional. It is rendered for the saved goal and follows the choice before a save.
- **The calendar's ovulation marker is labelled as an estimate.** The legend and the dot's tooltip
  now read "Estimated ovulation (dash: no temperature shift yet)" and "Ovulation (estimate)": the
  solid dot is also drawn for projected cycles, for phases derived from logged period starts, and
  for the current cycle when temperature tracking is off, so it never meant a confirmed day on its
  own.
- **The perimenopause hint on the stats page states the criterion it refers to**: a difference of 7
  or more days in the length of two consecutive cycles that happens again within the next 10
  cycles, rather than an undefined "persistent" difference.

- **`GET /api/v1/stats/overview` reports today's fertility status as a statement about an estimate,
  and says what it was read against.** `current_fertility` answers `fertile`,
  `outside_estimated_window` or `unknown`. A day outside the window is reported as outside an
  estimated window, never as a day on which conception is not possible: an estimated window often
  misses the real fertile days, regular cycles included, so `outside_estimated_window` is not a safe
  day and not a method of contraception — after a confirmed thermal shift too, where it only says
  that today lies outside the window derived from that shift.

  Two fields sit beside it. `fertility_basis` names the window the status was read against —
  `projection` for the one rolled forward from the cycle history, `confirmed` for the one derived
  from a shift the owner's own temperatures confirm — and is `null` exactly when the status is
  `unknown`. `cycle_data_stale` reports the verdict the dashboard and the statistics page show as
  their out-of-date notice: the running cycle has passed the account's reference cycle length. While
  it is true the endpoint answers `unknown` for both the phase and the fertility status, as both
  pages do; the projected dates are still reported unless a suppression signal withholds them.

- **In irregular-cycle mode the current cycle's fertile window covers the whole ovulation range.**
  An account with irregular-cycle mode on and three or more completed cycles is shown a range of
  possible ovulation days, from its shortest recent cycle to its longest, but the fertile window and
  the fertility status were built from the six days around a single median estimate — so the status
  called days "outside the window" that the same dashboard named as possible ovulation days. The
  current cycle's window now runs from five days before the shortest cycle's ovulation to the
  longest cycle's ovulation, on the dashboard, the statistics page, the calendar and the JSON
  overview; the cycles the calendar projects after it keep the median window. The published
  ovulation date stays the median estimate, so on the days between it and the window's end no phase
  is named — the ovulation may still be ahead of the owner — unless bleeding is logged that day,
  which stays menstrual. The window can run past the projected
  next period; the calendar marks those days as both.

- **The `osv-scanner.toml` suppression of GO-2026-5932 now expires.** It carries an `ignoreUntil`
  date, so the unreachable `golang.org/x/crypto/openpgp` finding is re-judged instead of staying
  silenced indefinitely.

- **A request refused by the CSRF check now answers under its own error key, `csrf_refused`.**
  Until now it carried the shared `forbidden` key, the same one the role guards use, so a client
  could not tell a stale token from an account that may not do something. The status (403) and the
  `error_detail.category` (`forbidden`) are unchanged, so a client that branches on those is
  unaffected; a client that matched the `forbidden` key to detect a CSRF refusal must match
  `csrf_refused` instead. The HTMX and page-form refusals carry the new stable key as
  `data-flash-key="common.error.csrf_refused"`, with copy in all six languages that tells the owner
  to try again first and to reload the page only if the retry is refused too: the page refreshes its
  token after the refusal, but a form sent without JavaScript does not, and its retry from a page
  left open is refused again. `docs/openapi.yaml` names the new key on every operation that
  documents the CSRF refusal.
- **After a CSRF refusal the page re-reads its token on every htmx control, not only the day
  writes**, so the "try again" the notice offers can succeed. A 403 whose body names no key, such as
  a proxy's own error page, is treated the same way and shows the refusal copy rather than "check
  your connection". The calendar editor's delete and cycle-start controls, which have no status
  area, now say so in a toast instead of staying silent.
- **The token refresh reads the privacy page, not the page the owner is on**, so a refusal on a
  one-time surface (the two-factor setup page, the recovery code) never re-fetches that surface and
  spends or re-mints what it shows.

- **Fiber 3.6.1.** The web framework moves from 3.5.0 to 3.6.1 (fasthttp 1.76.0). This release
  normalizes a request path before routing it — `/x/../days` now reaches `/days` — and the
  onboarding gate already decides on the path the router dispatches on, so an account still
  onboarding stays held at onboarding whichever spelling it sends.

- **`ovumcy webhook set` reads `OVUMCY_WEBHOOK_URL` only when `--url-env` is given.** An exported
  variable used to replace the stored endpoint on any edit, even one that only flipped a toggle.
  Scripts that set the endpoint through the variable must add `--url-env`. `--url-env`,
  `--url-stdin` and `--clear-url` are mutually exclusive.
- **`ovumcy webhook set --clear-url` now also turns delivery off** unless `--enabled=true` is
  given (which is refused, as a webhook with no endpoint). Clearing used to fail while delivery
  was on, so withdrawing an endpoint took two commands.

- **A webhook URL must now name a host and, if it carries a port, an in-range one.** `http://:8080/`
  parses with a non-empty authority but an empty hostname, so it passed the old "has a host" check —
  and Go's dialer reads an empty host as the unspecified address, i.e. the machine running Ovumcy. A
  port outside `1..65535` was likewise accepted and left for the transport to complain about. Both
  are now refused when the endpoint is saved **and** again before delivery, because save-time
  validation never revisits a URL already stored. An endpoint of this shape saved earlier will start
  failing with the usual invalid-URL message; re-save it with a hostname.

### Removed

- **The "your prediction shows a range" explainer.** The prediction has rendered
  as a range for a while and the range now names itself, so the sentence under it
  only restated the surface. Owners with an irregular cycle keep their own
  explainer.

- **The "additional terms under section 7" paragraph.** It required the source
  of a modified network service to be made available to that service's users,
  which is section 13 of the license and applies on its own. As a section 7
  term it was also outside the closed list that section admits, so a recipient
  was free to strike it; keeping it only made the license text non-canonical.

### Fixed

- **A route's rate-limit budget now covers every spelling of its path the router
  accepts.** The five limiters scoped to a single endpoint — sign-in, sign-out,
  registration, forgot-password and the language switch — decided whether a
  request was theirs by comparing the path as raw bytes, while the router matches
  a normalized copy of it: routing is neither case-sensitive nor strict about a
  trailing slash, so `POST /LANG` and `POST /lang/` reached the language handler
  with the limiter's own filter waving them through uncounted.

  What that cost differed per route. The language switch is the one
  unauthenticated route outside `/api` that reads a request body, so it has no
  second budget behind it and had no cap at all on those spellings. Its four
  siblings fell through to the `/api` catch-all — 300 requests per minute against
  their own 8 per 15 minutes — with the per-account `AuthAttemptPolicy` still
  capping failed credentials underneath, so those were a loosened budget rather
  than an absent one.

  The filter now compares the path in the router's own normalization. It stays an
  exact match rather than a prefix one, so `POST /api/v1/sessions/2fa-challenge`
  still does not spend the sign-in budget. Canonical paths keep the same budgets,
  the same response keys and the same log lines.

- **The documented Postgres restore now actually restores.** The runbook's
  restore command — a plain dump piped into `psql` — succeeded loudly and did
  nothing whenever the target database still held its schema. `pg_dump` writes
  `CREATE TABLE` then `COPY` and carries no `DROP`, so every `CREATE TABLE` hit
  an existing relation, and `COPY` (one all-or-nothing statement per table) only
  ever landed in a table that was still empty; `psql` without
  `-v ON_ERROR_STOP=1` ran past all of it and exited `0`. The outcome therefore
  depended on the state of the target and was indistinguishable from outside:
  into an empty database it restored everything, into a partially lost one it
  restored nothing and left the gap, and into an intact or drifted one it
  changed nothing at all — leaving the operator reading the generation of data
  they were trying to replace, on a stack reporting healthy.

  The procedure now drops and recreates the `public` schema first — the
  counterpart of `docker volume rm ovumcy_data` + `docker volume create` on the
  SQLite path — and runs both halves under `-v ON_ERROR_STOP=1`, which turns the
  silent no-op into a non-zero exit. The drop is destructive, so the runbook
  states the rollback-backup precaution the SQLite path already carried.

  Three verification claims moved with it, because two of them ended at "the app
  is up": the backup-contract bullet and the named-volume restore step now route
  to *Post-Restore Verification* instead of standing on `/readyz` and a page
  load, and that checklist gained a step for the failure the old step 4 could
  not see — data present, but the same data as before the restore. Restore
  drills are now specified against a populated stack: an empty target is the one
  case where a broken restore procedure still looks correct.

- **`docs/openapi.yaml` described a recovery code almost no real code could
  pass.** `ForgotPasswordRequest.recovery_code` declared
  `pattern: "^OVUM-[0-9A-F]{4}-[0-9A-F]{4}-[0-9A-F]{4}$"` — hex digits only —
  while `GenerateRecoveryCode` draws each of the 12 random characters from a
  32-symbol Crockford-style alphabet (`A`–`Z` without the visually-ambiguous
  `I`/`O`, plus `2`–`9`), of which only 14 symbols are also hex digits: a real
  code matched the documented pattern with probability `(14/32)^12 ≈ 4.9e-5`
  — roughly 1 in 20,000. Recovery is the last path back into a locked-out
  owner's account, so a client that validates a request against the published
  spec before sending it — the same failure class as the `2fa-challenge`
  content-type gap documented elsewhere in this list — could not complete it
  for all but a vanishing fraction of real codes.

  The pattern now documents the class the server actually accepts
  (`ValidateRecoveryCodeFormat`, `[A-Z0-9]`) rather than the hex subset, or
  the generator's own narrower alphabet — the server deliberately accepts a
  wider class than it generates, so pinning the pattern to the generator's
  exact alphabet would have reproduced the same defect in milder form.
  `TestOpenAPIRecoveryCodePatternAcceptsGeneratedCodes` pins the documented
  pattern against both real generated codes and the server validator's
  accepted character class, with a proof-on-defect regression against the
  original hex-only pattern.

  The same sweep, run across every `pattern`, `enum`, and numeric bound in
  the spec, found three more entries narrower than what the server accepts:
  the language enum on `InterfaceSettings.language` and `POST /lang` was
  missing `it`, a fully supported locale; `POST /lang` additionally
  documented its form field as `language`, while the handler reads `lang` —
  so a client sending the documented field name could not set the language
  to any value, not only `it`; and `OnboardingStep2Request.cycle_length`
  declared `14`–`60` where the server accepts `15`–`90`, understating
  onboarding's real cycle-length range. All three are description-only
  fixes — no application behavior changed.

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

- **Five transport error keys reached the browser as raw machine text.** The
  app-wide envelope maps every rejection to a stable key and, for a browser flow,
  renders localized copy beside it — but the key-to-copy table only ever covered
  the keys introduced alongside it. Five keys the same table owns had no entry,
  and an unmapped key is rendered as itself: `request_timeout` (a request that
  outlived its 60-second budget), `unauthorized`, `not found`,
  `request_too_large` and `request_headers_too_large`. An HTMX flow that ran out
  of budget was therefore shown the literal string `request_timeout`, in all six
  languages, next to refusals on the same page that answered in a sentence.

  All five now resolve. `404` and `413` reuse the copy their own surfaces already
  show, so one status keeps one phrasing whichever layer produced it; `401`,
  `431` and the request budget's `503` ship new copy in all six locales. The JSON
  envelope is untouched — `error` and `error_detail.key` carry the same machine
  keys as before. What changes for a browser client is the visible text and the
  `data-flash-key` hook beside it, which now carries the resolved translation key
  (for example `common.error.request_too_large` in place of `request_too_large`),
  matching what every already-mapped rejection has always emitted.

- **The request budget is now pinned to being switched on, not only to
  working.** Both regressions behind the 60-second budget assembled their own
  Fiber app and mounted the middleware themselves, so they described how the
  guard behaves once installed and said nothing about it being installed:
  deleting the registration line left the entire API and command test suites
  green, with every request back on an unbounded `context.Background()`. That is
  the same defect shape the budget work called out one floor down — proving the
  middleware ran proves nothing if it never runs in the shipped app. The
  registration is now covered by a regression that drives the real assembled
  application and reads the deadline **inside the repository call**, closing the
  second half of the gap as well: the existing tests observed the context at the
  handler, while the unbounded wait this budget exists to prevent happens at the
  database. No runtime behaviour changes.

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
- **A request that runs out of its budget no longer hands back the response it
  is refusing.** Every request is bounded by a 60-second budget, and one that
  outlives it answers `503 request_timeout` — a status that says nothing
  happened and the caller may retry. That answer replaced the status and the
  body of whatever the handler had built, but not its headers, so a sign-in or
  an erasure step-up whose work had already committed still delivered its
  `Set-Cookie` and its `Location` next to the 503. A client acting on the status
  would retry while already holding the session the status said it never issued.
  The response is now rolled back to the state it was in before the handler ran,
  so nothing the handler set survives the refusal. The headers the app puts on
  every response — the browser-hardening set and the CSRF cookie — are written
  before that point and are unaffected.
- **The API specification no longer claims an error format the calendar feed
  does not use.** `docs/openapi.yaml` stated that every rejection the server
  emits carries the `{error, error_detail}` envelope. The read-only `.ics`
  feed's not-found path does not and should not: it answers the fixed body
  `Not Found` for a malformed token, an unknown selector, a wrong verifier and
  a disabled feed alike, because it is a public cookieless endpoint read by
  calendar applications, and the envelope negotiates its shape from the
  caller's headers and carries localized copy. `/healthz` and `/readyz`
  already held the same carve-out for their fixed one-word body. The spec now
  names all three fixed-body exemptions instead of overstating the rule — the
  feed's per-IP `429` is explicitly not among them and answers the shared
  envelope like every other route — and a route sweep over the real
  application fails if a fourth appears, or if a declared one quietly starts
  using the envelope after all. No response changed.
- **The 60-second request budget no longer justifies itself with a timeout that
  does not bound it.** The comment on `RequestBudget` derived the value from the
  server's `WriteTimeout`, on the reasoning that a handler outliving the write
  timeout could not deliver its response anyway. It can: fasthttp arms the write
  deadline only after the handler returns, so `WriteTimeout` caps writing a
  finished response to the socket and never how long the handler took — which is
  exactly why abandoned requests answered with latencies past fourteen minutes
  under that same 60-second `WriteTimeout`, and why an in-process budget had to
  exist at all. The value is unchanged and still defensible: it is sized by the
  widest legitimate request, a full-size JSON restore. What changed is the
  reasoning, which would otherwise have survived into the next change to
  `WriteTimeout` and dragged the budget along with it. The two constants are now
  recorded as independent, and the fasthttp behaviour the reasoning rests on is
  pinned by a test that fails on an upgrade moving the deadline.
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

- **An erasure confirmed through SSO now names the owner it erased.** The audit
  stream takes the actor from the request context, and only the authenticated-
  route middleware published it there. The two step-up completion handlers
  resolve their session themselves — `/auth/oidc/callback` cannot carry that
  middleware, because ordinary sign-in has to work for a visitor with no session
  — so every event they emitted was unattributed, including the health-data
  mutation recording a cleared diary or a deleted account. On an instance
  hosting more than one owner, the two most destructive operations in the
  product left a log line that did not say whose data they touched. The session
  resolver now publishes the actor for the whole request, which covers both
  step-up purposes and any handler that resolves a session the same way later.
- **The API specification described error responses the server never sends.**
  Nineteen operations in [docs/openapi.yaml](docs/openapi.yaml) declared `422
  Unprocessable Entity` for a rejected payload, while every validation refusal
  the app produces is a `400` — there is no `422` branch anywhere in the server.
  A client written against the document was therefore branching on a status it
  could never receive, and met the real refusal in whichever branch it kept for
  something else. The document now says `400`. Nothing in the server changed:
  moving the app to `422` would have broken every client already reading `400`,
  in exchange for a distinction the response envelope already carries in
  `error_detail.category`, where `validation` is its own stable value.

  The published `error_detail.category` enum had the mirror-image defect, and a
  worse one — it was **missing** a value the server really sends. The enum is
  documented as closed and is what the document tells clients to branch on, yet
  it omitted `too_large`, which every request refused by the 16 MiB body limit
  and every one refused by the oversized request head carries. A client that
  trusted the enum did not merely wait for something that could not arrive; it
  met something it had been told could not exist.

  Decision (2026-07-28): `413` legitimately carries **two** categories, and the
  document now says so instead of picking one. `category` names the reason, not
  the status: a body past the byte limit is refused before the request reaches
  an operation, with nothing about its content examined, and carries
  `too_large`; a restore payload that parses cleanly but declares more than
  20 000 entries has failed a content rule after parsing, and carries
  `validation`. Collapsing them would tell a client "send fewer bytes" and
  "split this export into several restores" in the same word. The two keys that
  `413` carries were already documented and deliberate; the two categories are
  the same distinction one level up.

  Sweeping the remaining statuses the same way closed the opposite gap. The
  `409` that a duplicate symptom name — or a cycle start that would overwrite an
  existing one — really answers with was documented nowhere in the file, nor was
  the `404` for a symptom that does not exist.

  The examples were the same story in miniature: fourteen of them showed a bare
  `{ "error": ... }` that the `ApiError` schema above them forbids, and three
  key strings — `too many attempts`, `internal error`, and the
  `auth.invalid_credentials` that headed the schema itself — belong to no
  response the server can produce (the real ones are `too many requests`,
  `internal_error` and `invalid credentials`). Every example in the document now
  carries a complete envelope whose key, category and target are the ones a
  client will actually receive.

- **Onboarding and three settings endpoints now appear in the audit stream.**
  With `AUDIT_LOG_ENABLED=true`, both onboarding steps, onboarding completion,
  the reminder window, the timezone and the display name wrote to the account
  and recorded nothing at all. Onboarding was the consequential gap: it writes
  the same `users` columns the settings cycle form writes, so an operator asking
  "were the cycle settings changed?" got a correct answer when the owner used
  Settings and a wrong one when the same result came from onboarding. Those
  actions now carry `domain="health_data"` with the same `target="cycle_settings"`
  the settings form carries (completion carries `target="day_entry"` for the
  period days it seeds), so one filter selects the change whichever surface
  produced it.

  The reminder window and the timezone join the same domain because a filter on
  it is a statement about effect, not about the value submitted: the first
  decides when a prediction is announced, including off-host through the
  webhook, and the second decides which calendar day a predicted period falls
  on. The display name is audited separately under `domain="account"` — nothing
  in the cycle math reads it, and putting it in the health-data domain would
  make that filter mean less than it says. No audit line carries the submitted
  value. See [docs/security/logging.md](docs/security/logging.md).

- **Rate-limit refusals answer in the application's own format, like every other
  rejection.** A `429` from an edge limiter built a body of its own — the stable
  key and `retry_after_seconds`, with no `error_detail` — so the same refusal
  reached a client in two different shapes depending on which layer produced it:
  the limiter's stripped body on `POST /api/v1/sessions`, and the full envelope
  from the per-account attempt budget behind the very same endpoint. Every
  limiter now answers the shared envelope, and `retry_after_seconds` rides along
  inside it as an extension member, so a client that reads it keeps working
  while one that parses `error_detail` no longer needs a second parse path.
  Stable keys are unchanged.

  Two browser-facing halves went with it. A refused language switch used to
  return the JSON body to a full-page navigation — `POST /lang` is the only
  public form with no HTMX and no JavaScript behind it, so the browser simply
  displayed the raw JSON; it now renders the localized status fragment. And
  because the limiters deliberately sit ahead of the language middleware (a cap
  has to count requests that never reach a handler), a rate-limited HTMX flow
  had no locale catalogue to translate against and displayed its own machine key
  — `too_many_login_attempts` — as the message in all six languages. Refusals
  now resolve the language themselves, and the two limiter keys that had no
  copy at all, sign-out and registration, have it in every locale. The calendar
  feed's previously bodyless `429` joins the same envelope.

- **The audit stream can now answer what left the instance, not only what
  changed inside it.** Tagging the erasure actions gave an operator a filter for
  "was tracked data changed or destroyed?" — and nothing at all for the question
  that matters at least as much on a health tracker: did any of it leave? The
  three export endpoints logged an action name and a format through the plain
  security-event path, carrying no domain and no target, so they were invisible
  to a domain filter; the shared prologue lost even the format, which made a
  refused CSV download indistinguishable from a refused JSON one. The two
  one-time secret reveals — the `.ics` subscribe URL and a recovery code —
  emitted no audit event at all, so the moment a standing capability over an
  owner's data was handed out left no trace.

  Exports and reveals now declare a typed egress kind, the read-side counterpart
  of the mutation kind, and every branch logs through it. Six surfaces are
  covered: the CSV, JSON and summary exports (success, refusal and storage
  failure alike, each attributed to its format), the calendar-feed reveal page,
  and both surfaces that display a recovery code. Each line records the *fact*
  of the disclosure — no export payload, subscribe URL, feed token, or recovery
  code ever enters it.

  Egress lines carry a **separate** `domain="health_egress"` rather than joining
  `domain="health_data"`. Existing filters are unaffected in both directions: the
  `data.export` action string is unchanged, and `domain="health_data"` keeps
  selecting exactly the data-changing actions it selected before, so an erasure
  review does not start collecting every routine download. A review that wants
  both classes at once matches the shared prefix in one clause (`domain="health_`).
  Polling of the calendar feed is deliberately **not** audited — the reasoning is
  recorded under *Calendar-feed polling is not audited* in
  [docs/security/known-disclosures.md](docs/security/known-disclosures.md). Field
  reference: [docs/security/logging.md](docs/security/logging.md#logging-policy).

- **The restore paths that still ended at "it started" now route to the
  verification that can fail.** *Post-Restore Verification* says in its own text
  that every up-signal above step 4 stays green on an empty database, and the
  backup-contract bullet and the named-volume restore step were corrected to
  defer to it — but two other passages still let a restore be signed off without
  reading any data back. The Postgres runbook ended on "prints no `ERROR:`
  lines, exits `0`", which a truncated or empty dump satisfies exactly, because
  it replays into a freshly dropped schema without an error and leaves the stack
  healthy and blank. The safe-upgrade procedure's rollback step said to restore
  from backup with the procedure's own confirmation — the container healthcheck
  and one page load — sitting above it as steps 4-5.

  Both now point at [docs/self-hosted.md](docs/self-hosted.md) *Post-Restore
  Verification* and name why the quick signals cannot close a restore out. The
  checklist itself is unchanged and stays the single home of the procedure.

An OIDC step-up (erasure, local-password enrollment, identity linking) now
tells apart a provider that returned no `auth_time` at all from a sign-in that
was merely too old. The first case used to render "too old … try again", which
can never succeed on a provider that omits the claim, and both cases reached
the audit stream under one reason, so an operator could not separate a
non-conforming identity provider from a slow owner. The missing-claim refusal
now has its own copy in all six languages and its own audit reason
(`reason="oidc reauth auth_time missing"`); the too-old refusal is unchanged.

Resetting a password, regenerating the recovery code, and enrolling a local
password after an OIDC step-up each rotate the recovery code and revoke every
session. They used to commit the new code first and only then issue the new
session and stage the code's one-time reveal, so a failure in between left the
account with a code nobody had been shown and the previous one gone. The
session and the reveal are now sealed inside the same database transaction,
before it commits; if either cannot be sealed, nothing is written — the
password, the previous code, existing sessions and the calendar feed stay as
they were, and the action can simply be retried. The session re-issued after
the rotation now carries the version read back from the database rather than
one computed from the account as loaded before the write.

- **Breaking (API shape):** `PUT /api/v1/users/current/2fa` on an account that already has two-factor authentication on now answers 409 `totp already enabled` instead of 401 `totp session expired`, and `DELETE /api/v1/users/current/2fa` with a wrong password answers the error key `invalid password` instead of `invalid credentials` (still 401). In the browser, regenerating the recovery code, changing the password and turning two-factor authentication on or off now answer at the form: a wrong password shows its message beside the form and returns focus to the emptied password field, a password change empties the password fields and confirms once, and the two-factor panel refreshes to its new state, points to the recovery code after enabling and asks for confirmation before disabling.

- **The stats page and the dashboard now ask for the same number of completed cycles, and the calendar
  legend lists only what the grid draws.** Stats unlocked its insights after two completed cycles
  while the dashboard held its ranges back until three, so one account was told both "2" and "3".
  Stats now waits for three, with the progress bar and its "n / 3" line reading that count; the
  sentence explaining why predictions are held back now shows above that waiting state too, so an
  irregular-mode account with one or two cycles still reads it. The late-cycle notice compares
  against a personal range, and the dashboard's cycle-factor hint appears, from three completed
  cycles as well. The calendar legend used to list every projected state to every account, including
  accounts whose grid drew none of them; it now shows an entry only when the month on screen carries
  that state.

- The "lost your authenticator" link on the two-factor challenge now leads to the recovery flow
  that works — email, then the recovery code beside the current password, which signs in without
  the authenticator and rotates both — instead of the one-time reveal page, which sent the visitor
  back to sign-in with no message. That flow is retitled "Sign in with your recovery code" on the
  page, in the sign-in link and in the tab title, since it never recovered a forgotten password: the
  current password is one of its operands.
- Auth page errors are announced: the error notice on the sign-in, registration, two-factor,
  recovery and reset pages is a `role="alert"` region, and the fields the error is about carry
  `aria-invalid` and are described by it. An enumeration-safe refusal marks every operand it
  compared, never one of them.
- The recovery-code reveal after registration guards against leaving the page before the code is
  acknowledged: a reload or navigation away asks for confirmation first, and the copy says where a
  lost code is regenerated (Settings → Account, behind the password).

- **Saving a past day no longer says it falls in your fertile window.** Filling in a day that is
  already behind you used to answer "this day is likely inside your estimated fertile window"
  whenever that date sat inside the window the current cycle projects. That line now appears only
  when you save today; a day you record after the fact, or one still to come, gets the plain
  "Saved." message.

- The stats page's BBT chart now scales its axis floor window and tick step to the owner's
  temperature unit. A °C axis kept its 0.8 °C floor and 0.2 °C tick; a °F axis (converted
  server-side, per #789) used to inherit those same numbers unscaled, cramming a °F reading's
  ~0.3 °C biphasic shift into a fifth of the visual range a °C owner sees. It now uses a 1.44 °F
  floor (0.8 °C × 1.8) and a 0.4 °F tick — the nearest readable step to the unscaled 0.36 °F.

- **An impossible temperature is no longer saved as an empty one.** For an account tracking in
  Fahrenheit, any entry from just above 0 °F up to 32 °F was quietly filed as "not measured" —
  the day saved without a word, the reading gone — because the "no measurement" test ran on the
  converted Celsius value instead of on what was typed. It now runs in the account's own unit: 0
  or less still means "not measured", and anything above it is judged against the physiological
  range and refused if it falls outside. The day form rejects such a value in the browser, so it
  was reachable through the JSON API, or from the form with scripts off. An infinity typed into
  the form is refused the same way, where a negative one used to be filed as "not measured" too.
- **A hidden field can no longer refuse the day it is not part of.** A form save from an account
  that hides temperature or cycle factors no longer loses the whole entry to a value in the hidden
  field: the form's hidden fields are not read at all — on an existing day the stored reading is
  kept, on a new day the field starts empty. JSON bodies carry every field they send.

- **A temperature shift landing exactly on the required margin above the coverline is no longer
  dropped.** The basal body temperature detector confirms a shift once three consecutive days sit
  above the coverline and the third of them reaches at least 0.2 °C above it, but comparing that
  margin as a floating-point number could reject a reading sitting exactly on it — a coverline of
  36.2 °C plus 0.2 came out a hair above 36.4, so a third day recorded at precisely 36.4 read as
  short of the margin. Readings entered in Fahrenheit hit the same boundary after conversion. The
  comparison now runs in the same fixed-precision units a saved reading is rounded to, so an
  exact 0.2 °C shift is confirmed whichever unit it was entered in. A restored backup now rounds
  its temperature readings onto that same precision, as a day saved through the app is.

- **"Mark anyway" no longer quietly does nothing.** A cycle start confirmed after the short-gap
  warning is kept as an uncertain start, and an uncertain start used to withhold the whole bleeding
  episode it sat in — so the marked day counted for less than an unmarked one. It now counts as no
  mark: the episode opens a cycle by the ordinary rules (two consecutive period days, or a single
  period day dated today or yesterday).

- **A recorded period day is never called fertile at the same time.** A period day that opens no
  new cycle — a spotting day, or a lone day once it is older than yesterday — was shown as
  "Menstrual" from the log while the running projection still called the same day fertile and
  counted down to an ovulation a few days off, on the dashboard, the statistics page, the API
  overview, the calendar and the `.ics` feed alike. On the day such bleeding is recorded and the
  day after, the fertile window and the ovulation day are now withheld on every surface, and
  `GET /api/v1/stats/overview` names the new reason `bleeding_outside_cycle_start`. The phase still
  reads "Menstrual" and the next-period estimate stays.

- **The startup pass that repairs the learned luteal phase no longer counts a cycle start you
  recorded ahead of time.** Ovumcy lets a cycle start be logged up to two days in the future, and
  the personalised luteal phase is learned from completed cycles. The one-shot repair that runs at
  startup read the whole stored history with no upper bound, so a start recorded for tomorrow was
  taken as the end of the last completed cycle and the stored value was derived from a day that had
  not happened yet.

  Every surface that shows a prediction re-derives this value over a window that stops at your own
  today, so the stored one and the live one disagreed by construction — which is the drift this
  repair pass exists to remove, not to introduce. The bound now sits in the single calculation all
  three writers of that value share — a day save, a restore from backup, and the startup repair — so
  none of them can count a day that has not happened. Bounding only the startup pass would have been
  worse than bounding none: it would have corrected the value, and the next saved day would have put
  the future-dated one straight back. A start already in the past counts as it always did.

  The stored value is a fallback rather than a dead cache: predictions fall back to it whenever the
  live inference has too little history to answer (fewer than three recorded cycle starts, or fewer
  than two usable ovulation signals), including on the dashboard and in the outbound webhook
  reminder. On those accounts the future-dated start reached what was actually shown.

- **A database that stops answering during startup now fails the start instead of hanging it.**
  Ovumcy runs three repair and sentinel passes when it starts, after the migrations and before it
  begins serving: the calendar-feed key-rotation check, the one-shot auth-email repair, and the
  one-shot luteal-phase recompute. Each waited on storage with no time limit. A database that
  accepts the call and then never answers — a SQLite file held by another process, a Postgres
  endpoint that connects and stalls — left the server alive and silent for as long as that lasted:
  no listener, no error, and a container healthcheck with nothing to report but "starting". An
  operator can read a refusal; there is nothing to read in a hang.

  Each pass now runs under a five-minute budget. The number is deliberately far above what any of
  them costs — the heaviest reads every owner's day logs once, uncontended, because nothing is
  serving yet — so reaching it means storage is stuck rather than slow. What happens then is
  unchanged and still differs per pass: the feed-rotation check and the email repair stop the boot,
  because a feed left armed or an account left unable to sign in is worse than an instance that
  refuses to start, while the luteal-phase recompute logs the failure, leaves its marker unwritten
  and lets the server start, since it maintains a cache with a safe fallback. The budget is per
  pass and the passes run in sequence, so the worst case for a start as a whole is fifteen minutes
  rather than five — size a container healthcheck start period against that. The operator runbook
  gains a troubleshooting entry naming the message, what to look at, and the fact that a start
  cut short this way loses nothing: every pass is safe to interrupt and safe to repeat.

- **The calendar's confirmed ovulation day now falls on the day the temperatures point at.** When
  basal body temperature is tracked and the recorded readings show a sustained thermal shift, that
  shift confirms an ovulation that has already happened — it says which day it was, and it never
  predicts a future one. The stats chart has always marked that day. The month grid did not: on a
  detected shift it kept its solid ovulation marker on the day the cycle model had projected before
  any temperature arrived, and the detector's answer was read only as a yes-or-no. The two surfaces
  therefore named two different days for one shift whenever the observation and the projection
  disagreed, which is the ordinary case rather than an unusual one — a cycle that ovulates later
  than its own average is exactly the cycle a thermal shift is being read for.

  The grid now moves the marker to the day the detector named and takes it off the projection it
  supersedes; the fertile-window shading around the projected day is unchanged, since that window
  remains a forecast and never claimed to be confirmed. The behaviour when no shift is found is
  unchanged too: the projected day stays, drawn as tentative. One detector, one day, on the calendar
  and in the chart alike.

- **Calendar days tell a screen reader what they show.** A day's accessible name was its date
  alone, so a recorded period, a predicted one, the fertile window, an estimated ovulation, a logged
  entry and today were all silent. Each day now reads its date followed by the legend's words for
  what the cell draws — a fertile day as an estimate, as the ovulation mark already is — today is
  marked as the current date, and the selected day reads as pressed.
- **Opening a calendar day moves to it.** Choosing a day, or Edit, Cancel or Delete inside the day
  panel, now moves keyboard focus to the panel's heading and brings the panel up below the header
  when it sits under the month or out of view — on a phone a tap looked like it did nothing, and
  after Edit focus fell back to the top of the page. The month is a single Tab stop navigated with the
  arrow keys instead of one stop per day, and Escape in the day panel returns to the selected day.

- **Dates counted forward or back from today no longer slip a day on a clock-change date.** In the
  timezones west of UTC whose daylight-saving jump happens exactly at midnight — Santiago on
  6 September 2026, Havana on 8 March 2026 — that local midnight does not exist. Counting a number
  of days inside such a timezone and landing on one of those dates quietly returned the day before
  it, at 23:00. Ovumcy counted days that way in nine places, and each of them named the wrong day
  once a year for owners in those zones:

  - the projected ovulation and fertile window behind every predicted date on every surface;
  - the chain of future cycles painted on the month grid, which shifted a whole projected period
    one day earlier;
  - the events published to a subscribed calendar app through the `.ics` feed;
  - the rule allowing a cycle start to be recorded up to two days ahead, which refused a date it
    was meant to accept;
  - the earliest date the onboarding and settings date pickers offered;
  - the ninety-day window behind the cycle-factor context on the stats page, and the dashboard's
    look at yesterday.

  All nine now count through one shared step that does its arithmetic on dates rather than on
  clocks, and then resolves the answer back into the owner's own timezone. Nothing changes on any
  other date or in any other zone.

- **The calendar reaches your whole stored history.** Its earliest month was the month of account
  creation minus three years, so older entries — imported or back-filled, and still served by the
  export, the API and the day panel — sat in months the calendar silently clamped away, along with
  the `?day=` that pointed at them. The lower bound is now the older of that window and your
  earliest stored entry (never before 1900-01, the oldest day a write accepts), and a disabled
  "Prev" says the calendar starts there. A month link that cannot be read (`?month=abc`) now opens
  the calendar with a notice saying so and keeps `?day=`, instead of redirecting without a word.

- **A long note or symptom name no longer widens the calendar page.** A note with a long unbroken
  run, such as a pasted link, or a 40-character symptom name used to stretch the whole page at phone
  width, month grid included. Both now wrap inside the day panel.
- **Calendar nits.** The day panel heading names the year when the day is not in the current year.
  The previous- and next-month links keep the open day; Today still opens today. A day save fetches
  the month grid once instead of twice, and marking a cycle start no longer starts a grid fetch that
  the page reload then discards. Phones now show a compact today mark in the today cell. A
  calendar link that opens a day for editing, such as the dashboard's link to log yesterday, now
  scrolls a day panel stacked under the month into view; opening another day from the grid drops
  that link's edit mode from the address.

- The Stats charts no longer run the last two day labels together ("1718") or cut the top temperature label ("100.0°F"), repaint when the system light/dark scheme changes, and show the "cannot render" message in the interface language.

- **The release image is built by the Go toolchain the test suite actually runs.** The builder stage
  had been bumped to `golang:1.27rc2-alpine3.24` while every CI job kept resolving its toolchain
  from `go.mod` (1.26.6), and `GOTOOLCHAIN` only ever upgrades — so the published binary was
  compiled by a release candidate that no unit, race or browser test had ever run under. The builder
  is pinned back to `golang:1.26.6-alpine3.24`. CI now fails when the builder tag and the `go.mod`
  directive disagree. Dependabot still offers a pre-release bump of that image — a docker `ignore`
  condition takes version requirements and cannot express "no `rc` tags", and an invalid one would
  stop Dependabot for every ecosystem in the repository — so this CI check is what catches it,
  documented next to the `dependabot.yml` entry it explains.

- **`ghcr.io/ovumcy/ovumcy-web:latest` follows `main` again.** The CI job that publishes it was
  skipped on every push to `main` while the run itself reported green, so `latest`, `main` and a
  `sha-…` tag all still resolved to one image, 242 commits behind the head of `main`, and the head
  carried no tag of its own. The job's condition named only the event and the branch, so
  it rested on the predicate GitHub supplies when a condition names no job status of its own. On a
  push to `main` every lane below the five gates the publish depends on is deliberately skipped,
  because the merge queue has already run them on that exact commit, and the publish was skipped
  with them. The condition now names what it needs instead of resting on that implicit predicate:
  it requires each of the five gates — `test`, `race`, `e2e`, `e2e-postgres-smoke` and
  `image-smoke` — to have reported `success` by name, so a gate that fails still blocks the
  publish. A documentation-only push, where the image smoke test does not boot the image,
  publishes nothing on purpose and leaves `latest` on the previous commit, which carries the
  identical image.

- **Onboarding no longer records period days that have not happened yet.** With automatic period
  fill on, finishing onboarding marked the whole configured period length from the chosen start
  date — a period started today with a length of five stored the next four days as recorded period
  days, and the calendar painted them as logged. The fill now stops at the owner's own today, in
  the owner's timezone, on the same bound the day editor's automatic fill already used: a period
  still in progress is recorded through today, and one that ended in the past is recorded whole.
  The chosen start date is stored exactly as before. Days written by an earlier onboarding are left
  as they are.

- **A recorded ovulation is no longer captioned "approximate".** The dashboard adds that marker when
  the projection's arithmetic had to be squeezed — a luteal phase that does not fit inside a short
  cycle gets clamped, and the resulting day is an approximation of an approximation. It is a
  statement about the projection, and it kept riding along after the day on the line came from
  recorded temperatures instead.

  The marker is dropped for a day the temperatures named. Everything about it is unchanged for a
  projected day, on the cycle-phase ring, and for an account with no thermal shift recorded — and
  the line still reads "around <date>", because the reading places ovulation within about a day
  rather than to the hour.

- **A confirmed thermal shift now moves the fertile window and the fertility status with it, on
  every surface.** When basal body temperature is tracked and the recorded readings show a sustained
  shift, that shift says the ovulation has already happened — nothing more. Ovumcy already moved the
  ovulation DAY onto the day inferred from those readings; the window around it and the fertility
  status stayed on the projection the shift had superseded. An owner
  whose shift landed earlier than the model expected therefore read a confirmed ovulation day
  beside a fertile window that ended days later, and a "fertile" status for a day the same
  temperatures had already placed behind them — on the dashboard header and ring, on the month
  grid, on the statistics page and in the JSON overview, each of which had converted a different
  half of the answer.

  All of them now read one resolver: the confirmed day, the six-day window ending on it (clamped to
  the recorded cycle start, exactly as the projected window is), and the status computed over that
  window. Past the third elevated day the status is "outside the estimated window", which is the
  only thing the method asserts — never that the day is infertile. The projected next period is
  deliberately left alone — it stays a projection and is not recomputed from a confirmed ovulation.

  The cycle phase moved with them. Ovumcy estimates how long a period lasts from your own average,
  and used to call every day inside that estimate "menstrual" even when the ovulation day published
  right beside it fell on the same day or earlier — one screen answering two ways about one date.
  That estimate is now cut short at the published ovulation day, so a day past a confirmed shift
  reads as luteal rather than as an estimated period still running. A day you logged bleeding on is
  untouched: what you recorded outranks every estimate, and always did. The month grid follows the
  same rule — the expected-period shading now stops before the ovulation day rather than sitting on
  the same cell as the ovulation marker and the fertile shading. So does the dashboard ring, which
  shortens its period band instead of disappearing, which is what a shift confirmed early in a cycle
  used to make it do, to the owners tracking their temperatures most closely.

  A confirmed shift also no longer needs the model to agree that a cycle can ovulate at all: an
  account whose recorded history is too short for the model to place an ovulation used to see its
  own temperature signal silently dropped from the calendar, the overview and the dashboard — whose
  hero ring read the same withheld claim independently of the calendar's fix and stayed hidden — and
  the hero ring is visible again. The reminder banner reads a different gate and stays as it was:
  it only ever announces a day still ahead, and a confirmed day is by construction several days
  behind today, so it was never the day this fix addresses. Where predictions are withheld
  altogether — unpredictable-cycle mode, a pregnancy pause, an overdue cycle, or before the first
  completed cycle — the silence is now more complete than before, not merely unchanged: the cycle
  phase label used to keep naming the withheld ovulation day even after every other field went
  quiet, so a suppressed account could still read "ovulation" today, or "follicular"/"luteal" on
  either side of it, off the phase alone. The phase is now recomputed from the same fields the rest
  of the response withholds, so it can say "menstrual" during a logged or projected period, or
  "unknown" otherwise, but never point at the day suppression exists to hide. The dashboard's cycle
  ribbon widget read the day through a second, separate path and stayed just as loud: its phase
  label and its phase-card breakdown followed the same suppressed ovulation day the rest of the
  response had already gone quiet on. The ribbon itself — the axis, the day cells, the "today"
  marker — still draws; only the parts that named the day now don't. A recorded observation still
  never becomes a way around any of this.

  Going quiet is not the same as going blank, and the ribbon now says which one it is. The days
  whose phase depends on the withheld ovulation day carry a status of their own — "fertile details
  held back" — in their own colour, instead of the empty track that means the projection has run
  out. Mid-cycle, the difference is the difference between "there is more to show later" and
  "tracking has stopped". The dashboard's phase line says the same words as the cells beneath it
  rather than a second phrase for the same silence. And an account still waiting for its first
  completed cycle — the tier with nothing behind the fertile half but the cycle length from setup —
  is now told so in a sentence under the ribbon, which is where every other reason for a quiet
  prediction already says its name.

- **The dashboard, the stats page and the day-save message now read today's fertility status and
  phase against the same window as `GET /api/v1/stats/overview`.** Once the running cycle's
  ovulation was behind today and the window rolled to the next cycle, the API and the calendar
  called today fertile while the dashboard header and the stats page still showed "luteal" and
  "outside the estimated window", and the save message stayed neutral. They now follow the rolled
  window: `fertile` with no phase named where it covers today, and `unknown` for both where the
  rolled day falls past 9999-12-31. Pregnancy pause, unpredictable-cycle mode, suppressed
  predictions and out-of-date cycle data are unchanged.

- **A second account on the same browser now sees the dashboard's first-run tips.** The "seen" mark
  for the dashboard tour, sex-section and cervical-mucus tips was stored without naming the account,
  so whoever signed in after the first owner never saw them. The mark is now kept per account.

- **The dashboard's ovulation line now names the day your temperatures point at.** When basal body
  temperature is tracked and the readings show a sustained thermal shift, that shift confirms an
  ovulation that has already happened and says which day it was. The month grid and the stats chart
  both name that day. The dashboard line did not — it kept naming the day the cycle model had
  projected before any temperature arrived.

  The two disagreed most visibly on the projected day itself. The line rolls its estimate forward to
  the next cycle only once the projected day is behind you, so on that day it stayed put and
  announced an ovulation as happening now, while the calendar beside it marked the same ovulation
  several days earlier. For someone reading the two together to time anything, that is the
  difference between "today still counts" and "this window has closed".

  The line now names the confirmed day, which is usually already behind you, instead of announcing
  one as upcoming. It does not raise the "date is already in the past" notice while doing so: that
  notice is about an estimate the app is still pointing at after its day has gone by, and a confirmed
  thermal shift being behind you is simply what the rest of a cycle looks like. Both surfaces resolve
  the day through one shared reader, so a single shift can no longer produce two dates.

  A confirmed day also outranks the two ways the dashboard expresses how uncertain a *projection* is:
  it is named as a day rather than widened into the range shown for irregular cycles, and it is not
  withheld under "needs more cycles". Neither of those is about a day the temperatures have already
  pointed at, and both used to hide it while the calendar went on marking it — for irregular cycles,
  the very accounts the model serves worst.

  One thing does stop appearing, and that is the point of the change: the dashboard's "ovulation is
  coming" reminder no longer counts down to a day the temperatures have already placed behind you.
  It counted down to the projection, so on the projected day it announced an ovulation as arriving
  today while the calendar beside it marked the same one several days earlier. With no shift
  recorded it counts down exactly as before.

  Nothing here changes *whether* an estimate is shown. An account with predictions turned off, a
  paused pregnancy estimate, an overdue cycle, or an account that has not completed a single cycle
  yet withholds the ovulation estimate exactly as before, on the dashboard and the calendar alike —
  they now read that decision from one place instead of each applying it separately. With no shift
  recorded, the projected day is shown unchanged.

- **The dashboard header now follows a Save.** A save sent through the form itself (Enter in a
  journal field) left the cycle day, phase and ribbon showing the old state after the period
  checkbox or the pregnancy test changed; it now refreshes them the way the autosave does.

- **The dashboard no longer states a fertility verdict for an account whose predictions are
  suppressed.** The header's fertility gate carried one of the four suppression signals — the
  zero-completed-cycle floor — so an owner in unpredictable-cycle mode, on a pregnancy pause, or
  past their own reference cycle length still read "Fertile window" on the status line, and the
  header still declared "fertile" or "not fertile" as its fertility state. In unpredictable-cycle
  mode that line sat next to the notice saying predictions are off. All four signals now decide it,
  through the same gate the calendar grid, the `.ics` feed and the reminder banner already use: in
  those states the status line shows no fertile-window item and the header declares no fertility
  state at all, while the cycle day, the phase and recorded observations are untouched.
- **The journal grid stops publishing that verdict too.** The grid below the status header carried
  the raw fertility classification with no gate on it whatsoever, so it stayed readable to scripts
  and styling in every suppressed state, including before the first completed cycle. It now reads
  the same single verdict the header does.
- **An overdue cycle no longer names a next-period date to an owner with irregular cycles and
  little history.** Withholding the estimate past reference + 7 days reached everyone except the
  one group tracking irregular cycles with fewer than three completed: they kept seeing a projected
  date captioned "needs more cycles" — a date rolled a whole cycle forward from a period that
  should already have started. That owner now gets the same "estimate paused" notice everyone else
  past that point gets. Owners inside their reference length still see the estimate with its
  caption.
- **An owner tracking to conceive whose first cycle runs long is told when the fertile window
  arrives, instead of nothing.** Before the first completed cycle the status line carries one
  dateless sentence — "your fertile window appears after your first completed cycle" — in place of
  an ovulation estimate. Once that first cycle passed the reference length the sentence disappeared
  too and the slot was simply empty, at the point the owner has the most reason to wonder. Pausing
  a projection withholds a date; this line names none, so it now stays for as long as the first
  cycle is open. It still goes when predictions are off altogether — unpredictable-cycle mode or a
  pregnancy pause — where promising a future window would contradict the rest of the page.

- **Breaking (API shape): a date outside 1900-01-01 through 9999-12-30 is now answered `400`
  `invalid date` instead of being accepted.** `GET /api/v1/days/0001-01-01` used to answer `200` with
  an empty `date`, and `9999-12-31` built a read range ending in year 10000 that cannot be encoded or
  ordered in the database. The bound applies to every date input: the `{date}` path of the day routes
  (`GET`, `HEAD`, `PUT`, `DELETE`, `cycle-start`), the `from` and `to` bounds of `GET /api/v1/days`
  and of the exports, and the dates in an import file, where such a row is counted as rejected and the
  rest of the file is imported. A client that reads, writes or deletes a day dated before 1900 has to
  stop asking for it; a whole-account export without `from`/`to` still includes every stored day.

When a day save met a conflict and another account had signed in in the same
browser since the page was rendered, choosing "keep mine" or "take theirs" on
the dashboard or in the calendar editor showed "couldn't save, try again",
which no retry could make true. It now shows that the account changed, as the
other day reads already do, and the entry stays in the form. The
account-changed notice on either day form also stays until the next save
starts: an earlier save's own confirmation or failure, or the answer of the
cycle-start control beside the form, no longer replaces it when it arrives
second, and a confirmation's timed dismissal no longer takes it down.

- **The temperature chart no longer draws unlogged days as 0 °C readings.** A day with no
  measurement travels as JSON `null`, and the chart converted it to the number 0, which passed every
  finite check: the first days of a cycle appeared as real points at 0 °C, the line ran through them,
  and the axis stretched from 0 °C to the highest reading — flattening the biphasic shift the chart
  exists to show. An unlogged day is now a gap: no marker, and the line breaks around it.
- **The temperature axis is scaled to the readings.** It spans the reading range plus 0.15 °C on each
  side, widened to a floor window of 0.8 °C so that a cycle with little movement is not magnified
  into noise, and it carries a tick every 0.2 °C.
- **The chart series line meets the 3:1 contrast floor for non-text content.** The light-theme line
  colour measured 2.97:1 against the card behind it; both themes' line colours were revalidated
  against the surface they are drawn on, and markers gained a ring in the surface colour so a point
  stays readable where the line passes under it.

- **The dashboard cycle ring was made visible in the dark theme again — since superseded.** Its
  track — the rest of the cycle the coloured phase segments were drawn on — measured **1.34:1**
  against the card behind it, where WCAG 1.4.11 puts a meaningful graphic at 3:1, so the ring
  dissolved into the card in the dark; this pass raised it to 3.38:1/3.95:1. Wave 4 later replaced
  the ring itself with a cycle ribbon — one cell per day carrying the phase, the fertile window and
  entry marks along the band — which carries its own contrast: only the period and ovulation cells,
  the two a reader looks for, are held to the 3:1 floor. The recessive follicular/luteal pair is
  deliberately exempt against the light card, where it sits under the floor; on the dark card it
  happens to clear it anyway. The ribbon as a whole is redundant to the day marks and status text
  around it, which is why the floor is scoped to those two cells at all. This ring's numbers
  describe a track this release no longer contains.
- **The dark cycle card no longer carried a pale smudge in its top-right corner from the
  light-theme glow tints.** Two decorative corner glows kept their warm light-theme paper tints on
  the dark card, lifting it by up to **3.27:1** and 1.71:1 — a decoration painted as loudly as a
  meaningful graphic, which is why the brighter one read as a render artifact. This pass dropped
  both from the dark theme; a later pass gave dark its own tinted pair instead, so the corner is no
  longer bare there either — only the light-era light tint this fix removed stays gone. The light
  theme keeps its original pair at the 1.09:1–1.13:1 they were drawn for.

- **Onboarding step 1 now asks for the last period start once, through a month calendar.** The
  screen carried two mechanisms for the same value — a 60-button day grid that was clipped
  mid-row on a phone, with the "Next" button overlapping the cut-off row and reading as
  disabled, plus a separate day/month/year field beside it. Both are replaced by a single
  compact picker: "Today" and "Yesterday" shortcuts, a month grid whose cells are real buttons
  labelled with the full date, month names and weekday headers in the interface language, the
  owner's week-start honoured, and every date after today rendered inert. Nothing scrolls
  inside the picker, so at 390px the whole month fits above "Next" instead of behind it. The
  submitted field, its name and its ISO format are unchanged, and the server's range validation
  is untouched.

- **Primary buttons are readable again.** Login, "Update entry", "Save changes", "Change
  password", "Add custom symptom" and the other primary actions all render white bold text on
  the one `.btn-primary` component, whose warm gradient measured **2.23:1** at its dark stop and
  **1.63:1** at its light stop against that text — WCAG AA asks for 4.5:1 on text this size. The
  component now fills with a solid terracotta (5.77:1 in the light theme, 5.14:1 in the dark
  one), with hover, active and the three cycle-phase tints carried as tokens that clear the bar
  as well. Solid rather than a deeper gradient on purpose: text on a `background-image` is what
  automated contrast checks report as *incomplete* rather than as a failure, which is how this
  slipped through in the first place. A browser check now resolves the painted background — every
  stop of it, if it is ever a gradient again — and asserts 4.5:1 in both themes.

- **A day entry that fails to save now says so, and offers to send it again.** On a self-hosted
  instance the server is routinely unreachable — the owner is off the home network — and a save that
  died in flight left the calendar day editor silent: the button un-pressed itself and nothing else
  changed, which is indistinguishable from a save that worked. The editor now answers a failed save
  in its existing `aria-live="polite"` status region with a neutral notice and a retry control that
  resubmits the same form. The form survives in the DOM; nothing is persisted client-side.

- **The mobile tab bar no longer shows page content through itself, and the footer link below it is
  reachable again.** The floating bar painted a translucent background (`alpha 0.96` in light,
  `0.95` in dark) with no backdrop filter, so text scrolled visibly through it on dashboard,
  calendar and insights. It is opaque in both themes now. The bottom clearance the layout intended
  was also inert: the footer's `padding-bottom` lost to the utility class on the same element, which
  left the privacy link 34px inside the bar at the bottom of the page on a 390x844 viewport.

- **The active language pill is readable.** The pre-auth switcher marked the current language
  with near-white text on a light warm gradient that measured **1.98:1** at its light stop
  (**3.35:1** in the dark theme) — WCAG AA asks 4.5:1 for a label this size. The pill now fills
  with a deeper tone of the same hue: 5.25:1 in the light theme, 5.60:1 in the dark one, worst
  stop of each gradient.
- **Decorative emoji are no longer read out as content.** 48 of the 58 distinct glyphs this UI
  renders reached the accessibility tree, so a screen reader announced "shield", "file cabinet",
  "hourglass", "see-no-evil monkey", "crystal ball" and "electric plug" ahead of the headings,
  toggles and section links they decorate. Every decorative glyph is now `aria-hidden`, and the
  mood chips — whose only content was an emoji — name themselves instead.
- **The cycle-length and period-duration sliders can be hit.** Both offered a pointer target of
  1038×9 px, under the 24 px WCAG 2.2 AA minimum. The rail keeps its slim look while the control
  now claims a 24 px-tall target.
- **Navigation landmarks are told apart.** An authenticated page renders four to five `<nav>`
  elements and most carried no name, so a landmark list showed several identical "navigation"
  entries. The header, mobile menu, bottom bar and footer navigations now carry distinct
  localized labels in all six languages.
- **Selected choice tiles keep a margin over the contrast bar.** The label on a selected goal,
  pregnancy-test, BBT-unit, week-start, language, theme or mood tile measured 4.64:1 against the
  darker end of its fill — a pass with nothing to spare; it now measures 5.60:1.

- **The phase label no longer alternates between two vocabularies.** On the same day the dashboard
  could say "Follicular" while Insights said "Current phase: Fertile" — cycle phase (menstrual /
  follicular / ovulation / luteal) and fertility status (inside the fertile window or not) are two
  different facts, and each screen answered with whichever one it computed. The phase slot now
  always carries one of the four phases, and the fertile window shows as its own "Fertile window"
  note beside the phase on the dashboard and on Insights, read from the same window bounds the
  calendar already shades — so the surfaces cannot disagree again.

- **The coverline is painted for the theme it is on.** `--chart-baseline` had no definition in either
  theme: both the stylesheet and the chart script fell back to the same light-theme colour, so the
  dark card drew its reference line and probable-ovulation marker in a light-theme neutral. The token
  now carries a value per theme, each measured against the surface it is drawn on.

- **`POST /api/v1/users` documents the `consent` field it has always required.** `RegisterRequest`
  declared `required: [email, password, confirm_password]` next to `additionalProperties: false`,
  while the handler refuses any body whose `consent` is not truthy — so a client generated from the
  spec was refused for omitting a field the same document forbade it to send. The field is published
  with its accepted spellings (`1`, `true`, `on`, `yes`, case-insensitive, trimmed) and the refusal
  it produces. Found on the load stand, where the harness could not register until it sent `consent`.
  A new contract test reads the required set out of `docs/openapi.yaml` and lets the endpoint judge
  it: a body carrying exactly the declared required fields must not be refused as invalid, so a
  field the server starts demanding without documenting it fails the suite.

- **The self-hosted backup runbook no longer calls a live whole-volume archive safe.** It stated
  that the archive captures `ovumcy.db`, `ovumcy.db-wal` and `ovumcy.db-shm` together, and asked for
  the app to be stopped only when an operator copies the files individually. Capturing all three is
  necessary and, against a running instance, not sufficient: `tar` reads them one after another, and
  a checkpoint landing between the read of the main database file and the read of the WAL writes the
  WAL into the main file *after* that file was read, then empties the WAL *before* it is read. The
  archive comes back carrying all three files and missing a commit that was in the database before
  the backup began, with nothing about the run reporting a problem. The runbook now asks for the app
  to be stopped before the data volume is archived — or for the archive to be taken from an atomic
  snapshot of the volume — and says so where an operator meets the archive command as well as in the
  backup contract.

- **Post-Restore Verification now sends an operator back to the calendar feed.** A restore returns
  the feed columns to their state at backup time, so a subscription an owner revoked or rotated
  *after* that backup was taken is armed again at its old subscribe URL. This is a different trigger
  from a `SECRET_KEY` rotation, which disarms feeds instead, and the runbook's rotation section
  covers only that one. `docs/gdpr.md` had recorded the restore case — confirmed in a live restore
  drill — and the file an operator actually follows through a restore never pointed at it. It is now
  step 7 of the checklist.

- **`docs/openapi.yaml` now states the real recovery-code and password-reset-redeem responses.**
  `POST /api/v1/users/current/recovery-code` published only "recovery code surface rendered" and told
  JSON clients to follow a redirect that never happens for them — the handler answers a JSON caller
  directly with `{ok, next_step, next_path}`, never a `303`. The spec now declares that body and the
  browser-only `303`/`HX-Redirect` separately. `POST /api/v1/password-resets/redeem` claimed its
  success redirect lands on `/login`; it lands on `/recovery-code`, where the freshly minted code
  waits — fixed, with a guard test driving the real browser-surface request. The same operation's
  `403` named only an operator-set `must_change_password` as the reason a forced-from-OIDC reset
  token survives the local-auth-disabled gate; an enrolled-but-unverifiable TOTP secret (a
  `SECRET_KEY` rotation) reaches the identical escape hatch and is now named too, with a guard test
  (local public auth switched off before redeem, against a fixture account that is genuinely
  TOTP-enabled) proving that account's recovery path completes end to end. Two smaller trues alongside:
  `NextStepResponse`'s note no longer reads as a list of producers — `POST /api/v1/password-resets`
  answers `recovery_code` too, through its own schema and without a `next_path` — and both
  recovery-code operations now name the third success shape, the htmx one, which lands on `200`
  with `HX-Redirect` and a plain-text body instead of the declared JSON, under a guard test.

- **The API reference now lists the language switch's `400` and `429`.** `docs/openapi.yaml`
  documented only the success answers of `POST /lang`. It now also lists the `400` that a missing
  or blank `lang` gets, with the shared `bad_request` error envelope, and the `429` from the
  route's own per-IP rate limit, with `Retry-After`. Each one says which callers get the JSON
  envelope and which get an HTML fragment instead. The `lang` field no longer claims a closed
  list of codes: the server falls back to the default language for a code it does not ship. The
  `303` description now says that the redirect goes to the form's same-origin `next` path, not
  to the referer. A guard test now finds every rate limiter mounted on a documented path outside
  `/api/v1`, so the spec cannot leave out a `429` there. The server's behaviour is unchanged.

- **The API reference now lists the language switch's `403` and `500`.** `docs/openapi.yaml`
  now lists the `403` the CSRF check gives `POST /lang` for a missing, mismatched or expired
  token or a cross-site request, with the shared `forbidden` key, and the `500` a signed-in owner
  gets when the chosen language cannot be stored on the account, with the key `failed to update
  interface settings` and no language cookie set. Each one says which callers get the JSON
  envelope and which get an HTML fragment instead. The server's behaviour is unchanged.

- **Starting an older Ovumcy against a database a newer one wrote is refused instead of passing
  unnoticed.** Migrations are forward-only, and the runner only ever looked for migrations it had
  yet to apply — so a rolled-back image found nothing to do, said nothing, and served requests
  against columns and conventions it does not know about, writing rows the next upgrade then had to
  reconcile. A start whose ledger records migrations this binary does not carry now ends before any
  statement runs, naming those versions, the newest one this binary knows, and the two ways out
  (start the newer release again, or restore a backup taken from before the upgrade). The check
  lives in the binary being started, so it covers every downgrade from this release onward; going
  down to a release that predates it stays silent, and the runbook says so.

- **The first start after upgrading no longer adopts a legacy calendar feed as its baseline.** Feeds
  armed before the keyed verifier MAC landed (pre-migration-032 rows) verify through bcrypt, which
  does not depend on `SECRET_KEY`, and nothing in such a row records which secret minted it. The
  boot sentinel treated an absent stored key epoch as "new installation", recorded the current epoch
  beside those rows and left them armed — so the first poll backfilled a MAC under today's key, and
  a subscribe URL that a `SECRET_KEY` rotation in that same maintenance window was meant to kill
  came back to life. That branch now runs the same disarm as a detected rotation before it records
  anything: the legacy rows are cleared and their owners generate a fresh subscribe URL from
  Settings, while a genuinely new installation reaches the branch with nothing armed and is
  unaffected. The startup log names the count when it happens.

On the outgoing-data card, the result of saving a webhook now appears right
under its Save button, and revoking the calendar link shows its result beside
those buttons; focus stays at the control instead of dropping to the page. A
webhook address that was refused comes back in the field so you can correct it,
and an address that does not start with http:// or https:// is caught before
sending. The stored destination now shows its host and port with the path
masked, and the card summary no longer claims a route sends data while the row
below says this instance sends nothing. Italian wording for the one-time link
display is clearer.

- **The German calendar-feed texts use the formal address.** The feed's settings card, subscribe
  page and status messages addressed the owner as "du" while the rest of the German interface uses
  "Sie"; they now use "Sie" throughout.

The fertility chips on the dashboard ("High fertility", "Fertile window",
"Fertile window, best timing"), the calendar's fertile-window legend entry, the
fertile-window caption on the dashboard and stats, the "Ovulation" phase
label, the dashboard's ovulation range and the next-period days line now carry
an estimate qualifier in their own wording, in all six languages. Before,
these surfaces read as a statement of fact unless the separate "estimated"
caption happened to render beside a thin history.

The calendar no longer shows a fertile window or an ovulation day on cycles
projected far ahead. A regular account with enough history saw full fertile
shading and ovulation markers on every chained cycle, as far as three years out,
and the `.ics` feed sent an ovulation event for two cycles past the running one.
Each projected cycle adds its own spread to the one before, so a few cycles out
the estimate is wider than the window it names. Fertility is now shown on the
running cycle and the next one only — on the running cycle only in irregular
range mode — on the calendar and in the feed alike. Predicted periods still
cover the whole calendar.

- **Breaking (API shape): enabling or disabling 2FA now answers a JSON client with a body.** `PUT` and
  `DELETE /api/v1/users/current/2fa` content-negotiated on `HX-Request` alone: an HTMX caller got
  the inline status markup and *everyone else* — an API client sending `Accept: application/json`
  included — was redirected to `/settings`, a response carrying nothing a non-browser client can
  read, and one `docs/openapi.yaml` never declared. Both now take the JSON arm their sibling
  settings mutations (`data-wipe`, `DELETE /users/current`) already take and return the declared
  `{"ok": true}` alongside the re-issued session cookie; the browser redirect is unchanged, and so
  is the HTMX markup the settings page renders. The spec publishes the `200` body and states the
  `303` as the non-JSON fallback.
  - The contract guards did not miss this for want of walking the route table; they exclude `303`
    on purpose. `crossCuttingStatus` in the per-operation reachability test suppresses it because
    the shared error responder redirects any `/api/v1/users/current` path that does not accept
    JSON, which would make the status look emittable from every operation. Lifting that exclusion
    turns 20 operations red, and 18 of them falsely: those handlers return JSON from an
    `if acceptsJSON(c)` arm before ever reaching the redirect, which the guard's HTML-surface
    narrowing reads only as an `if` guard, never as an early return. Teaching it to see redirects
    therefore needs flow sensitivity plus a per-operation verdict on each survivor, and is tracked
    as its own change.

- **A malformed mood in a form-encoded day save is refused instead of clearing the recorded value.** `mood=abc` used to be substituted with zero — "no mood recorded" — so a malformed update erased a mood the owner had saved and still answered 200. It now answers 400, exactly as the same value in a JSON body always did. An absent or empty field still means "no mood", which is what an unchecked radio posts.
- **A calendar page failure no longer reports that the stats load failed.** The calendar error mapper named only one of its two sentinels, so a stats-load failure and any future failure both inherited the message of the other one. Both sentinels are named now, and an error the mapper does not know answers with its own key.

- **Leaving the dashboard no longer drops a journal edit made while a save was still running.** The
  autosave coalesces on the request already in flight, so an edit typed during a slow save bumped the
  pending version but queued nothing; the only thing that would have carried it was the re-arm timer
  that runs after the response, which a page unload never reaches. Closing the tab or navigating away
  in that window therefore left the newer value on screen and the older one on the server, with no
  error shown. The unload flush now sends the current form state on its own keepalive request
  whenever it is newer than the save on the wire — once per version, so a cancelled navigation does
  not resend it. The ordinary debounce path is unchanged.

- **A mistyped security setting now refuses the boot instead of running on the default.** A value the
  app cannot parse for `COOKIE_SECURE`, `HSTS_ENABLED`, `TRUST_PROXY_ENABLED` or
  `WEBHOOK_BLOCK_PRIVATE_ADDRESSES` (`COOKIE_SECURE=ture`) used to log a line and start on the
  fallback, so what an instance actually ran with could not be read from the env file. With
  `TRUST_PROXY_ENABLED=true`, a `TRUSTED_PROXIES` entry the trust boundary cannot use — a broken CIDR,
  a non-IP string, an IP not written in canonical form, or an IP listed twice — was likewise dropped
  or collapsed, leaving the proxy
  untrusted so every client behind it shared one rate-limit bucket while the startup banner counted
  the raw list and reported the typo as configured. Both now stop the process with an error naming
  the key and the rejected value. Unset keys still mean their documented defaults, and every other
  env var keeps its fallback behaviour.

- **Predictions no longer jump a whole cycle, or drop a day's own entry, outside
  UTC.** On the estimated ovulation day itself, accounts west of UTC saw the
  dashboard, the calendar, the reminder and the `.ics` feed skip that estimate and
  advertise the next cycle's instead, and the feed lost its ovulation event for
  the day; accounts east of UTC had today's entry ignored when deciding whether a
  temperature shift had been observed. The Insights cycle stack also left the
  first or last fertile day unshaded, depending on the time zone, and never marked
  the peak day at all.

- **The calendar grid no longer prints one day twice and drops the last one in
  time zones whose clock jump lands on midnight.** In zones west of UTC where
  daylight saving starts at 00:00 (America/Santiago, America/Havana), local
  midnight does not exist on the transition date. The grid stepped from day to
  day as an instant, so that missing midnight resolved one calendar day
  backward: a month whose displayed range crossed the transition rendered the
  preceding day in two cells and never rendered the final day of the range at
  all — the cell for it was the duplicate. The grid now counts and steps its
  cells as calendar days, so every day in the range appears exactly once, in
  order. Zones without such a transition, including UTC, render identically to
  before.

- **The calendar no longer opens the wrong month in time zones whose clock jump
  lands on the first of a month.** In zones west of UTC where daylight saving
  starts at 00:00 on a first-of-month (America/Asuncion 2023-10-01,
  America/Havana 2012-04-01), local midnight does not exist that day. The month
  the page anchors on was parsed and built directly in the request zone, so the
  missing midnight resolved one calendar day backward: asking for that month
  rendered the whole grid, the heading and the month value for the *previous*
  month instead. The month links inherited it — "previous" skipped a month, and
  "next" pointed back at the page already shown, so the affected month could not
  be reached at all — and the same construction shifted the lower navigation
  bound a month past the account's own history. Month anchors are now stepped as
  calendar dates and resolved through the single day-construction point, so the
  requested month is the month that renders. Zones and months without such a
  transition, including UTC, behave exactly as before.

- **A shaded range on the calendar no longer loses a day in time zones whose
  clock jump lands on midnight.** In zones west of UTC where daylight saving
  starts at 00:00 (America/Santiago, America/Havana), the builders that paint a
  predicted period, a fertile window or the pre-fertile band stepped through the
  range by adding a day to an instant, and the missing midnight resolved
  backward into the previous calendar day: the range painted that day twice and
  dropped one of its own — the transition day itself, or the range's last day.
  Ranges are now stepped and bounded by calendar day. The same change restores
  the closing day of the projected and historical pre-fertile bands, which every
  zone west of UTC lost in every month because the band's two ends were measured
  from different midnights. UTC and zones east of it are unaffected.

- **Marking a new cycle start now offers to replace an earlier start on the first
  bleeding day of the same period.** Depending on the account's time zone, a start
  already recorded on the edge day of a period could go unnoticed: no replacement
  was offered, and confirming one left the old start in place, so the same period
  carried two cycle starts.

- **`docker-compose.yml` no longer lets `PORT` and `TZ` silently diverge from `.env`.** The bundled
  compose file read `${PORT:-8080}` for the `ports:` host **and** container mapping, but the
  container's actual listening port came only from `env_file: ./.env` — `PORT=9090 docker compose
  up` published `9090:9090` while the process kept listening on `.env`'s `8080`, making the service
  unreachable with a green internal healthcheck. Separately, `TZ=${TZ:-UTC}` sat inside
  `environment:`, which always outranks `env_file`, so a leftover shell `TZ` silently beat `.env`'s
  `TZ` — and timezone decides cycle-day boundaries. `PORT` now lives in `environment:` so the ports
  mapping and the container's env come from the same substitution; `TZ` now comes only from
  `env_file`.

- **The dashboard cycle ribbon's predicted-flow hatch and start-window gradient are now
  readable in both themes.** They previously reused the calendar's warm accent colour, which sat
  at nearly the same lightness as the phase fill they are drawn over (measured 1.00-1.02:1 for the
  hatch, 1.20-1.22:1 for the gradient's dark-theme 'beyond' case — both well under the WCAG 3:1
  floor for a meaningful non-text graphic). Both overlays now use a neutral stripe calibrated
  against what they actually paint over (4.61:1 / 3.57:1 for the hatch on a phase fill; 5.77:1 /
  7.50:1 for the gradient on the dark 'beyond' track).

- **The dashboard cycle ribbon is no longer invisible to a screen reader.** It carried
  `aria-hidden="true"` on the premise that every fact it draws is stated in the status line above
  it, but two per-day facts — which days fall in the predicted start window and which were actually
  logged — exist only in the ribbon, with no textual equivalent anywhere else on the page. Each day
  cell now carries its own accessible name (`role="img"` plus a computed `aria-label`: cycle day,
  phase, and start-window/logged when they apply) instead of the ribbon being hidden outright. Also
  dropped `data-projected`, a ribbon-day attribute nothing ever read.

- **The dashboard reminder banner no longer marks a confirmed ovulation day approximate.** The
  in-app "ovulation in ~N days" banner derived its approximate marker from
  `DisplayOvulationExact` alone, while the dashboard's own ovulation line also checks
  `DisplayOvulationConfirmed` — so a BBT-confirmed shift landing on a clamped (non-exact) luteal
  projection read as approximate in the banner while the line beside it did not. The banner now
  reads both fields, matching the dashboard.

- **The long-period warning is now counted over calendar days in time zones
  whose clock jump lands on midnight.** The backward walk that measures how many
  consecutive days the current period has run stepped its cursor inside the
  request time zone. In zones west of UTC where daylight saving starts at 00:00
  (America/Santiago, America/Havana) that step skipped the transition date
  entirely: a continuous period spanning it counted one day short, so a nine-day
  period stayed one day below the threshold and the "period running long" notice
  never appeared. When the transition date carried no period at all, the walk
  jumped that gap instead of stopping there and merged two separate periods, so
  the notice was anchored to — and acknowledged against — the previous period's
  start date. The walk now steps over calendar days, visiting each exactly once.
  Zones without such a transition, including UTC, are unaffected.

- **A day note in a non-Latin script is no longer cut in half on save.** The notes field advertises a
  2000-character limit, but the server measured that number in bytes: 2000 typed Cyrillic characters
  are 4000 bytes, so roughly half the note was dropped on save, and 2000 emoji were dropped to 500 —
  with no warning and no error, the shortened text simply came back on reload. The limit is now
  counted in characters on both sides, so everything the field accepts is stored whole. The limit
  itself is unchanged at 2000 characters.

- **A day save, delete, cycle-start mark, or restore near midnight could refine the stored luteal
  phase from the wrong day.** The derived `luteal_phase` estimate is recomputed after every one of
  those writes, bounded at "today" so the derivation never reads a cycle start that has not
  happened yet. That bound was read from the request's timezone (the browser's header/cookie)
  instead of the account's own stored timezone — the same bound the boot-time recompute and the
  two egress passes already use. An account near a date line, or simply logging in a timezone the
  request happened to carry a stale value for, could see its derived luteal phase refine (or fail
  to refine) a day earlier or later than the boot pass would have computed for the same data — a
  disagreement between the two that only the *next* write corrected.

  Every writer of the derived value now resolves the account's own stored timezone first, falling
  back to the request's zone only when none is stored yet, matching the resolver the webhook and
  `.ics` feed passes already share. Nothing changes for an account whose stored timezone matches
  the browser's.

- **The desktop navigation marks Settings as the current section again.** After the
  duplicate gear icon was removed, the account chip was left as the only desktop
  route to settings but did not take over the active highlight, so on `/settings`
  the desktop navigation marked nothing while Today, Calendar and Insights each
  marked their own page. The chip now carries the same active treatment and
  `aria-current="page"`; the bottom tab bar stays the single marking on a phone.

- **The spring-forward date can now be entered in time zones whose clock jump
  lands on midnight.** In zones west of UTC where daylight saving starts at
  00:00 (America/Santiago, America/Havana), local midnight does not exist on
  that date, and every date input resolved it one calendar day backward: the
  day picked was stored, shown and exported as the previous day, and an
  onboarding cycle start recorded on it anchored every later prediction to the
  wrong day. Calendar days now resolve to the first instant that really exists
  on the requested day. Zones without such a transition, including UTC, are
  unaffected.

- **The two public Postgres example env templates no longer downgrade the stack they ship with.**
  `docs/examples/reverse-proxy/caddy-postgres/.env.example` and its nginx twin pinned
  `OVUMCY_IMAGE=ghcr.io/ovumcy/ovumcy-web:v0.8.4`, while the `docker-compose.yml` beside each one
  defaults to `${OVUMCY_IMAGE:-ghcr.io/ovumcy/ovumcy-web:v1.9.2}`. The runbook links both files as
  the env template and instructs an operator to copy them to `.env` before the first start, and an
  assignment in `.env` wins over the compose default — so the documented setup path deployed a
  pre-1.0 image on a stack written for the current release, skipping every intervening fix and
  migration. Both now pin `v1.9.2`; the three other examples carry no override and already floated
  to the compose default.

- **No fertile window, peak band or ovulation day before the first cycle is complete — on any
  surface.** Until one cycle has been observed, that window is nothing but the onboarding
  cycle-length slider projected forward, and the dashboard header has always withheld it. Four other
  surfaces did not: the calendar grid painted a fertile window, a peak band and an ovulation day; the
  `.ics` feed pushed three ovulation events into subscribed calendar clients, where they outlive any
  correction made in the app; the webhook pass sent an ovulation reminder to the configured endpoint;
  and the dashboard's own reminder banner counted down to a date the header beside it was declining
  to name. All four now stay quiet until a cycle closes, and resume with the first completed one. The
  next-period estimate is unchanged everywhere — it is anchored on a day that was actually recorded
  and carries its own estimate qualifier — and so are recorded observations such as logged period
  days and the BBT signal.
- **One stats page, one cycle history.** A cycle start marked uncertain was honoured by the insights
  ribbon and ignored by the phase cards on the same page, so the page reported one merged 56-day
  cycle in one panel and two 28-day cycles in the other — and the merged length then fed the
  cycle-factor comparison, which labelled that fabricated cycle "longer" and attributed the logged
  factors to it. Both halves now read the same detector, the one that honours an explicit start and
  withholds a cluster whose only explicit start is uncertain.
- **The stats current-phase card states its phase where it can be read.** On a stale cycle the card
  withheld the phase in words only; it now carries the unknown phase and fertility status in the
  same attributes the rest of the page exposes, so the withholding is visible to assistive
  technology and pinnable by a test.

- **`docs/openapi.yaml` mischaracterized `ForgotPasswordRequest.recovery_code`.** The schema's
  pattern required the full `OVUM-XXXX-XXXX-XXXX` shape even though `ForgotPassword` treats an
  absent or blank `recovery_code` — empty, or all whitespace, matching the server's own
  `strings.TrimSpace(...) == ""` test — as the step-1 request and answers it successfully — never as
  a refusal. A client that validates its step-1 body (`email` only, `recovery_code: " "`) against
  the published spec before sending it would have the request rejected by its own tooling even
  though the server accepts it. The pattern now has a `|^\s*$` alternative for that value, and both
  the field's own description and the schema's top-level description now say plainly that
  `recovery_code`'s absent/blank case selects step 1 rather than being refused — unlike `password`,
  whose absent/blank case in step 2 is refused by name.

- **Saving a day during a first tracked cycle no longer announces a fertile
  window.** With a single logged period start no cycle has closed yet, so there
  are no observed cycle lengths and the fertility window is the default cycle
  length projected forward with the default luteal phase — configuration, not
  anything the account recorded. The save confirmation stated it as fact ("You
  are in your fertile window right now"), which is the estimate-presented-as-fact
  the medical-safety rule forbids: where the only source is configuration
  defaults, the surface is withheld rather than qualified. The day save now falls
  back to its neutral confirmation until the first cycle closes; from the first
  completed cycle on, the fertile message is unchanged, as are the self-care,
  pregnancy-paused and unpredictable-cycle messages.

- **Switching the language from the public switcher while signed in now sticks.** The switcher on
  the login and onboarding pages wrote only the browser's language cookie, so for a signed-in owner
  the choice was silently reverted the next time a session was issued — the cookie is re-written
  from the account at every sign-in. A switch made with a session present is now saved on the
  account as well, through the same save `Settings → Interface` uses, and only for a language this
  build actually ships. A visitor with no session is unaffected: the switch stays a cookie, nothing
  is stored, and the answer is the same either way.

- **The late-cycle notice no longer reassures on a cycle its own suppression gate has already
  flagged.** Once the overdue gate (the shorter of the projection and reference lengths, plus a
  week) fires, the notice used to keep comparing the running cycle day against the account's
  recorded maximum, and an account whose maximum was itself a merged span from an unlogged period
  (three 28-day cycles beside one 300-day gap) read "still inside your recorded range of 28 to 300
  days" beside a dashboard that had withheld every projected date since day 36. The notice now
  states the fact the gate is already acting on instead — the cycle has run past the length
  predictions are based on, and predictions wait for a new logged period — whenever the gate has
  fired, rather than a comparison that can land on the very outlier the gate exists to see past.
  The measured-excess and no-personal-range states are unchanged.

- **The two-factor settings page no longer shows a raw translation key.** An account with TOTP
  enabled saw the literal text `settings.2fa.status_enabled_hint` under the "Two-factor
  authentication is enabled" heading, in all six languages, because no catalogue defined that key
  while the page rendered it. The hint is now translated in English, German, Spanish, French,
  Italian and Russian, and reads: signing in requires a code from the authenticator app in addition
  to the password.
- **The English stale-baseline notice on the statistics page reads as a sentence.** It said "Phase
  shown as estimate due stale baseline."; it now says "Phase is shown as an estimate because the
  baseline is out of date." English only — the other five languages already carried a full sentence.

- **A personalized ovulation estimate no longer lands a day before the day your own logs pointed
  at.** Once enough basal-temperature or cervical-mucus entries let the app learn your own luteal
  phase instead of assuming the 14-day model default, that length was measured one day too long: it
  counted the ovulation day itself, which belongs to the phase before it. The prediction built from
  it then sat one day early — the ovulation date and both edges of the fertile window alike — on the
  dashboard, the calendar, the stats cycle ribbon, the API, the webhook reminders and the `.ics`
  feed. An ovulation observed
  on cycle day 15 came back as a prediction for cycle day 14. Learning and predicting are now two
  directions of one arithmetic, so an ovulation observed on a cycle day is predicted on that same
  cycle day when the next cycle runs the same length. Accounts still on the 14-day default, and
  every recorded observation, are unchanged. One side effect is worth naming: the 10-to-20-day
  plausibility window a cycle must fall inside to count is now measured the corrected way too, so a
  cycle that sat exactly on its lower edge no longer counts. An account whose signal only just
  qualified can fall back to the 14-day default rather than shift by a day.

- **Every navigation entry says which page you are on.** Today, Calendar, Insights and the
  bottom bar's settings icon marked the current section with a fill and a weight only, so a
  screen reader read the entries out with nothing to distinguish the page already open. The
  desktop section links and the mobile tab bar now carry `aria-current="page"` alongside that
  fill, exactly one entry per navigation.

- **Enrolling a local password on an OIDC-only account now shows the recovery code it mints,
  instead of dropping the owner on the dashboard with nothing.** The enrollment finishes at the
  provider's callback, which redirected to `/recovery-code`. That page claims the account's
  one-time reveal, and the claim is guarded on Fetch Metadata so only a same-origin initiator may
  spend it. `Sec-Fetch-Site` describes the whole redirect chain, and the callback is a cross-site
  POST the provider makes, so the redirect arrived still labelled off-origin: the guard refused, the
  reveal was skipped, and the owner landed on `/dashboard` — the freshly minted code shown to
  nobody, with no route back to it. She could still regenerate one from Settings, and nothing was
  disclosed, but the code the flow exists to hand over was lost every time.

  The callback now hands the browser a same-origin document that performs the hop, the same
  interstitial the outbound provider hop already uses. The navigation into the reveal is then
  started by this origin, so the label the guard reads is true rather than excused, and a page on
  another site still cannot produce one — the guard itself is unchanged.

  The regression reached `main` because the browser lane that walks this flow is opt-in
  (`E2E_OIDC_PROVIDER=local` with `OIDC_LOGIN_MODE=oidc_only`) and skipped in default CI.

- **`docs/openapi.yaml` now declares `429` (`RateLimited`) on every `/api/v1` operation.** The
  app-wide API limiter (`cmd/ovumcy/server.go`'s `app.Use("/api", limiter.New(...))`) is mounted
  ahead of route registration with no `Next` filter, so it sits in front of all 47 `/api/v1`
  operations — not only the twelve that also carry a narrower per-account budget and were the only
  ones documented. This was the same "N of N+1" shape as the status-reachability gap fixed
  previously: a prior change added `429` where the per-account reauth budget lives and stopped
  there, leaving the shared edge limiter's `429` real but undocumented everywhere else. No server
  behavior changed — this is documentation only.
  - A new test, `TestOpenAPIDeclaresRateLimitedOnEveryLimiterCoveredOperation`, reads the registered
    `/api/v1` routes and fails when any of them lacks a declared `429`, so the gap cannot reopen
    silently.

- **Reminders and calendar events stopped inventing dates for a cycle that had not started.** Once a
  cycle runs past the account's reference length by more than a week, the projection can only roll a
  whole cycle forward at a time — and three surfaces kept following it. The webhook reminder
  announced a "period soon" for a date nothing in the account supported, and because its
  already-sent marker is the predicted date itself, every roll re-armed a new one, repeating about
  once per cycle for as long as the cycle stayed open. The `.ics` feed pushed up to three cycles of
  invented period and ovulation events into subscribed calendar clients, where they outlive any
  correction made in the app. The calendar grid shaded a predicted period in the past — on days that
  came and went with no period logged — and phantom fertile windows ahead of it. All three now stop
  projecting for an overdue cycle, exactly as the dashboard already does; a temperature-confirmed
  ovulation day is recorded rather than projected, and the grid and the feed keep showing it as the
  dashboard does. Logged days, their markers and
  the calendar subscription itself are untouched. When the new cycle start is finally logged, the
  next reminder fires once, on time.

- **An overdue cycle no longer gets a confident next-period window.** The projection rolls forward a
  whole cycle at a time, so it always produced a future date: on cycle day 45 with a 28-day
  reference the dashboard named the anchor plus 56 days as firmly as it names tomorrow, and the
  reminder banner counted down to it. Once a cycle runs past the reference length by more than a
  week — the same threshold that raises the late-cycle notice — no next-period window, ovulation
  estimate or reminder banner is shown at all; one line says the estimate returns with the next
  cycle start. The cycle day, the late-cycle notice and its actions stay exactly as they were.

- **Ovulation-impossibility claim no longer survives fertility suppression.** `GET /api/v1/stats/overview` (and every other surface fed by the shared published-stats adapter) used to publish `ovulation_impossible: true` even while `suppression.fertility` was also `true` — a claim derived from the same fertility projection the suppression withholds. It is now cleared alongside the ovulation date and fertile window whenever fertility is suppressed.

- **The dashboard no longer calls today's ovulation estimate "already in the
  past".** Accounts west of UTC saw the amber "this date is already in the past"
  notice, and the prompt to update their cycle data, printed beside the ovulation
  date the header had just named — on the ovulation day of every cycle, on an
  ordinary account with no irregular mode and no daylight-saving change involved.

- **Breaking (API shape): a password refused for being too long now says so, instead of being called weak.** The maximum
  is bcrypt's 72-byte input limit and is unchanged, but it was enforced through the same error as
  the composition rules and advertised as "8 to 72 characters" in every locale. A passphrase in a
  non-Latin script reaches the cap at roughly half that many characters — a 37-character Cyrillic
  passphrase is 73 bytes — so it was refused with a message it visibly satisfied, listing
  requirements it already met. Length now has its own error, carried through registration, password
  reset, the settings password forms and the JSON API: it names the one fact that helps, that
  letters from other alphabets and emoji count as several, and asks for a shorter password. A
  password that is both too long and missing a character class is reported as too long, because
  shortening is required either way. The weak-password message is back to character classes alone,
  and the hint under the field states the limit once.

- **Onboarding accepts the whole window its own message promises.** The lower bound of the
  last-period-start field was raised to 1 January of the current year whenever that was the later
  date, so on 1 January the accepted window was a single day, and stayed shorter than the promised
  60 through January and February — while the rejection read "Choose a date within the last 60
  days" and onboarding refused to finish without a date. The only way out was to enter a date the
  owner knew was wrong, on the field that anchors the first cycle and every estimate built on it.
  The window is now the last 60 days on every calendar date, and it crosses into the previous year
  when it has to. The date picker offers the same range, because both read the same bound.

- **The corrected luteal-phase estimate now reaches the accounts that can no longer recompute one
  themselves.** Ovumcy learns a personal luteal phase from logged BBT or cervical-mucus signal, and
  it used to measure that phase one day too long: it counted the calendar span from the observed
  ovulation to the next period start, while the prediction consumes the number of days that
  *follow* ovulation. An ovulation observed on cycle day 15 predicted cycle day 14, moving the
  ovulation date and both edges of the fertile window a day earlier on the dashboard, the calendar,
  the API, the webhook reminders and the calendar feed. The computation was corrected, but the
  result is also cached on the account, and that cache is only refreshed when the owner saves a day
  or restores an export. An account whose logs no longer support an inference at all — fewer than
  three recorded cycle starts, or fewer than two cycles with a usable ovulation signal — refreshes
  nothing and would have kept predicting a day early indefinitely.

  The first start after the upgrade now recomputes the stored estimate from each owner's own logs,
  before the server accepts a request. It records a marker once it has been through every account
  without a failure, so it runs once; an account it could not read or write is skipped, counted in
  the startup line, and picked up by the next start rather than left behind.
  Nothing an owner typed is touched: the value has no settings field and no onboarding step behind
  it — it is derived from the day logs alone, and this recomputes it from the same logs the
  dashboard reads. An account with no usable signal lands on the 14-day default the product ships
  with, and an account whose stored estimate already matches its logs is left alone.

  This could not be done in SQL. The repair is not an arithmetic correction of the stored number:
  14 is at once the default, the value written whenever the inference declines, and a plausible
  stale estimate that should now be 13; the plausibility filter that rejects implausible cycles was
  applied to the shifted quantity, so cycles it used to discard now count and cycles it used to
  keep no longer do; and an owner left with too few usable cycles falls back to the default rather
  than to a shifted value. Measured on one such history, a stored 14 recomputes to 18 — four days
  up, from a number indistinguishable from the untouched default. Only re-running the inference
  over the logs finds it.

  A storage failure during the pass does not stop the server: the estimate is a cache with a safe
  fallback, so the failure is logged, the marker is left unwritten, and the next start tries again.

- **A missing `schema_migrations` row no longer costs health data.** The migration runner
  re-applies any migration whose ledger row is absent — a restore from a backup taken before the
  row was written, or a pruned bookkeeping table — and what made that safe was a single skip that
  recognizes `ALTER TABLE ... ADD COLUMN` and nothing else. Migration 003 reconciles `daily_logs`
  by rebuilding it, and its replacement table is the table as it looked at version 003: replaying
  it on a database that later migrations had widened copied the nine columns of migration 001 and
  dropped `mood`, `sex_activity`, `bbt`, `cervical_mucus`, `cycle_start`, `is_uncertain`,
  `cycle_factor_keys` and `pregnancy_test` — eight health columns and every value in them — on an
  instance that was already fully migrated, with the boot reporting success. Migration 024 has the
  same shape and is destructive today only because nothing was added to `daily_logs` after it.

  The runner now refuses a migration that narrows the schema it is applied to, measured two ways
  so neither state of the ledger is uncovered. A recorded LATER version means a migration that
  drops a table is spent, so nothing is executed at all. And — for a ledger lost entirely, where
  no later version exists to compare against — the columns of every table are read before the
  migration's first statement and again after its last, inside the migration's own transaction,
  so a table that ends up without a column it had before fails the migration and every statement
  it ran is rolled back. Reading the whole schema rather than the tables a migration names is
  what makes that check about the effect instead of the spelling: SQLite rebuilds a table either
  by dropping the original and renaming a replacement onto its name, as migrations 003 and 024
  do, or by renaming the original away first and recreating it — the second narrows the table
  just as much while dropping only a name that did not exist when the migration began. Either
  way the database is left exactly as it was, and the refusal names the migration, the table, the
  columns it protected and what to restore.

  Every disappearance a migration means stays expressible, each by something the author writes
  out and names: a column retired by an explicit `ALTER TABLE … DROP COLUMN`, a column moved by
  `ALTER TABLE … RENAME COLUMN old TO new` — which loses nothing, since the values are under the
  new name — and a table retired for good by the marker line `-- ovumcy:removes-table <name>`,
  which authorizes the one table it names. Each authorizes only what it names: renaming one
  column does not excuse a second one going missing beside it. A bare `DROP TABLE` is
  deliberately not read as consent — it is the middle
  statement of every rebuild in the tree, and reading it as consent is how migration 003 came to
  discard eight health columns while reporting success.

  Nothing changes for a clean install or a normal upgrade beyond the time it takes: on both, no
  migration numbered above the one being applied is recorded, and the rebuilds preserve every
  column they copy. Reading the schema between statements costs about 150 ms once, over a clean
  SQLite bootstrap of the whole set (mean 300 ms against 113 ms over twenty runs); an instance
  with no migration to apply pays nothing, because the ledger check comes first. What changes is
  that the one situation in which a boot used to destroy records now stops instead.

- **Breaking (API shape): a date your time zone never had is now refused instead of quietly saving the
  day before.** A zone that crosses the date line skips a whole calendar day
  (Pacific/Apia had no 2011-12-30, Pacific/Kiritimati no 1994-12-31). Entering
  one used to parse as the previous day and report success, so the save, the
  onboarding anchor or the imported row landed on a day nobody named. Date input
  now reports it as invalid, on the same validation path an empty or malformed
  date already takes; an import counts such a row as rejected and imports the
  rest of the file. Dates that do exist are unaffected — a day whose midnight a
  daylight-saving jump skips still resolves to that day's first real instant.

- **Two reminder passes running at the same time no longer send the same reminder twice.** The
  notify pass decides what is due from the snapshot of accounts it read at the start, and it used to
  record "sent" only after the request came back. Two passes that both read before either recorded —
  the built-in scheduler and an `ovumcy notify` run started by hand or by cron, which is a separate
  process and therefore beyond anything the app can lock — each saw an unsent reminder and each
  delivered it, so the owner's endpoint received one health-derived reminder twice. The pass now
  claims a reminder before the request goes out, by a conditional write only one pass can win; the
  pass that loses skips it, and the one that wins delivers. A failed delivery hands the claim back,
  so the reminder is still retried on the next pass exactly as before, and `--dry-run` continues to
  make no request and to write nothing at all. One trade comes with it, now written down in the
  notifications guide: a pass killed outright mid-request — a reboot, an OOM kill, an interrupted
  `ovumcy notify` — skips that cycle's reminder instead of retrying it, because a missed reminder is
  a smaller harm than a duplicate one about health data.

- **The reminder scheduler no longer spins through a full notify pass every few
  milliseconds on a daylight-saving night.** With the daily run hour set to `0`
  and a server timezone whose clock jump lands on midnight (America/Santiago,
  America/Havana), the next scheduled run resolved to 23:00 of the *current*
  day — an instant already in the past. The wait until it was negative, so the
  pass fired immediately, re-listed every account and re-decided its reminders,
  and then computed the same past instant again, repeating from local 23:00
  until the transition roughly an hour later. Outbound webhook delivery was
  attempted on each turn; the per-reminder already-sent watermark kept duplicate
  notifications from going out, but not the work. On such a date the daily pass
  now runs at the first moment the date actually has — the transition itself,
  the same resolution the rest of the app already uses for a calendar day with
  no midnight — so that day's reminders are still delivered rather than skipped.
  Every other timezone and run hour is unaffected.

- **The Insights cycle stack no longer marks a fertile window it cannot locate.** A completed cycle
  shorter than 19 days has no room for a full luteal phase, so the ovulation day behind its fertile
  shading, its peak band and its ovulation cell was a clamped fallback rather than an estimate from
  the account's own data. Those rows now show what was recorded — their length and their period
  days — and nothing inferred; the marks stay on every cycle whose window fits.
- **A day in the cycle stack can no longer be both a period day and the fertile peak.** On a short
  cycle the two landed together and the row said both at once, leaving the display to decide. The
  peak is now the ovulation cell itself, so one day carries one claim.
- **A cycle drawn past the stack's axis says so.** Two cycles longer than the axis were drawn as two
  identical full-width rows while the numbers beside them differed; such a row is now flagged as
  truncated rather than reading as a complete comparison.
- **A temperature entered in Fahrenheit comes back exactly as it was typed.** Readings are stored in
  Celsius, and the stored value was rounded to two Celsius decimals — a step too coarse to represent
  a two-decimal Fahrenheit entry, so 98.61 °F redisplayed as 98.62 °F after every save. Stored
  readings now keep enough precision for the owner's own unit; nothing already stored changes.
- **A cycle start in the previous December can be entered in settings.** The form accepted only
  dates from 1 January of the current year, so in early January the anchor every prediction is
  measured from could not be recorded truthfully. The accepted window is now the last year, measured
  from today; every date the old bound accepted is still accepted.
- **A custom symptom named "Mood" exports as itself.** It was written into the built-in "Mood
  swings" column and left out of the other-symptoms list, so an export carried the wrong symptom and
  a re-import restored it. Export columns are now keyed on the built-in catalog, one column per
  built-in symptom.

- **The per-account logout budget ran at the per-IP number.** Two limiters guard
  `DELETE /api/v1/sessions/current`: an edge limiter keyed on the client address (60 requests
  / 15 minutes) and an identity-keyed budget `AuthService` enforces per account, documented and
  coded as 20 per 15 minutes (that one counts every logout, not only failures). Both read from the single `RATE_LIMIT_LOGOUT_MAX` /
  `RATE_LIMIT_LOGOUT_WINDOW` pair, so the account budget shipped at 60 — three times its
  documented size — and `services.DefaultLogoutAttemptsLimit` never took effect outside tests.
  The two now read from two pairs: the per-IP one keeps its name, the per-account one is
  `RATE_LIMIT_LOGOUT_ACCOUNT_MAX` / `RATE_LIMIT_LOGOUT_ACCOUNT_WINDOW`, defaulted from the
  service constants so the number in the security policy and the number in the code cannot
  drift. Operators who tuned `RATE_LIMIT_LOGOUT_*` to widen the per-IP budget for a household
  instance were widening the account budget with it; that side effect is gone, and the account
  budget is now tunable on its own. Regression: `TestLogoutBudgetsMoveIndependently`.

- **The mood- and symptom-by-phase cards on Insights draw their phase icon
  again.** Both cards passed the phase icon's *name* to the page instead of the
  icon, so each phase heading read "drop Menstrual" or "sprout Follicular", with
  the English name showing through in every language. They now render the same
  sprite icon as the rest of the page, and a template guard fails the build if
  any surface reaches for a phase icon without drawing it.

- **Breaking (API shape): `GET /api/v1/stats/overview` now names the ovulation day the temperatures
  confirm, not the day the cycle model projected before any of them arrived.** A client that reads
  `ovulation_date` as the model's projection has to read `ovulation_confirmed` beside it. When basal body temperature is tracked and
  a sustained thermal shift is detected in the current cycle, that shift confirms an ovulation that
  has already happened. The calendar's solid marker, the dashboard's ovulation line, the stats chart
  marker and the two outbound surfaces (the `.ics` feed and the webhook reminder pass) already
  resolve this through the shared detector; the JSON API kept publishing the model's superseded
  projection instead. `ovulation_date` now carries the confirmed day when one exists.

- **The Insights cycle stack stops claiming a fertile window once predictions are suppressed.** With
  "unpredictable cycle" switched on — or a pregnancy pause, or a cycle running past its own
  reference length — every other surface withholds the fertile window, the peak day and the
  ovulation phase, the calendar's past cycles included. The stack kept shading all three over its
  completed cycles on the strength of the "show historical phases" preference alone, so an account
  that had told the product its cycle math does not describe it still read an ovulation day off the
  stats page. The inferred phase axis now goes dark on the stack as a whole — follicular and luteal
  with it, since both are placed only relative to that same estimated ovulation day — and the
  recorded half is untouched: the rows, their observed lengths and their logged period days stay
  exactly as they were, period days keeping their own colour.

- **The stats BBT chart now shows temperatures in the account's own unit.** The chart, its axis,
  the hover readout, the coverline, the readings table and the text summary on the stats page
  always printed the stored Celsius value under a fixed `°C` label, so an owner tracking in
  Fahrenheit saw 36.5 °C on the stats page for the 97.7 °F they had logged. Every one of those
  surfaces now converts for display and names the same unit; stored readings are unchanged and
  ovulation detection still compares them in Celsius, so the detected shift does not move.

- **Two requests can no longer give one account two symptoms with the same name.** The
  per-owner name rule lived only in application code: the symptom service listed the owner's
  catalogue, compared normalized names in memory, and then issued an unrelated `INSERT`, with
  nothing joining the two. Two requests that both passed the check before either wrote both
  wrote, and the account was left holding two symptoms the day-entry picker, the frequency
  counts and the export cannot tell apart. The built-in seeding path had the same shape on a
  read path — two page loads each list the catalogue, each compute the same missing built-ins
  and each insert them — which is the half a normal session reaches, since it needs only two
  overlapping page loads rather than two overlapping form submissions.

  `symptom_types` now carries a unique index on `(user_id, lower(name))`, so the second writer
  is refused by the database rather than by a check it had already passed. A refused create or
  rename answers with the same "symptom name already exists" conflict a duplicate has always
  produced. A refused built-in seeding does not surface at all — the request that lost the race
  only wanted to read, and it is answered with the catalogue the other one wrote — but only once
  a re-read confirms those built-ins are in fact there now. A refusal that leaves the catalogue
  short is reported, rather than repeating in silence on every page load.

  The index is keyed per account, so two accounts on one instance each keep their own "Cramps",
  and it covers archived symptoms as well as active ones — an archived name has always stayed
  taken for its owner, and restoring an archived symptom re-checks it, so leaving archived rows
  out would have admitted a pair that can never be restored.

  What the index does **not** do is stated in the migration and pinned by a test: it is a
  backstop weaker than the application rule, which also collapses runs of internal whitespace
  and drops invalid UTF-8. Portable SQL can do neither, so "Mood&nbsp;&nbsp;swings" and "Mood
  swings" are one name to the application and two keys to the index, and the application check
  stays in front of it. `lower()` differs between engines as well: PostgreSQL folds by locale,
  SQLite folds ASCII only, so a case-only variant of a non-ASCII name is covered on one and not
  the other.

- **The day-entry picker hides the four built-ins it says it hides.** The set of built-ins kept
  out of the picker was keyed on a spelling of the display name and looked up through a
  normalizer that lowercases and collapses whitespace but never removes it, so the entry
  `moodswings` matched nothing the product can create. Fatigue, Irritability and Insomnia were
  kept out; Mood swings was not, and was merely pushed into the picker's overflow by a second,
  unrelated rule. The set is now keyed on the catalogue's own identity for a built-in, so all
  four are kept out of the list unless the day already carries them, and a key that names no
  built-in fails a test instead of quietly hiding nothing.

- **The segmented date field shows its day/month/year hints in the selected language.** The three
  boxes carried the English `DD`, `MM` and `YYYY` as literal markup, while the label above each box
  and its `aria-label` were both resolved from the catalogue — so on every date-entry surface a
  Russian, German or French account read an otherwise fully localized control with English
  abbreviations inside the boxes it was typing into. The hints now come from
  `date_field.day_placeholder`, `date_field.month_placeholder` and `date_field.year_placeholder`,
  defined in all six catalogues.

- **The calendar's two fertile tiers now render for every usage goal.** The edge days and the peak
  days around ovulation were shaded apart only for the "avoid pregnancy" and "trying to conceive"
  goals, and only in the light theme; the default goal painted the whole fertile window flat. The
  tier fills are written against the base fertile cell now, so the grid shows the same two tiers for
  every goal and in both themes.

- **The settings re-auth checks that gate password change, clear-data, and account deletion no longer
  answer an account with no local password faster than a wrong password.** That branch returned
  before touching bcrypt at all, so response timing told the two apart for anyone already holding a
  session; it now spends a dummy compare at the current hashing cost, and a wrong password whose
  stored hash predates that cost is topped up to match. Because that refusal now costs a real
  bcrypt, it also draws down the re-auth budget like a wrong password, so it cannot be bought
  without limit. The refusals decided by the submission
  itself — a blank field, a mismatched confirmation — deliberately stay free, and are answered
  before the account-state branch so that a blank submission costs the same whichever state the
  account is in: they disclose nothing and draw down no re-auth budget.

- **Three settings/privacy strings no longer fall back to English in `de`, `es`, `fr`, `it`, `ru`.**
  `privacy.third_parties.ledger_link`, `settings.success.webhook_removed`, and
  `settings.error.webhook_url_unreadable` shipped with the English source text copied verbatim into
  all five non-English catalogues; they now carry a native translation matching each locale's
  existing webhook/endpoint terminology.

- **A path spelled in another case, or with a trailing slash, gets the same answer as the lowercase
  spelling.** The router matches paths case-insensitively and ignores a trailing slash, but several
  answers were still chosen on the raw path. `POST /LANG` or `POST /lang/` past the language-switch
  budget answered JSON where `/lang` renders a page fragment. A form error or rate-limit refusal on
  `/API/v1/sessions`, `/api/v1/sessions/`, `/AUTH/OIDC/start` or an `/API/v1/users/current…` form
  answered the JSON envelope where the lowercase spelling redirects back to the form with a flash
  message. `GET /API/…` on an unknown path rendered the HTML 404 page instead of the JSON one. A
  session that has not finished onboarding got `403` `onboarding_required` on
  `DELETE /API/v1/sessions/current` and `/api/v1/sessions/current/`, where the lowercase sign-out
  succeeds. The `scope` field of a rate-limit security event filed those spellings under `api`
  instead of `auth` or `settings`. Each of these now follows the lowercase spelling. Regressions:
  `TestEveryRawRequestPathReadIsRoutingNormalized`,
  `TestRateLimitRefusalsAnswerEveryRoutableSpellingLikeTheLowercaseOne`,
  `TestRespondAuthErrorRedirectsEveryRoutableSpellingOfAnAuthForm`,
  `TestRespondSettingsErrorRedirectsEveryRoutableSpellingOfTheSettingsForms`,
  `TestNotFoundAnswersEveryRoutableSpellingOfAnAPIPathAsJSON`,
  `TestAuthRequiredLetsEveryRoutableSpellingOfSignOutPastTheOnboardingGate`,
  `TestRateLimitScopeClassifiesEveryRoutableSpelling`.

- **The language-switch form's refusals stay readable.** `POST /lang` used to answer a blank
  language, a rejected CSRF token, a server fault or an expired request budget with a raw JSON
  envelope painted into the browser window when submitted as a plain page navigation. Every
  refusal on that route now answers that caller with the same localized error message the form's
  rate limit and failed save already showed, followed by a link back to the page the form was
  submitted from. Status codes and error keys are unchanged. A caller that asks for JSON — through
  `Accept: application/json` or a `Content-Type: application/json` body — still gets the JSON
  envelope, and HTMX requests still get the fragment; a caller that sends `Accept: */*` or no
  `Accept` header at all now gets `text/html` instead of JSON.

- **The edge rate limiters guarding `DELETE /api/v1/sessions/current` now answer `429`, never a
  `303` redirect to the login page.** A plain client (no JSON `Accept`, no `HX-Request`) refused by
  either the per-IP logout row or the app-wide `/api` limiter behind it used to be redirected to
  `/login` instead of getting `429` with `Retry-After`, because the error-mapping switch had no
  case for the endpoint and fell through to its default redirect arm. JSON and HTMX clients
  already got the correct answer for both limiters and are unchanged, including the JSON
  `error_detail.target`, which stays `auth_form`. The per-account sign-out budget, which the
  handler enforces after the session has already ended, still sends a browser to `/login`.

- **Linking or unlinking an OIDC identity, or clearing your data, no longer reports itself as
  failed when only the follow-up session re-issue could not be carried out.** If the change
  committed but a concurrent revocation or a session fault kept this device from being
  re-issued a session past it, the owner used to see "failed to create session" or "failed to
  clear data" — a message that is false, since the change already went through. Both cases now
  say so and ask the owner to sign in again.
- **A settings change refused after this device was signed out now lands on the sign-in page
  with its message.** Linking an identity, unlinking one, clearing data (by password or by
  provider step-up) and setting a local password each clear the session cookie when a
  concurrent revocation wins or the session cannot be re-issued. Their answer used to redirect
  to the settings page, which bounced to sign-in and dropped the message, or — for a
  password-confirmed clear-data sent from a plain form — rendered a raw JSON error. A browser is
  now redirected to `/login` with the message, and an HTMX request gets `HX-Redirect: /login`
  instead of an inline banner on a page that no longer has a session. The same now holds for a
  password change, a recovery-code regeneration, and turning two-factor authentication on or
  off, which used to answer the settings form with an error envelope after signing it out.

- **An expired or rotated CSRF token on the sign-in, registration, forgot/reset-password, or 2FA
  challenge form no longer paints the raw JSON error response into the browser.** A plain (no
  HTMX, no JavaScript) form submission on one of those routes now answers with the same readable
  status page the language switcher already uses, with a stable key next to the localized message
  and a link back to the actual page the form was submitted from (never "/") — whichever rejection
  produced it: a transport-level one reached before the form's own handler runs (CSRF, a
  too-large body, a request-deadline timeout), or a handler-layer internal error the handler
  itself builds (a session/token/account write failing). A caller asking for JSON, or submitting
  through HTMX, keeps its existing answer unchanged — including on that same internal error.
  Logout's own answer is unchanged and is being decided separately.

- **The `429` API docs now say when `retry_after_seconds` is missing.** Login and password-reset can be refused by a per-account attempt lockout that carries no refill timer, not only the per-IP rate limiter — the shared `RateLimited` response schema now documents both cases instead of promising the field unconditionally.

- A refusal rendered before the request's language was resolved — most visibly a POST /lang form
  submission or an HTMX request whose body or head exceeded the server's limits (413/431) —
  showed its status page with the raw machine key (`request_too_large`, `common.back`) in every
  locale instead of the owner's language. The shared status-fragment renderer now resolves the
  request's own locale catalogue before rendering, whichever layer produced the refusal, so the
  page reads in the owner's language as a refusal produced after routing already did.

- **Day states stay readable under a Windows contrast theme.** Forced colours repaint fills,
  shadows and border colours in the system palette, which erased the period toggle's thumb, the
  logged-day dot, and the difference between recorded-period, fertile and plain calendar days (the
  legend swatches went blank the same way). Each state is now restated in border geometry the theme
  keeps — thick solid for a recorded period, dashed for a predicted one, dotted for the start
  window, double for the fertile window, an inner ring where a prediction falls in the window, a
  firm solid line before the fertile window and a thin solid line with a dotted inner ring for a
  tentative ovulation day, so no fertility day looks like a period day — and the legend draws the
  same shapes for the states it lists. The toggle draws itself in system colours, today and the
  selected day get outline rings, and the dashboard cycle ribbon and the Insights cycle stack keep
  their phase colours.
- **Printing drops the interface chrome.** The header, the fixed mobile tab bar, toasts and the
  install offer no longer print, and a dark-theme page prints dark ink on a white page.
- **The dashboard's quick jumps honour reduced motion.** Jumping to the mood or symptom section
  scrolled smoothly regardless of the system's reduced-motion preference; it now jumps instantly
  when reduced motion is requested, as the link-target scroll already did.

- **The Postgres restore in `docs/self-hosted.md` no longer promises an export that the procedure
  cannot produce, and its "changed nothing" case now says what it does change.** Running the
  documented commands settled both. A dump taken after the documented restore is never
  byte-identical to the one that was replayed: `pg_dump` 17.6+ wraps every dump in a fresh random
  `\restrict` token, and dropping and recreating `public` leaves a schema `initdb` did not create,
  so every later dump carries an `ALTER SCHEMA public OWNER` / `COMMENT ON SCHEMA public` /
  `REVOKE USAGE ON SCHEMA public` block the earlier one did not. Neither is data, and an operator
  who verified a restore with a byte-for-byte `diff` — which the old wording invited — would have
  read a good restore as a failed one. The runbook now names both differences and says to compare
  the `COPY` blocks and `setval` lines instead. The failure row of the same section is corrected
  too: replaying a dump into a database that still holds its schema restores no row, but `setval`
  is the one statement that still succeeds, so every sequence is rewound to the backup's value and
  the next insert lands on an id that is already taken.

- **A `HEAD` request now reaches the page or endpoint it names, instead of the 404.** Every address
  the app serves on `GET` — the sign-in page, the dashboard, `/healthz` and `/readyz`, the `/api/v1`
  reads, the calendar feed's `.ics` URL — answered `HEAD` with "not found", because the framework
  adds its `HEAD` counterparts only at startup and they landed behind the catch-all that answers
  unknown addresses. Uptime checks, link checkers, reverse-proxy probes and calendar clients that
  ask for headers before downloading all saw a dead site. They now get the same status and headers
  the matching `GET` returns, with no body, and an address that really does not exist still answers
  404 on either method. Pages that show a secret once, or mint one-time auth material a redirect
  would carry — the recovery code, the freshly minted subscribe URL, the registration hand-off,
  starting sign-in with a provider, beginning two-factor setup — deliberately keep answering
  `HEAD` with 404: a headers-only request there would spend the single use with nobody able to
  see or follow it. The one-time notice a page shows after a redirect (a sign-in error, a
  forgotten-password prefill) is left untouched by `HEAD` too, so the browser's own next visit is
  still the first to read it.

- **Onboarding and the cycle-start control work without JavaScript and no longer put their fields
  in the address bar.** Both onboarding steps and the manual cycle-start form on the dashboard and
  the calendar day panel declared no method or action, so a browser without JavaScript submitted
  them as a GET to the current page: the CSRF token, the timezone, the last period date and the
  cycle lengths landed in the query string, and nothing was saved. Each form now posts to its own
  endpoint, and the browser is redirected with 303: to the next onboarding step, to the dashboard
  after onboarding, and back to the dashboard or the calendar day after a cycle start is marked.
  Without JavaScript, onboarding shows a plain date field in place of the date picker, and
  replacing an existing cycle start or marking one after a short gap asks for confirmation with a
  checkbox in place of the dialog. A refused submission from a browser without JavaScript shows the
  localized error with a link back to the form, under the same status code, instead of the JSON
  error body. API clients get the same responses as before.

- **The implantation-bleeding hint waits for a completed cycle.** Marking an early bleed as a new
  cycle start used to offer the "this may be implantation bleeding" caution even for an account
  with a single recorded cycle start, where the ovulation it counts the days from is the configured
  cycle length rolled forward rather than anything observed. The hint now follows the same
  first-cycle floor the fertile-day save message, the calendar grid, the calendar feed, the webhook
  reminder and the dashboard banner already follow, and reappears once one cycle has completed.

- **Unchecking a period start no longer erases a flow you logged by hand on a later day.** With
  automatic period fill on, unchecking the start day also clears the following days the fill had
  created, stopping at the first day carrying anything you entered. A flow (including spotting)
  was not counted as something you entered, so a hand-logged heavy day right after the start was
  cleared with it. A flow now stops the clearing unless it is the very flow the fill copied from
  the start day.

- **A day edit already being saved is no longer lost when the page is reloaded or closed.** The
  dashboard journal's autosave now rides a keepalive request, so a save the page has started
  outlives the page instead of being cancelled with it. A calendar Save still waiting on the server
  is re-sent on a keepalive request as the page leaves, carrying the same entry. While a save is
  open, leaving the page asks for confirmation first.

- **The deployment documentation's commands and examples now work on the published example stacks.**
  The operator CLI and probe commands use `docker compose exec ovumcy …` (`-T` for a piped
  password) instead of `docker exec ovumcy …`, because only the root compose file names its
  container `ovumcy`. Both nginx example configs set `client_max_body_size 16m`, so a JSON restore
  within the app's 16 MiB limit is no longer refused by nginx's 1 MB default. The self-hosting guide
  now states that a bind-mounted data directory must be owned by UID/GID 10001, and that
  `POSTGRES_PASSWORD` must be URL-safe because it is placed unescaped into `DATABASE_URL`.
  No runtime behavior change.

- **A temperature shift landing after the expected next-period date is now confirmed on every
  screen, not only on the stats chart.** The calendar's solid marker, the dashboard's ovulation line
  and the JSON API stopped confirming a late shift because the shared detector's window was cut off
  at the model's own projected next period start; the stats chart used a different, correct window
  and kept showing it. Those three now name the day the chart names. The fertile window, the
  current-fertility status and the dashboard ring still follow the projection; moving them with a
  confirmed day is a separate change. The situations in which no ovulation date is shown at all —
  an overdue cycle, unpredictable mode, a pregnancy pause, the first cycle — are unchanged, and the
  webhook reminder and the calendar subscription send exactly what they sent before.

- An owner who has not finished onboarding can sign out: `POST /logout` is no longer redirected
  back to `/onboarding` with the session alive, and the onboarding page carries its own sign-out
  control. The sign-out confirmation now says that it also ends the account's sessions on every
  other device, which it always did.

- **The Docker Hub mirror no longer fails its own signature check by asking too early.** The release
  job signs the mirrored digest on Docker Hub and reads that signature back before it writes a
  public tag there. Docker Hub accepts the signature before it lists it: the read-back ran about a
  second after the write, found nothing, and failed the job with the image mirrored but untagged —
  so no unverifiable tag was published, and no verifiable one either. The read-back now waits for
  the registry's listing to catch up, up to a minute, and still fails the job when the signature
  never appears.

- **`scripts/mutation.sh` no longer reports success for a baseline run that mutated nothing.**
  `baseline <slug>` exits non-zero and lists the valid plain and shard-form slugs when the slug
  matches no registered target, instead of printing "baseline JSON written" having invoked gremlins
  zero times. A shard number written with a leading zero (`internal_api_08`) is refused rather than
  silently accepted: the range test failed on the octal literal inside an `if`, where `set -e` does
  not apply, so the shard was admitted and then run as shard 8 modulo the shard count — a different
  slice under the requested name. The accepted set now matches the one the error message
  advertises. A registered sharded package with no non-test Go source is refused at the baseline
  site as well as in `verify-shards`, because an empty exclusion list turns a shard run into a full
  unsharded run of the package under that shard's name; and `verify-shards` now reports a
  registry entry whose directory is missing apart from one whose directory is empty.

- **A refused account or two-factor form shows a page with a way back, without JavaScript.** When the
  profile, password, recovery-code, SSO link or unlink form on the settings page, or the two-factor
  enrol or disable form, is submitted without JavaScript and refused before it is handled (an expired
  CSRF token, for instance), the browser showed the raw JSON error. It now shows the localized message
  and a link back to the page the form was on. A wrong second-factor code on the enrol form, and a
  wrong password on the disable form, read the same way. API clients and htmx get the same responses
  as before.

- **A refused erasure or egress form shows a page with a way back, without JavaScript.** When the
  delete-account, clear-data, step-up, webhook or calendar-feed forms on the settings page are
  submitted without JavaScript and refused before they are handled (an expired CSRF token or a lost
  session, for instance), the browser showed the raw JSON error. It now shows the localized message
  and a link back to the settings page. API clients and htmx get the same responses as before.

- **A refused day form shows a page with a way back, without JavaScript.** When the day editor, the
  dashboard day form, the usage-goal switch or the cycle settings form is submitted without
  JavaScript and refused before it is handled (an expired CSRF token, for instance), the browser
  showed the raw JSON error. It now shows the localized message and a link back to the page the form
  was on. A day entry whose values are invalid answers 422 with the same page instead of a bare
  error. The message for an invalid day no longer claims a save failed, since a read or a delete of
  a malformed date shows it too. A day save or delete, or a cycle-settings save, that the server
  fails says so in the person's language instead of showing an internal message. API clients get the
  same responses as before, and htmx keeps its status and fragment.

- **A refused settings form shows an error page with a way back, not raw JSON, in a browser
  without JavaScript.** When the reminders, interface, tracking or symptom forms were refused
  before their handler ran (an expired page token, or too many requests on a symptom form), a
  browser without JavaScript painted the JSON error body as the page. It now shows the localized
  error under the same status code, with a link back to Settings. Too many requests on the
  reminders, interface or tracking form still returns to Settings with the message, as before. API
  clients get the same responses as before.

- **The refusals for a period, a pregnancy test or a restored file recorded too far ahead name the
  bound the server enforces.** The copy read the number from a literal in six languages; it now
  takes it from the same constant as the check, with the noun in the plural form that number needs.

- **A day entry refused for an ended session can be saved after signing in again.** When a
  dashboard autosave or a calendar day save is refused because the session has ended, the notice
  now carries a localized "Sign in in a new tab" link beside its retry. Signing in in that tab
  leaves the typed entry on the original page, and the retry then saves it; nothing from the entry
  is stored in the browser along the way. If a different account signs in in that tab, the retry
  is refused with "You're now signed in to a different account. This entry was not saved." and
  writes nothing: every day write a page renders (save, delete, the dashboard undo, cycle start)
  carries an opaque binding to the account that rendered it, and a write whose binding names
  another account — or a write from a page that carries none — answers 409. A page left open
  while another account signs in elsewhere in the same browser can therefore no longer save,
  delete or mark a cycle start in that account. The calendar, whose day editor is fetched rather
  than rendered with the page, sends the account it was rendered for on every request from inside
  it: a calendar tab left open while another account signs in answers the day fetch and the grid
  refresh with a refusal ("This day can't be opened: you're now signed in to a different
  account. Reload the page.") instead of showing that account's day; a refused grid refresh
  keeps the grid on screen with that notice above it. JSON API clients that do not send the
  binding are unaffected. If the page's security token went stale while the owner was signing in
  (it idled out, or the server restarted), the refused retry says "This save was refused. Your
  entry is still here — try again." rather than asking for a reload, re-reads the token from the
  page, and the next retry saves.

- **A re-authentication at a provider on another site now finishes the action it was asked for.**
  Linking an SSO identity, setting a first local password, clearing data and deleting the account
  all send the owner to the provider and complete when it sends her back. When that provider lives
  on a different site and returns the answer as a form post, the browser withholds the session
  cookie on the way back — that is what `SameSite=Lax` is for — so the app could not tell whose
  re-authentication had just arrived and refused all four with "that re-authentication does not
  match the account signed in here". The return now lands on a short same-site page that carries the
  answer forward: the app reads the session there exactly as it always did, and no cookie was made
  reachable from another site to achieve it. That page's own hand-off is single-use, expires in a
  minute, is scoped to one address, and that address turns away any request another site starts. A
  refusal comes back the same way: when the provider declines, or sends back something that does not
  answer the request that was made, the owner arrives on the settings page and is told why — on every
  browser, rather than only on those that treat a redirect begun elsewhere as still being her own
  visit. A provider on the same site is unaffected.

- **An abandoned re-authentication no longer blocks the next sign-in.** Starting one of those
  settings actions and then leaving the provider without finishing left a note in the browser that
  said "a re-authentication is in progress". For the next ten minutes that note answered every
  attempt to sign in with SSO — the sign-in came back from the provider, was mistaken for the
  abandoned action, and was turned away with nothing shown on the page. Starting a sign-in now
  discards the abandoned action, and so does signing out.

SSO sign-in works with identity providers that send the ID token's
`email_verified` as the string `"true"` or `"false"` rather than a JSON
boolean, as AWS Cognito does; such a sign-in used to fail outright. Only those
two exact strings are accepted. An absent or `null` claim still means the email
is unverified, and any other value still fails the sign-in.

- **SSO sign-in works with providers that send the email only from UserInfo.** Ovumcy read the
  email address and its verification flag from the ID token alone. OpenID Connect delivers those
  claims from the provider's UserInfo endpoint in the authorization code flow, and a provider may
  leave them out of the ID token entirely, as Authelia 4.39 and later do by default. Every first
  sign-in through such a provider was refused as an unavailable account, whether it would have
  created an account or matched an existing one. When the ID token carries no verified address, a
  first sign-in now reads the address and its verification flag from the UserInfo endpoint. An ID
  token that does carry one is used as before, with no UserInfo request, and a sign-in through an
  already linked identity never asks UserInfo either.

- **Breaking (API shape): onboarding step 2's refusal of the removed `age_group` field now covers every shape the
  endpoint accepts, not only a form body and a JSON string.** The presence check told "the field is
  not there" apart from "the field is there but malformed" by inspecting the outcome of a typed
  decode, which reads a JSON number or `null` for `age_group` as absent (the typed probe fails with
  a type error, not a missing-field error), and only ever inspected `PostArgs`, which fasthttp never
  populates for a multipart body or for a value the client put in the URL's own query string. A
  client using any of those four shapes was told `200` with the field silently dropped — the exact
  removed-field-reads-as-success outcome the original refusal exists to prevent, one spelling over.
  Presence is now decided from the raw request the same way for every shape: the parsed JSON object's
  own keys for a JSON body, and the URL query string, the urlencoded body, and the multipart form in
  that order otherwise — the same three sources `FormValue` itself reads from. Two narrower routes
  around the same guard are closed alongside it: the query-string check now runs unconditionally
  before branching on Content-Type, so a JSON request carrying `age_group` only in its URL (not its
  body) is caught too; and the JSON-key match is now case-insensitive, matching the fallback
  `Bind().Body` itself uses, so a body naming `Age_Group` or `AGE_GROUP` — exactly the off-spec
  casing a client still on the old contract might send — is refused rather than silently dropped.

- Finishing onboarding now stores the language the onboarding pages were shown in on the account when none was stored yet, so calendar subscriptions and notifications no longer fall back to English for an owner who never opened the language setting. The same step starts the week on Monday for German, Spanish, French, Italian and Russian accounts; English keeps Sunday, and existing accounts keep their week start.
- In Settings → Interface, an account with no stored language can now save the language already on screen: Save stays enabled and a note explains that the language is not saved to the account yet.
- A browser that marks a language as unwanted (`q=0` in `Accept-Language`) no longer gets the interface in that language.

- **A subscribed calendar and a webhook reminder no longer announce an ovulation your temperatures
  have already placed.** When a sustained thermal shift is recorded, the dashboard, the month grid
  and the stats chart all move onto the day the temperatures name. The two surfaces that leave the
  instance did not: the `.ics` feed kept publishing an ovulation event on the projected day, and the
  reminder sent to your webhook endpoint kept counting down to it.

  Both exist to announce a day that is still ahead, so neither could simply be moved onto a day
  already behind you. They now stay quiet for that cycle instead. The gap was widest exactly where
  it mattered: on the projected day itself, the app on screen said the ovulation had happened three
  days earlier while a calendar you had subscribed to marked it as today.

  Only the cycle the shift belongs to is affected. The next projected cycle's ovulation event and
  its reminder are unchanged, as are every period event and period reminder, and a cycle with no
  thermal shift recorded behaves exactly as before. The suppression rules already in force —
  predictions turned off, a pregnancy pause, an overdue cycle, or no completed cycle yet — are
  untouched, and both surfaces still read them from the same place every other surface does.

- **`ovulation_exact`'s description no longer claims it is a personalisation signal.** The flag
  reports exactly what `CalcOvulationDay` decides: whether the luteal phase fit inside the cycle
  without the arithmetic clamp. It is `true` on the 14-day model default whenever that default
  happens to fit a cycle, and `false` on a per-owner inferred value the cycle was too short to hold —
  the opposite of what the wording said. It remains independent of `ovulation_confirmed` and always
  `false` when `ovulation_date` is null; only the misleading half of the description changes.

- **Ovulation inferred from your own observations no longer lands a day early on a clock-change
  date.** Both signals Ovumcy reads an ovulation from count a calendar day forward from something
  already known: the thermal shift names the day before the first elevated temperature, and the
  cervical-mucus fallback names the day after the last fertile-quality day. Both counted that day
  inside the timezone the page was being viewed in. In the zones west of UTC whose daylight-saving
  jump happens exactly at midnight — Santiago on 6 September 2026, Havana on 8 March 2026 — that
  local midnight does not exist, and a count landing on it resolves backward into the day before.
  The inference then named 5 September for an ovulation it had found on the 6th.

  Two things read that answer, and on those dates both were a day out: the personalised luteal
  phase learned from past cycles, and — now that the month grid marks a confirmed ovulation on the
  day the temperatures point at rather than on the earlier projection — the solid ovulation marker
  itself. The count now runs over calendar days anchored at UTC, which no timezone shifts, exactly
  as the month grid's own bounds already do. Nothing moves on any other date, in any zone: the
  value has only ever been read as a calendar date, never as a clock time.

- **A day that is both an expected period day and a fertile day now says so.** On a short cycle with
  a long average period the projected period band and the fertile window cover the same calendar
  days — bleeding inside the fertile window is ordinary rather than an anomaly, and both statements
  about such a day are true at once. The month grid paints one fill per cell, and the band's fill
  outranked the window's, so those days were shown as expected bleeding only and the fertile window
  appeared shorter on the calendar than it actually was.

  Those cells now carry a fill of their own — the two hatches woven together, dashed like the
  projected band and drawn in the fertile ink — with its own entry in the calendar legend. The days
  the next period may start on get the same fill with their usual dotted stroke wherever the fertile
  window reaches them, whether or not bleeding is projected on them as well — on an irregular cycle
  that window of possible start days runs for weeks and covers fertile days the projected bleeding
  never touches — so the fertile window is drawn without a gap. The fill follows the calendar's goal
  colours, so it warns or encourages exactly as the rest of the window does. Nothing is hidden to
  make room for it: the day remains both an expected period day and a fertile day everywhere else it
  is read, and every cell that carries only one of the two keeps exactly the fill it had.

- **The period-off question keeps following a day save when its feedback cannot be read.** Un-marking
  a period day asks first when the un-tick would move the cycle start or clear the auto-filled
  days after it. A save whose follow-up feedback failed to load left the open form with the question
  it was rendered with, even when the save itself changed what that question should say; the
  question is now recomputed from the saved day in that case.

- **The 40-45 and 45+ age-group labels no longer both claim age 45.** All six locales showed
  `40-45` next to `45+`, so a woman exactly 45 literally fit either radio button even though they
  are mutually exclusive (`components/settings_cycle.html`). The only reader of the bracket,
  `shouldShowStatsPerimenopauseHint` (`internal/services/stats_page_view_service.go`), treats them
  as disjoint — it shows the STRAW+10 perimenopause note only for `age_45_plus` — so which button a
  45-year-old picked silently changed the medical guidance she saw. The AWHS cohort the bracket
  comment cites uses non-overlapping 40-44 / 45-49. Copy only: `40-45` → `40-44` (French:
  `40–45 ans` → `40–44 ans`) in `de/en/es/fr/it/ru`; the stored value `age_40_45` and the `45+`
  label are unchanged, so no migration is needed.

- **Docs now cover restoring a backup that predates a `clear-data` or account-deletion request.**
  `docs/self-hosted.md` documented the analogous calendar-feed resurrection case but never mentioned
  erasure at all: neither operation leaves a tombstone in the database, so a restore from a
  pre-erasure backup silently undoes the erasure with no signal that it happened. Added step 8 to
  Post-Restore Verification and a new `docs/gdpr.md → Backup Restore and Erasure` section telling
  operators to track erasure requests outside the database and re-apply any that postdate a
  restored backup. Documentation only, no code changed.

- **`docs/openapi.yaml` now declares every status each operation's own handler chain can answer
  with.** Thirty-two declarations were missing across twenty-eight operations, all pre-existing;
  none is a v2.0.0 breaking change, and no server behavior moved. The gap was structural: the
  existing contract test proves `declared ⊆ emittable` over the whole server at once, so a status
  emittable *somewhere* satisfied it even on an operation that never declared it.
  - Fifteen operations answer `403` and did not say so — eleven behind the owner-only gate
    (`GET /api/v1/days`, `GET /api/v1/days/{date}`, `GET /api/v1/symptoms`, `GET
    /api/v1/stats/overview`, `GET /api/v1/users/current`, `DELETE /api/v1/users/current`, `DELETE
    /api/v1/sessions/current`, the three onboarding steps, `POST
    /api/v1/users/current/data-wipe/validate`), and four on the deployment gate that turns the
    local-credential surface off in an SSO-only install (`POST /api/v1/users`, `POST
    /api/v1/sessions`, `POST /api/v1/password-resets`, `POST /api/v1/password-resets/redeem`). The
    `Forbidden` component's description now covers the second cause too.
  - Ten operations answer `400` on input they parse themselves and did not declare it: the three
    `/api/v1/exports/*` range parameters, `DELETE` and `HEAD /api/v1/days/{date}` on the path date,
    the `interface` / `tracking` / `reminders` settings bodies, `POST /api/v1/onboarding/complete`,
    and `POST /api/v1/sessions`.
  - `POST /api/v1/users/current/recovery-code` and `PUT /api/v1/users/current/2fa` share the
    per-account re-auth budget with the four endpoints that got their `429` in the previous
    release, and answer the same `429`.
  - The three step-up endpoints (`POST /api/v1/users/current/{password,data-wipe,deletion}/step-up`)
    answer `503` when the identity provider they must re-authenticate through is disabled or
    unreachable. A `ServiceUnavailable` response component carries it.
  - Three auth endpoints documented only their browser redirect and never their JSON success
    answer: `POST /api/v1/users` returns `201`, `POST /api/v1/password-resets/redeem` returns
    `200`, both with the new `NextStepResponse` shape (`ok`, `next_step`, `next_path`), and `POST
    /api/v1/password-resets` returns `200 {ok: true}`. A programmatic client had no documented
    success status on any of the three.

  A new test walks each `/api/v1` route's registered handler chain as Go AST and fails when the
  chain can reach a `fiber.Status*` the spec does not declare for that operation, so the class
  cannot reopen silently.

- **Breaking (API shape): a basal body temperature written in Fahrenheit via the JSON API now
  converts, same as the form, so `bbt` on a write is read in the account's own unit.** A client
  that sends Celsius for a Fahrenheit-preference account has to send Fahrenheit instead.
  `PUT /api/v1/days/{date}` accepts `bbt` from both a browser form post and a JSON body; only the
  form path converted the value into the account's preferred unit before storing it. A Fahrenheit-preference
  owner posting JSON (`{"bbt": 98.6}`) had it validated straight against the Celsius physiological
  range and refused — no bad data was ever stored, but every JSON write of BBT failed for her. The
  JSON path now converts through the same unit-aware helper the form path uses. `docs/openapi.yaml`
  also now states that a read of `bbt` is always Celsius, regardless of the account's write-side unit.

- **`docs/openapi.yaml` now matches three places where the server's actual behavior had drifted
  from the published contract.** All three were pre-existing since v1.9.2; none is a v2.0.0
  breaking change.
  - Four settings endpoints that share the per-account re-auth budget — `DELETE
    /api/v1/users/current`, `PUT /api/v1/users/current/password`, `POST
    /api/v1/users/current/data-wipe/validate`, `POST /api/v1/users/current/data-wipe` — can answer
    `429` once the budget is spent, but the spec never declared it. All four now do.
  - `ForgotPasswordRequest` (`POST /api/v1/password-resets`) required `recovery_code` and
    `password` even though the handler is a two-step flow: step 1 sends `email` alone and gets
    `next_step: recovery_code` back, without starting anything. Only `email` is schema-required
    now, and the schema documents both steps. The endpoint's declared `401` was also unreachable —
    `mapPasswordRecoveryStartError` has no arm that returns it, every step-2 rejection is a uniform
    `400` — so the response now documents that instead.
  - `docs/openapi.yaml` declares `openapi: 3.1.0` but used the OpenAPI-3.0-only `nullable: true`
    keyword on nine properties (`DayPayload.bbt`, `DailyLog.bbt`, `Symptom.archived_at`, five
    `StatsOverview` projection fields, `ExportJSONEntry.bbt`), which fails structural validation
    under 3.1's JSON Schema base. Each now uses the 3.1 form, `type: [<type>, "null"]`.
    `npx @redocly/cli lint docs/openapi.yaml` is green.

- **A pregnancy-test result refused on a future day now says so.** Saving a test result on a day
  more than two days ahead is refused, but the message spoke of marking a cycle start, which the
  save did not ask for. It now explains that a pregnancy-test result can be recorded only for past
  days and up to two days ahead — in the day editor with or without JavaScript, and as its own
  error key (`invalid pregnancy test day`) from `PUT` and `PATCH /api/v1/days/{date}`. A period
  saved that far ahead keeps the cycle-start message.

- **`GET /api/v1/stats/overview` no longer publishes an ovulation day that has already passed.** Once
  the running cycle's projected ovulation was behind today, the dashboard, the calendar and the
  `.ics` feed rolled it to the next cycle while the API kept naming the passed day, flagged exact,
  with the fertile window around it. `ovulation_date`, `ovulation_exact`, `ovulation_impossible`,
  `fertility_window_start`/`fertility_window_end` and `next_period_start` now come from the same
  projection the pages use, so the API names the same days they do. `current_fertility` and
  `fertility_basis` are read against that published window: `unknown` (basis null) when the rolled
  day falls past 9999-12-31 or cannot be placed, and `fertile` when the rolled window already
  covers today. `current_phase` is then `unknown` beside a null `ovulation_date` (or `menstrual`
  on bleeding) and on a day the rolled window calls fertile. In irregular-cycle mode, where
  the dashboard shows the running cycle's ovulation range, the API keeps that cycle's day and
  widened window. A BBT-confirmed ovulation and every suppression rule are unchanged.

- **The README now states the Go version the project actually builds with, and a guard keeps it
  that way.** The badge and the build prerequisites still said Go 1.26.6 after the toolchain moved
  to 1.27.0 and then 1.27.1, so anyone building from source was told a version that no longer
  matches `go.mod` or the builder image. Both lines are now compared against `go.mod`'s `go`
  directive by `scripts/readmeversion`, which is the only thing that can catch this: every workflow
  resolves its own toolchain with `go-version-file: go.mod`, so CI stays green whatever the README
  claims.

A form submitted without JavaScript after the session ended — a calendar or
dashboard day save or delete, the cycle-start mark, an onboarding step, or one of
the settings forms — no longer lands on a bare refusal page with no layout. The
browser is sent to the sign-in page, which shows the "not signed in" notice in the
interface language. Any other refusal of those forms — an expired form token, a
value the form cannot save, a server error — keeps its status, its message and
its link back to the form, and is now shown as a full page in the app's layout
and in the interface language. The same holds for a refused sign-in, registration,
password-recovery or two-factor form and a refused language switch: the same
status, message and link back, shown as a full page instead of a bare fragment.
htmx requests and JSON API clients keep their status and the same body as before.
Every such page carries the app's usual security headers, including the refusal of
a form body over the upload limit, and a return address longer than 2048 bytes is
dropped for the default destination rather than echoed back.

- **Webhook reminders and the calendar feed now speak the owner's language.** Both surfaces that
  carry predictions off the instance resolved the mandatory "estimates, not medical advice or a
  method of contraception" disclaimer at the server default, and the webhook headline and sentence
  were English literals with no catalogue entry at all — so an owner on one of the five non-default
  locales received a notification written in two languages, neither of them necessarily theirs. The
  notify pass and the `.ics` feed now resolve every localized field at the owner's chosen interface
  language, and the reminder copy lives in all six locale catalogues.

- **Saving a Positive or Remove pregnancy-test result on the dashboard now updates the page in
  place.** The field still read "No result recorded" with no Remove action after a Positive
  result, the status header and the period-reminder banner kept their old estimate until a reload,
  and Remove dropped keyboard focus to the page body. The field now follows the click, the status
  header is refreshed from the dashboard once the result is saved (only the blocks that depend on
  the result are replaced, so the goal switch beside them keeps working and an open goal menu stays
  open), and focus moves to the first result control when Remove hides its own button. Undo puts
  the field back with the value it restores, a refresh keeps keyboard focus on the same link
  inside the refreshed block, and the status line announces the change to screen readers.

- **A period or a pregnancy-test result can no longer be recorded on a day that has not happened.**
  A day save — the dashboard and calendar forms, with or without JavaScript, and `PUT` or `PATCH
  /api/v1/days/{date}` — accepted a period or a test result on any future date, and the calendar
  then showed it as recorded, while marking a cycle start more than two days ahead was already
  refused. Both are observations, so a save that turns the period on or records a test result
  past today plus two days (in your timezone) is now refused with the same message as a cycle
  start that far ahead. Saves that record neither — a note, a symptom, or clearing a period or a
  test — still go through on any date, and entries already stored ahead are kept as they are and
  stay editable.

- **A release tag can no longer be published off a green that ran nothing.** Before a `vX.Y.Z` tag
  builds and signs an image, a gate re-derives the verdict CI left on the tagged commit: it must be
  contained in `main`, and the five checks that hold the rolling `:latest` back — `test`, `race`,
  `e2e`, `e2e-postgres-smoke`, `image-smoke` — must have passed on it. A tag push starts no CI run
  of its own and branch protection guards merges rather than tags, so this gate is the whole of the
  release path's judgement.

  It was reading three of those five in the one place where they carry no verdict. A commit on
  `main` is judged twice — once by the merge queue on the identical revision, once by the push that
  follows the merge — and the push run deliberately clears the unit, race and browser lanes,
  because the queue has just run them on that exact tree. Their gate jobs still report, and still
  report green, in a handful of seconds with every lane beneath them skipped. The gate consulted
  only the push run, so `test`, `race` and `e2e` cleared a release tag on a result that says
  nothing about the commit, and would say the same for any commit. It also accepted `skipped` from
  all five alike, which let a lane that never ran stand in for one that passed. The two readings
  contradicted each other: the queue's verdict was refused as untrustworthy, and then a push-run
  green whose only justification was "the queue already ran it" was accepted in its place.

  Each check is now read where it actually executes. `test`, `race` and `e2e` are taken from the
  merge-queue run on the same revision, and a commit that reached `main` without one is refused.
  `e2e-postgres-smoke` and `image-smoke` are the lanes the queue does not run, so they are taken
  from the push, release or manually dispatched run. `success` is the only conclusion that clears a
  tag in either half — `skipped` and a still-running check are both refusals now — and a failure
  anywhere still fails the workflow with no bypass, so a tag that cannot be verified does not
  publish.

  A refused tag says which half refused it, because the two have different remedies. A missing or
  unfinished push run is waited out, or produced by running CI on the commit by hand. A missing
  merge-queue run is not: that run exists only for a commit the queue built, so nothing an operator
  starts will create one, and the answer is to tag a commit that reached `main` through the queue.
  Either way the tag is deleted and re-pushed once the evidence is there.

- **A reminder pass that failed outright no longer costs the whole day's reminders.** The built-in
  scheduler keeps a once-per-local-day marker so a restart cannot re-fire the day's pass, and it used
  to advance that marker even when the pass had returned an error. The only error a pass raises at
  that level is "the list of owners could not be read" — raised before a single owner is processed —
  so a moment of database trouble at the scheduled hour recorded a day on which nothing was sent as a
  day already done, and a restart later the same day saw the marker, skipped the run, and sent
  nothing either. Marking it was deliberate: it stopped a broken database from making the scheduler
  spin. The pass is now retried instead — a few minutes apart, up to three attempts in the same slot
  — and the day is marked only once one succeeds or the attempts are spent, so the anti-spin property
  is kept while a transient failure costs minutes rather than every owner's reminders. A process
  stopped between two attempts leaves the day unmarked, so the next start catches it up, and the
  per-reminder watermark still guarantees a retry cannot re-send what an earlier attempt delivered.
  A pass that panics is unchanged: it is still contained, still leaves the day unmarked, and is still
  retried by the next scheduled fire rather than immediately.

- **An upgrade that a duplicate symptom name stops can now be finished.** Adding the per-account
  unique index on symptom names refuses when the database already holds two symptoms one account
  had under the same name, and that refusal is correct — it deletes nothing and names every
  conflicting group. What it did not have was a way out: the refusal is also what stops the
  server, so the instruction to resolve the groups "through the application" pointed at an
  application the refusal had already stopped, and every other operator subcommand applies
  migrations on the way in and met the same refusal.

  `ovumcy repair symptom-names` is that way out. It opens the database **without applying
  migrations**, which is what lets it reach the state that is stuck at all. Run on its own it
  inspects: it prints which symptom each account keeps and which rows would fold into it, changes
  nothing, and exits non-zero while duplicates remain, so an upgrade script can gate on it. Run
  with `--apply` it merges each group — every day log that named an absorbed symptom is
  re-pointed at the kept one **first, in the transaction that removes the rows**, so no day loses
  what the owner recorded, and a day that named both ends up naming one. A second run finds
  nothing and says so, which is also what a failed run leaves behind: the merge is one
  transaction, so a failure changes nothing and can simply be repeated.

  Which row is kept follows what the owner can still reach: an active row before an archived one,
  a built-in before a custom one, and the oldest of what remains. The built-in rule is not a
  preference — the reachable half of this class is the built-in seeding race, and a built-in
  symptom can be neither renamed, hidden nor removed from Settings, so leaving a second built-in
  behind would leave a duplicate in the day-entry picker with no surface anywhere that can clear
  it.

  The repair reads its groups through the database's own `lower(name)`, the same expression the
  index is built on, rather than folding names in Go. That is what makes one command correct on
  both engines where they disagree: PostgreSQL folds by locale and SQLite folds ASCII only, so a
  case-only variant of a non-ASCII name is a conflict on one and not the other — and the repair
  reports exactly what that engine's index would refuse, without needing to know which engine it
  is on. Both engines are covered by a test that takes a database holding duplicates through the
  documented steps to a completed start.

  Being the only entry point that opens a database of unknown schema version, it is also the only
  one that can be pointed at the wrong database and not notice — a SQLite path that does not exist
  is created empty rather than refused. So it checks the schema it needs before any query assumes
  it, and answers a mistyped `DB_PATH` by naming the database it actually reached, instead of with
  the engine's `no such table` about the very table the operator was told to repair. A PostgreSQL
  target is named by `DATABASE_URL` and never by its value, which carries credentials.

  The check covers the columns and not only the table, and the two failures get different answers
  because they are different mistakes. A schema that predates the column deciding which row of a
  group survives is not a wrong database, and is told so: start the instance to carry the schema
  forward, and if it stops on migration 037, run the repair again — by then with the column it
  needs.

  The refusal message now names this route and the runbook instead of the application.

- **`docs/self-hosted.md` says what a rollback past the last six migrations costs.** Downgrade
  Caveats stopped at migration 022, so everything added since was undocumented in both
  directions. It now covers 032 through 038: which are plain additive changes an older binary
  ignores, which one deletes rows and why that is not a loss, and the two whose protection is
  simply off while you are downgraded — the "shown exactly once" marks on the recovery code and
  the calendar-feed URL, and the webhook configuration counter. Migration 037 is called out as
  the opposite shape: safe to roll back, and the one that can refuse to roll forward.

- **A new operator runbook covers a migration refused by existing data.** It gives the whole
  sequence — stop the service, back up, inspect, apply, start — including the detail that the
  command is reached with `docker compose run` rather than `docker exec`, because the container
  `docker exec` would attach to is not up.

A settings restore refused because the page's security token went stale no
longer says "Restore failed. Please try again." while every retry would be
refused the same way. It now shows the refusal message and fetches a fresh
token, so the retry it asks for can succeed. A restore refused because the
session expired now says to sign in again.

- **The pregnancy-pause notice now says what actually resumes predictions.** It told the owner to
  "log a new period", but a period started on the day of the positive test does not lift the pause:
  the positive result wins a same-day tie, since the day carries no order and a bleed on the day of
  an early positive can be implantation spotting. The notice, in every language, now names the two
  things that do resume tracking: a new period starting after the test day, or removing the result.

- **A missing period log no longer lets a long-running cycle keep its predictions.** One unlogged
  period merges two real cycles into a single enormous span, and that span was averaged in with the
  ordinary ones: three 28-day cycles beside one 300-day gap average 96 days. The safety rule that
  withholds every projected date once a cycle runs a week past its expected length was measured
  against that average, so an account on day 61 of its current cycle was compared against 103 —
  and kept a next-period date, a fertile window and an ovulation day on the dashboard, the
  calendar, `/stats`, the JSON API, the webhook reminders and the `.ics` feed, all of them
  calculated from the 28-day median the projection actually uses. The rule now measures against the
  shorter of the average and the length those dates are calculated from, so no single outlying
  span can raise the bar and no history withholds later than it did before; the late-cycle notice
  follows that same length rather than answering separately, and the cycle ribbon on the
  dashboard, which is drawn entirely from the projection, is hidden while the estimate is paused
  rather than drawn around a date that is being withheld. The same rule now also covers the
  message shown after saving a day, which could still call a day fertile from a withheld window,
  and the implantation-bleeding hint offered when a new cycle start is logged — which is now also
  withheld in unpredictable-cycle mode and during a pregnancy pause, as every other projection
  already was. An ordinary 28-day
  pattern and a genuinely long-but-regular one (three real 50-day cycles) keep the exact threshold
  they had; a history whose average sits above its median now stops publishing dates on the day
  its own estimate has run out rather than a few days later. No recorded day is changed or
  removed — the merged span is still there to be corrected by logging the missing period.
- **An ovulation day confirmed by your temperatures stays visible when a long cycle pauses the
  estimate.** It used to disappear from the dashboard, the calendar and the JSON API together with
  the projected dates. It is a day read from your own recorded readings, not a projection, so it is
  now still shown — as an estimate, beside the usual disclaimer — while the fertile window and the
  fertility status stay withheld until the next cycle starts.

- **A relative `CALENDAR_FEED_FENCE_PATH` now refuses the server's boot instead of being accepted on
  one side only.** The calendar-feed restore fence keeps one marker in the database and one in a file
  at that path, and both the server and the operator CLI (`ovumcy users delete`, a forced
  `ovumcy reset-password`) have to open the same file for a revocation to be recorded outside the
  database. The CLI already refused a relative path, because it resolves against whichever working
  directory the command runs in; the server took the same value verbatim and resolved it against its
  own. On a bare-binary deployment that left a server whose fence worked and operator removals that
  could never be completed — refused with a remedy, "point the server at this path", the operator had
  already carried out. Both sides now judge the value with one predicate, so the set of accepted paths
  is the same on each.

  **Upgrade note.** An instance whose `CALENDAR_FEED_FENCE_PATH` is relative will not start after this
  release: the boot stops with `invalid CALENDAR_FEED_FENCE_PATH=...`, naming the value and asking for
  an absolute one. Change the variable to the absolute path of the same file — for the bundled compose
  stacks and the image this is already `/app/fence/calendar-feed.fence`, so nothing changes there — and
  start again. Leaving the variable unset is unchanged and still supported: no fence is configured, and
  every armed calendar feed is disarmed on each start, as the startup line reports.

- **The stats reliability card no longer describes two states of one account.** An owner in
  irregular-cycle mode with exactly two completed cycles read the heading "Early estimate" beside the
  hint written for a variable pattern. The heading and the hint now come from one reliability tier,
  so a "variable pattern" hint appears only where the "Variable pattern" heading does — at three
  completed cycles, the same minimum every other pattern statement on the page uses.
- **`ovumcy webhook show` reports the reminder lead window that is actually in force.** For a stored
  lead-day value outside 0–14 — a hand-edited row, a restored backup, or a row written before the
  bound existed — `show` printed the raw column, so it named a window no reminder would ever fire on
  and a no-op `set` looked like a change. Both the show and the set views now print the clamped value
  the reminder decision uses.

- **Settings deep links now land with the section title in view.** Opening `/settings#settings-cycle`
  (the dashboard's "update cycle data" button), the privacy page's ledger link, the account chip or
  any pill of the section index scrolled the card's top edge to the top of the viewport — under
  the sticky header and, from 640px up, the sticky section index, about 220px of chrome on a
  desktop. Every settings card now carries a `scroll-margin-top` that clears the header on a phone
  and the header plus the index on wider screens. The index wraps to as many rows as the locale and
  width need (three in Russian on a tablet) and the header wraps with text zoom, so both heights
  are measured and kept current by the settings script; without JavaScript a default applies.
  The index's sticky offset and the cards' clearance now read one shared value, so the two cannot
  drift apart.

Saves no longer lose their feedback. Any form that shows its result in a status
area (settings, the dashboard's usage goal and cycle start, onboarding) now says
so when its request never reached the server, instead of leaving an old "Saved"
in place; saving one settings form asks before it discards unsaved edits in
another; and renaming a custom symptom keeps you on the renamed row, with focus
and its confirmation announced to screen readers, even though the list re-sorts.

- **Settings forms submitted without JavaScript now do what their button says.** Six settings
  forms send PUT or DELETE through htmx but fall back to a plain POST without JavaScript: disable
  and enable 2FA, change password, unlink an OIDC identity, hide a symptom, and revoke the
  calendar feed. That POST got 405 on five of them. On the calendar feed it hit the generate route,
  so "Revoke" issued a fresh feed URL instead. Each form now carries a hidden `_method` field, and
  the server routes a urlencoded form POST as the PUT, PATCH or DELETE it names. The override runs
  ahead of the rate limiters and CSRF, so both see the verb that actually runs and the CSRF token
  stays mandatory. JSON bodies, the query string and every verb other than POST are never
  overridden. Any other `_method` value, an empty one or a repeated one is refused with 400.

- **Breaking (API shape): the reminder lead-days setting now refuses a value outside 0-14 instead of
  silently saving the nearest one.** An empty, non-numeric or out-of-range value is answered with
  its own message in the reminders form ("Enter a whole number of days from 0 to 14.") rather than
  the password sentence, and a JSON caller of `PATCH /api/v1/users/current/reminders` receives a
  400 where it used to receive a 200 with the clamped value. Saving the Interface card with the
  same language now confirms in place without reloading the page to the top; a language change
  reloads to the Interface card with its confirmation beside it, the confirmation is announced to
  screen readers and takes focus, and the theme control no longer pre-selects Light (the theme is
  still remembered in this browser only).

- **A refused step-up now says so on the settings page.** Linking an OIDC identity, enrolling a
  local password, or confirming a data wipe or account deletion at the identity provider returned
  the owner to an unchanged settings page whenever the provider re-authentication was refused —
  stale, belonging to another session, reported as an error by the provider, or an identity already
  linked elsewhere. Nothing was shown, so a refusal looked exactly like a success. The refusal is
  now displayed, in all six languages. The same return now also covers the step after the
  re-authentication: if enrolling the local password fails to be saved, or this device's session
  cannot be re-issued afterwards, the owner gets the settings page with an explanation instead of a
  raw error object.
- **Turning on two-factor authentication no longer leaves the enrollment cookie behind when the
  sign-in cookie cannot be refreshed.** Confirming the first code enables 2FA and re-issues this
  device's sign-in cookie. If that last step fails, the browser kept the short-lived cookie holding
  the authenticator seed until it expired on its own; it is now cleared along with the refusal, as
  it already was when the enrollment succeeded.
- **Turning two-factor authentication on or off now confirms itself where the owner can see it.**
  The success side of the same defect the refusals had: both answers were attached to the settings
  confirmation carrier and then sent to the two-factor page, which does not read it — and attached
  as a raw message name, which the page that does read it resolves to nothing. So the confirmation
  was invisible twice over, and the unread one rode along to whichever page consumed it next. Both
  now return to the settings page, which shows the confirmation together with the account's new
  two-factor state.
- **Switching the tracking mode from the dashboard no longer plants a stray "saved" message on the
  next settings page.** The quick switch returns to the dashboard, which shows the new mode itself
  and reads no confirmation carrier; the confirmation written for it was invisible on arrival and
  then appeared on the settings page the owner opened next, as a save they had not just made.

- **The dashboard now shows what a day save says back.** The journal saves itself in the
  background, and that save ignored the server's answer, so the sentence the server composes for
  the day never appeared — including, after a positive pregnancy test, that predictions are paused
  and that bleeding, pain or dizziness mean seeking medical care promptly. The sentence now appears
  in the journal's status line after each save, the same way the calendar editor shows it, rendered
  as text. A plain "Saved." is not repeated there, since the journal's own indicator already says
  it.
- **The pregnancy-pause sentence stays until it is dismissed.** On the dashboard and in the calendar
  editor, the save sentence that carries the red-flag guidance no longer disappears after a few
  seconds; it stays until closed. Other save sentences still clear themselves.

- An account without a local password (SSO-provisioned, password not yet set) opening
  Settings → Two-factor authentication is told that enabling or disabling 2FA is confirmed with the
  account password and linked to the local-password setup, instead of being shown a QR code and a
  form every submit of which was refused as "invalid password". No enrollment seed is minted for
  that account until it can confirm one.
- On an `OIDC_LOGIN_MODE=oidc_only` instance the account settings no longer invite setting a local
  password "for recovery codes", nor offer to regenerate one: both consumers of a recovery code
  (the public recovery flow and local sign-in) are closed in that mode, so the page names the
  identity provider or the operator as the recovery path. The local password still confirms safety
  actions and two-factor setup.

- **Ovulation and fertile-window estimates are now withheld once the running cycle is past its usual
  length.** From the first day past the account's reference cycle length — the day the dashboard
  said "Cycle data may be outdated", a hint the late-cycle notice now replaces — the dashboard, the
  calendar grid,
  `GET /api/v1/stats/overview`, the calendar feed and the "ovulation soon" webhook reminder used to
  keep naming an ovulation date and drawing a fertile window for a next cycle whose start was never
  logged. That half of the projection is now withheld on every surface until a new period is
  logged; the next-period estimate stays, until the existing overdue cut-off a week later. The
  stats API names the state with a new suppression reason, `cycle_data_stale`, added to the
  `reasons` enum of the overview response; a client that branches on the reasons should accept it.
  An ovulation day the owner's own temperatures confirmed is still shown. The plain-words late-cycle
  notice now appears from that same first day instead of a week later, and it is shown on the
  calendar and stats pages as well as on the dashboard. On the stats page it shares one card with
  the existing note about recent cycles longer than 45 days instead of standing beside it, and the
  `cycle_data_stale` flag and reason now always change on the same day.

- **The stats page's temperature chart no longer marks an ovulation day the rest of the app
  withholds.** In unpredictable-cycle mode, during a pregnancy pause and before the first cycle is
  complete, the dashboard, the calendar and the API all hold back the day a temperature shift
  confirmed — but the current-cycle BBT chart on the stats page still drew its "probable ovulation"
  marker and the coverline behind it. Both now follow the same decision the other screens use. The
  recorded temperatures stay on the chart in every case; only the interpretation drawn over them is
  withheld. An overdue cycle still shows the marker, as the dashboard and calendar do.

- **The stats BBT panel now says what it is waiting for, names the day of the probable ovulation and
  prints every temperature the same way.** With basal temperature tracking on and fewer than five
  readings in the cycle, the panel used to vanish without a word; it now shows how many readings
  the chart needs and how many are recorded. The text summary names the marker's date, the matching
  row of the readings table is flagged, and the coverline prints at one decimal with the unit
  attached (`36.5°C`), as the table and the chart do. The chart now points to its readings table,
  and a one-line note explains what the coverline is. French, Italian and German spell the term out
  (`température basale`, `temperatura basale`, `Hilfslinie`) instead of `TB`, `BBT` and `Coverline`.

- **The stats cycle stack now says how many cycles it shows, explains its colours and can be read
  without seeing it.** A caption states "showing N of M completed cycles" with a note that older
  cycles, and any unusually short or long ones among them, are left out. A legend lists the period,
  the logged-day mark and, when past cycles are shaded with inferred phases, the phase colours and
  the fertile window, with the note that these are estimates beside the usual disclaimer. Each row
  carries a text summary (start date, length, period, days with an entry and, when shown, the
  estimated fertile window), and the visible date and length are no longer read twice.

- **The statistics page no longer claims a fertile window while cycle predictions
  are paused by a positive pregnancy test.** The phase card there decided whether
  to show estimates from the unpredictable-cycle setting alone, so it was the one
  prediction surface the pregnancy pause never reached: the calendar grid, the
  calendar feed and the outbound reminders all withheld their projections while
  the statistics page kept showing "Fertile window", derived from the cycle
  length set during onboarding rather than from anything recorded. It now reads
  the same paused-or-off decision the dashboard already resolves, and shows the
  logged-history view instead. Nothing changes for an owner without a pause.

- **The stats page counts completed cycles the way the dashboard does.** The insights gate and the
  "n / 3" progress line on `/stats` used to count the gaps between cycle starts before today, so on
  the day a new period start was logged the dashboard already counted the cycle it closed while the
  stats page still waited one cycle longer. Both pages now read the same count. The trend chart keeps
  its own series.
- **No sentence names the three-cycle threshold as a literal any more.** The empty-state body and
  hint on `/stats`, the "waiting for more cycles" and irregular-mode explainers on the dashboard,
  calendar and stats page, and the dashboard's next-period, ovulation and fertile-window captions
  take the number from the one threshold constant, in all six languages.
- **A late cycle on one or two completed cycles no longer repeats itself.** On the dashboard, the
  calendar and `/stats`, the late-cycle card ("a personal range needs more completed cycles") now
  stands alone; the "fertile details wait for 3 completed cycles" explainer beneath it is shown only
  while the cycle is not late.

`docs/openapi.yaml` no longer promises a `303` redirect to the provider's
authorize URL from the three OIDC step-up operations
(`POST /api/v1/users/current/password/step-up`, `.../data-wipe/step-up`,
`.../deletion/step-up`). That hop stopped being a redirect when the settings
page's CSP began pinning `form-action` to `'self'`: Chromium enforces the pin
across a form navigation's whole redirect chain, so a cross-origin `3xx` aborts
in the browser. The server hands back a same-origin `200` page whose
meta-refresh performs the hop instead — the behaviour the fourth operation of
the same class, `.../oidc/link/step-up`, already documented. A JSON caller was
never affected: it receives `200 {ok, redirect_url}` and always did.

The `303` these operations really can answer a browser with is
`respondSettingsError`'s refusal bounce back to `/settings`, shared by the
settings mutations under `/api/v1/users/current`. Of the 23 operations the spec
publishes under that prefix, four declare a `303` after this change — the two
calendar-feed reveals and the two 2FA toggles — and there the redirect is the
browser's primary outcome, not a refusal. The refusal bounce is the
cross-cutting HTML-surface answer the spec's own preamble puts outside this
contract, so it is not declared here either.

A new guard keeps the class closed. Over-declaration in an OpenAPI spec is
generally undecidable — proving no path reaches a status is proving a negative,
which is why the existing per-operation reachability walk only reports
under-declaration. This one asks the opposite, positive question: does the
route's handler chain reach the same-origin interstitial helper? A hit is a
witness, and it settles the browser answer, so an operation that serves the
interstitial may not declare a `303`. Neither status guard could have caught
this on its own: the whole-server sweep passes because `303` is emitted all
over `internal/`, and the per-operation walk passes because `303` really is
reachable here — just not for the reason the spec gave. The falsehood lived in
the response description, and nothing in the suite reads one.

- **A refused symptom action on Settings now says so.** Editing, hiding or restoring a symptom
  that is not in your list (a stale page, a built-in symptom) re-rendered the card with no message;
  the refusal now appears at the top of the symptoms card and receives focus. It reads the same
  whether the symptom never existed or belongs to another account on the instance.
- **Clear-data re-authentication explains the lockout.** After too many wrong passwords the status
  beside "Clear all data" read "Request failed. Please try again."; it now shows the same
  too-many-requests message as account deletion.

- **The API docs describe the JSON bodies the settings saves and probes actually answer.** Six
  settings operations (profile, interface, tracking, reminders, timezone, webhook save) and the two
  withdrawals (webhook removal, calendar-feed revocation) declared the closed `OkResponse` while the
  server answered with the saved values echoed back, so a client validating responses against
  docs/openapi.yaml refused the server's own answer. Each now references its own schema listing the
  keys it carries. `/healthz` and `/readyz` document their fixed `{"status": ...}` bodies, including
  the `503` one; `ExportSummary.date_from`/`date_to` no longer claim `format: date`, since both are
  `""` for an account with no data; `ExportJSONEntry` no longer lists `bbt` as required, since a day
  with no measurement omits it; and the tracking save's request body lists `week_starts_on`. The
  server's behaviour is unchanged. A new contract test drives every JSON success response through
  the real router and compares its keys with the declared schema in both directions, along with
  enums and numeric and length bounds, so an undocumented key, a missing required one or an
  undeclared value fails the suite.

- **Breaking (API shape): `PATCH /api/v1/users/current/cycle` no longer rewrites the settings a
  request never mentioned.** A JSON save that carried the cycle geometry alone was read as a full
  snapshot, so every member it did not name was written back as that member's zero value. The
  expensive one was the tracking mode: an owner tracking to **avoid pregnancy** was silently moved
  to general health tracking, which reframes the fertile window, the badges and the summaries
  across every surface — a mode the product otherwise changes only on an explicit owner action.
  The age bracket was wiped to unspecified the same way, and the three cycle flags were cleared.
  The endpoint is now the partial update its verb promises: a member the body carries is written
  (including one carrying `false` or `""`, which is a value, not an absence), and a member it omits
  is left exactly as it stands. What changes for a client that was relying on the old behaviour: to
  clear a flag or a bracket, send it explicitly rather than leaving it out. The one body that is
  refused rather than accepted is one naming no member at all: it asks for no change, and a save
  that changed nothing should not be recorded as one.

  The save now writes only the columns the request named, rather than the whole row, so this holds
  between two overlapping requests as well as inside one: a save of the cycle lengths can no longer
  put back the tracking mode as it stood when that request began, undoing a mode switch made from
  another tab in between.

  The dashboard's mode quick switch is fixed on the same edge — it used to take a one-column path
  whenever the two lengths were missing, so a flag travelling beside the mode was dropped — and its
  success body no longer echoes the stored `usage_goal`, which made it the one save whose `200`
  a client validating against the published `OkResponse` schema rejected.

  The settings form is unaffected. It submits every control it owns on every save, and an unchecked
  box submits nothing at all, so a form body has no absent members to honour and is still read as
  the full snapshot it is — the toggles keep switching off.

Toast icons and the dismiss button now draw their glyphs (✓, ⚠, ×) from CSS instead
of text nodes, so a toast's text content is only its message and no longer repeats the
icon and the ×.

- **A notice no longer covers the bottom navigation on a phone.** After a save, the confirmation
  appeared on top of the floating tab bar and every tap on Today, Calendar, Insights or Settings
  landed on the notice for the five seconds it stayed up. Notices now appear just above the bar
  wherever the bar is shown, and the empty space around them no longer intercepts taps.

- **A tracking-settings save with an unrecognised `temperature_unit` is refused with `400`.** `fahrenheit`, `kelvin` and the like used to answer `200` and silently store `c`; only an omitted unit still reads as `c`.

- **The settings forms and the dashboard usage-goal switch save without JavaScript.** The profile,
  interface, tracking, cycle, reminders and symptom-edit forms, and the dashboard usage-goal quick
  switch, are sent by htmx as PATCH, but a browser without JavaScript posted them to URLs that only
  accept PATCH and got 405 Method Not Allowed, so nothing was saved. Each form now names its real
  method in a hidden field, and the browser is redirected with 303 back to the settings page or
  the dashboard. API clients and htmx get the same responses as before.

- **Account deletion and the day forms no longer put their fields in the address bar without
  JavaScript.** The account-deletion form, the calendar day editor's save and delete forms, and the
  dashboard's day form declared no method or action, so a browser without JavaScript submitted
  them as a GET to the current page: the deletion password and the day's entries landed in the
  query string, and nothing was saved or deleted. Each form now posts to its own endpoint with a
  hidden `_method` field and a CSRF token. The deletion password is read only from the form body.
  Such a form submission is redirected with 303: to the login page after deletion, and to the
  calendar day or the dashboard after a day is saved or deleted. Without JavaScript, deleting a
  day asks for a ticked confirmation box instead of the dialog. Requests that send PUT or DELETE
  themselves, htmx included, get the same responses as before.

- **`OIDC_CLIENT_SECRET_FILE` no longer blocks the boot of an instance with OIDC disabled.** The
  client secret was resolved before `OIDC_ENABLED` was consulted, so a stale or unreadable secret
  file path refused the start of an instance that never uses it. With `OIDC_ENABLED` off neither
  `OIDC_CLIENT_SECRET` nor the file is read. With OIDC enabled nothing changes:
  `OIDC_CLIENT_SECRET` still wins when both are set, and an unreadable file that is the only source
  still refuses the boot.

- **The irregular-cycle ovulation range on the dashboard is no longer a day late.** The range
  was the next-period range shifted back by the luteal phase, which puts ovulation one day after
  where the cycle model — and therefore the calendar, the `.ics` feed and the reminders — places it.
  With a 24-day shortest and a 45-day longest cycle from 1 March the dashboard showed 11 March to
  1 April while the model gives 10 to 31 March, so the earliest possible ovulation was named a
  day late. Both ends now come from the same predictor as every other surface. When the shortest
  observed cycle is too short for the model to place an ovulation at all (under 15 days), the old
  shift put the start of the range before the cycle even began (with a 12-day shortest cycle,
  luteal phase 14 and a period starting on 1 March, on 27 February); the range now starts at the
  earliest day the model ever names (cycle day 5).
- **Webhook reminders are decided from the same two years of history as the app.** The reminder
  pass read the owner's whole stored history while the dashboard, calendar, stats page and `.ics`
  feed read the last two years. With old cycles outside that window, the app could pause its
  next-period estimate while a reminder was still sent to the webhook endpoint (or the other way
  round). Both now cut the history through one shared helper.
- **The "saved" message names the same fertile days as the dashboard, and says it is an
  estimate.** After saving a day the message derived its own cycle statistics with the default
  14-day luteal phase and no confirmed temperature shift, so an owner whose luteal phase had been
  inferred as 10 days heard "fertile" on days the dashboard showed nothing and nothing on days it
  shaded. It now goes through the same history window, personal baseline, confirmed-shift step and
  fertility gate as the dashboard. The wording in all six languages now reads as an estimate
  ("this day is likely inside your estimated fertile window") instead of stating a certainty.
  Like the dashboard, which prints "unknown" for the fertility status once the running cycle has
  outrun its reference length, the message no longer calls a day fertile on an out-of-date cycle.

- **The example stacks now forward every setting the app reads.** The five stacks under `docs/examples/` listed their environment by hand and left out `REGISTRATION_MODE`, `HSTS_ENABLED`, `AUDIT_LOG_ENABLED`, `WEBHOOK_BLOCK_PRIVATE_ADDRESSES`, `REMINDER_SCHEDULER_ENABLED`/`REMINDER_SCHEDULER_HOUR` and the register, logout and calendar rate limits, so a value set in `.env` for any of them changed nothing and a public proxy stack ran with open registration whatever the runbook advised. Each is now passed through with the application's own default, so a stack whose `.env` sets none of them behaves as before. **A value `.env` already holds for one of them was dropped until now and takes effect after the upgrade**: `REGISTRATION_MODE`, `HSTS_ENABLED`, `AUDIT_LOG_ENABLED` and `WEBHOOK_BLOCK_PRIVATE_ADDRESSES` refuse to start the app on a value they cannot read (a typo that used to be harmless, or `REGISTRATION_MODE=closed` beside `OIDC_AUTO_PROVISION=true`), and `REMINDER_SCHEDULER_ENABLED`, `REMINDER_SCHEDULER_HOUR` and the rate limits change behaviour, or fall back with a log line when invalid. Review `.env` for these keys before the first start of the new compose file. A repository test type-checks the binary, finds every environment variable it reads by following the name to `os.Getenv`, and fails when an example omits one, or fixes it to a literal that ignores `.env`, without a stated reason. The runbook names the few settings a stack fixes on purpose, and now says to create accounts under `REGISTRATION_MODE=closed` with `ovumcy users create`.
- **The upgrade steps and the downgrade caveats no longer overpromise.** The runbook and the README now say to take the new compose file, or add the `ovumcy_fence` volume, before the first v2.0.0 start: without it every armed calendar feed is disarmed on every start. The downgrade section gains the case a release older than v2.0.0 can reopen: a feed revoked under it is brought back by restoring an earlier backup under the same fence file, and the remedy is `docker compose down` followed by removing the `ovumcy_fence` volume before upgrading again. The statement that a restore cannot revive a revoked feed now says which revocations the fence sees, in the runbook, the data-protection note and both security documents. The statements that a pre-041 binary refuses to start and that migrations 032/034/035 need nothing in either direction are corrected.

- **A slower publish for an older commit can no longer put an older image back on `latest` and `main`.** Each push to `main` publishes on its own, and nothing ordered two publishes that overlapped: the one that finished last wrote `latest`, `main` and the Docker Hub mirror's copies of them, whichever commit it was built from, so for a stretch both registries served an older build than the newest commit on `main`. Publishes of the moving tags now run one at a time, and before any alias is written the publish reads the commit the live `main` image was built from and proceeds only if its own commit is that commit or descends from it; otherwise it stops with nothing promoted or mirrored, and the newer commit's publish owns the aliases. Because GitHub keeps only one waiting job per group and cancels it when another arrives, a publish for a commit that is no longer the newest on `main` is now skipped before it reaches the group, so it cannot cancel a waiting publish of a newer commit. A release tag moves no alias and is unaffected.

- **In irregular-cycle range mode the dashboard ribbon no longer paints the uncertain days as
  luteal.** When your recent cycles vary widely, the estimated fertile window runs on past the
  estimated ovulation day, because the ovulation may still be ahead. The phase line above the ribbon
  already says "unknown" on those days; the ribbon cells beneath it still drew them as luteal, as if
  the ovulation were behind you. Those cells now carry the same "unknown" in a neutral colour, and
  luteal starts the day after the window ends. Regular-mode ribbons are unchanged.

- **The API reference now says that `GET /api/v1/days` requires both `from` and `to`.**
  `docs/openapi.yaml` declared both query parameters optional, although the server refuses a
  request that leaves either one out. Both are now marked required. The `400` now lists the three
  keys the server answers with: `invalid from date` and `invalid to date` for a bound that is
  missing, blank, malformed, not a real calendar date or a day the request's timezone never had,
  and `invalid range` when `to` is before `from`. `from` is checked first, so a request with both
  bounds wrong names only `from`. An HTMX request gets the same refusal as an HTML fragment. A
  guard test sends each case to the real route. The server's behaviour is unchanged.

- **The API reference now gives the real lifetime of a remembered sign-in: 30 days.**
  `docs/openapi.yaml` described `remember_me` on `POST /api/v1/sessions` as a cookie with a
  ~7-day `Expires`; the server has always set 30 days. The description now also says that without
  `remember_me` the cookie has no `Expires` or `Max-Age` but the session inside it still ends 7 days
  after the cookie is issued, lists the cookie attributes (`HttpOnly`, `SameSite=Lax`, `Path=/`,
  `Secure` under `COOKIE_SECURE`), and says which later steps keep the choice: the 2FA challenge and
  a re-issue after a password or 2FA change do, a forced password reset does not. A guard test
  reads both lifetimes from the spec and checks them against the cookie and the token the server
  issues. The server's behaviour is unchanged.

- **The API reference now explains the `id: 0` day and what `HEAD` on a day answers.**
  `GET /api/v1/days/{date}` answers `200` for a day with no stored record, with a placeholder built
  at read time and never stored. `docs/openapi.yaml` now documents that placeholder field by field:
  `id` is `0`, the timestamps are the zero time, and `bbt` is absent. It also says that a stored
  record always has a non-zero `id`. `HEAD` on the same path was described as checking for "at
  least one persisted field". It actually answers whether the day holds data, so the spec now lists
  the values that count. A stored record that carries none of them, such as one created by a `PUT`
  with no values, gets `404`. The `DailyLog` schema no longer calls every day record persisted, and
  no longer says `date` is shifted into the caller's timezone at read time: it is returned as the
  stored calendar day. Guard tests drive the real routes for these claims. The server's behaviour
  is unchanged.

- **Webhook reminders, the calendar feed, the calendar grid and the stats API no longer name a
  date the dashboard withholds.** With irregular cycle mode on and fewer than three completed
  cycles, the dashboard says more cycles are needed instead of naming a next period or an ovulation
  day, yet the reminders still sent "ovulation around …", the `.ics` feed still carried those days,
  the calendar grid still painted them and `GET /api/v1/stats/overview` still published them. None
  of them does now: the state withholds every projected date on every surface, and the dashboard's
  next-period line shows the "needs more cycles" note on its own, without the date beside it. The
  stats API names it with a new suppression reason, `irregular_needs_more_cycles`. As with the
  other withholding states, the stats page's inferred phases for past cycles and the late-period
  hint are not shown in this state either: both are built on ovulation days the app does not yet
  have the history to place.
- **Where the dashboard shows a range, reminders and the feed send that range, not one day.** With
  three or more completed cycles that vary in length the dashboard shows a next-period start window
  (and, in irregular cycle mode, an ovulation range) because a single day would overstate the
  estimate; reminders and the feed kept sending the middle day of that window. A reminder now names
  the first and last day ("Next period estimated to start between … and …"), and the JSON payload
  adds `event_date_end` beside `event_date`, which becomes the window's first day; a single-date
  reminder is unchanged and carries no `event_date_end`. The feed carries the window as one
  multi-day event. A reminder already sent for the current cycle before this update is not sent
  again as a range.

- **The dashboard says each thing once, and only when it is true.** "Date is already in the past"
  waits until the last predicted period day is behind you, not the first. Before the first completed
  cycle, the fertile-window timing is explained once instead of twice, and the egg-white badge names
  the observation ("Egg-white mucus logged today") rather than a fertile window that is still held
  back. The pregnancy pause note names the test day and links to it in the calendar, where its result
  can be removed. A period logged outside the start of the current cycle now says why fertile details
  are held back.

- **Primary buttons keep a frame in forced-colors mode, the success toast is opaque, and pre-fertile
  calendar days have their own legend row.** In a high-contrast theme a primary button had no border
  and read as bold text; it now draws a `ButtonText` frame. The success toast background was about
  one third opaque, so page text showed through it; it is now a solid surface in both themes with
  the same readable text colour. A pre-fertile calendar day used to share its dashed outline with a
  predicted period day and had no legend entry; it is now a solid line with an inner ring, listed in
  the legend as "Days before the fertile window (estimate)" (all six languages) and named in the
  day's accessible name, and in forced-colors mode it no longer looks like a recorded period. A day
  save that fails or is refused (a conflict, an expired session, a stale token) is scrolled into
  view on a desktop screen instead of appearing below the fold.

- **The day summary prints a temperature in the interface language's number format.** In Russian
  with Fahrenheit, the read-only summary read "98.60 °F" while the stats page read "98,6"; it now
  uses the same decimal separator and one decimal as stats. The temperature field in the day
  editor and on the dashboard shows the same separator too (98,6, not 98.60) and keeps it after you
  leave the field; a comma or a dot is accepted either way.
- **The language picked on the registration page is stored when the account is created.** It used
  to be stored only when onboarding completed, so signing in again before then showed English.

- **Narrow phones and enlarged text no longer scroll the page sideways.** At 200% text size on a
  320px phone the Insights cards, the dashboard status line and "more details" pill, the footer
  privacy link and the settings section index could push the page wider than the screen, taking
  the tab bar with it. They now wrap inside the screen.
- **The Danger zone password rows stack on phones.** The password field takes the full line with its
  button below it, instead of a squeezed field beside a stretched button.
- **Keyboard focus no longer rests under the floating tab bar on phones.** The scroll clearance now
  sits on the page scroller rather than on the content block.
- **Inline links are easier to tap on phones.** Sign-up, recovery, breadcrumb, logout and late-cycle
  links, and the "more details" pill, reach a ~40px touch target.

- **Settings check the password before the point of no return.** Deleting the account now reports a
  wrong password straight away, before the final confirmation, the same way clearing data does.
- **One export preset is marked at a time.** With less than a year of data, "All time" and
  "365 days" cover the same range; only the one you picked (or "All time") is highlighted.
- **Temperature stays hidden while BBT tracking is off.** The stats page no longer charts stored
  readings and the day summary no longer lists them while the field itself is hidden. The readings
  are kept and come back when tracking is turned on.

- **The calendar's start window and the dashboard cycle ribbon show the window the dashboard
  header names.** Both worked the window out on their own, so after the usual cycle length had
  passed they could show a different window from the header, the reminders and the calendar
  feed. They now read the header's window.

- **A late period is shown as due now, not a month away.** Once a cycle ran past its usual
  length, the dashboard header, the calendar's start window, reminders and the calendar feed
  jumped to the following cycle's dates for up to a week, until predictions were paused as
  overdue. They now keep the expected period of the current cycle, and the dashboard notes when
  that date has already passed.

- **Ovulation and fertile-window estimates now wait for three completed cycles.** A regular
  (non-irregular) account with one or two completed cycles used to get an ovulation date and a
  fertile window on the dashboard ribbon, the calendar, `GET /api/v1/stats/overview`, the calendar
  feed and the "ovulation soon" webhook reminder, built from one or two observed cycle lengths.
  Below three completed cycles that half of the projection is now withheld everywhere, the way it
  already was for irregular accounts and for an account with no completed cycle; the next-period
  estimate stays. The ribbon no longer labels days "Ovulation" or "Fertile" in this state, and the
  calendar feed and the webhook send no ovulation event until the third cycle completes. The stats
  API names the state with a new suppression reason, `awaiting_more_cycles`, added to the
  `reasons` enum of the overview response; a client that branches on the reasons should accept it.
  The floor withholds projections only: past completed cycles keep their inferred shading under
  "show historical phases". An ovulation day the owner's own temperatures confirmed is still
  shown. An account trying to conceive sees a line in the dashboard header saying the fertile
  window appears after the first three completed cycles, in place of the withheld ovulation
  estimate.

- **The egg-white fertility badge now follows the same verdict as the fertility header.** It is withheld in every tier where fertility details are held back for an out-of-date, thin or paused history. The one exception is the first cycle: there the badge reflects only the egg-white mucus you recorded today, not a prediction, so it still shows while the predicted fertile window stays hidden.

- **A day page left open no longer overwrites what another device saved since.** The dashboard
  journal and the calendar day editor saved the whole form, so a stale tab erased a period mark,
  flow or note written elsewhere and still answered "Saved". They now send `PATCH
  /api/v1/days/{date}` with only the fields the owner changed, each beside the value the page was
  rendered with (`base_<field>`); fields the save does not name keep their stored value, and
  turning the period off is sent as an explicit `is_period=false`. When another device changed
  the same field, the save is refused with `409` (`day changed elsewhere`) and the page offers
  "Keep mine" or "Take theirs". A stored value already equal to the value being written is not a
  conflict, so a re-sent save that already landed still succeeds. `PUT` is unchanged.
- **A partial save on the onboarding start day keeps its period tick.** A `PATCH` to a date with no
  saved entry that holds the stored onboarding start reads that day as the period day the pages
  show: a save that leaves `is_period` out records the period beside the changed field instead of
  a non-period entry, and an explicit `is_period=false` withdraws the start.

- **Keyboard focus no longer falls to the top of the page after an in-page action.** Saving a day in
  the calendar editor, the late-cycle quick links, the goal quick switch and marking a cycle start (on
  the dashboard and in the calendar) used to leave focus on the page body, so a keyboard or
  screen-reader user had to start over from the top. Focus now stays on the Save button, moves to the
  section a late-cycle link names, or returns to the goal chip or cycle-start button after the page
  reloads. Removing a pregnancy-test result and revoking the calendar feed already kept focus, and
  now use the same shared helper.

- **A page no longer mixes two languages after a switch in another tab.** Content swapped into a
  page (a saved day, a refreshed panel) rendered in the language last chosen anywhere in the
  browser, so a French page could gain German sections. The page now reloads in the new language
  instead; the change that triggered it is still saved.
- **The export date summary is localized in every language and ends at the last entry.** On German
  and Italian pages it showed English month names, and in every language it ended at today rather
  than at the newest entry in the chosen range.

- **The Stats page no longer grades predictions during a pregnancy pause.** With a positive pregnancy
  test saved and no later cycle start, Stats kept showing "Prediction reliability: Building pattern /
  Based on N cycles" and, in the third stat tile, the "Facts only" wording that belongs to
  unpredictable-cycle mode. Both tiles are now withheld while the pause holds; the pause explainer
  above them already says why, and the cycle length and range cards are unchanged. Owners who turned
  predictions off themselves keep their "Facts only" tile.

- **Clear data now says everything it resets, and the dashboard links back to the start date.**
  The Settings card and its confirmation named only calendar entries, cycle settings and custom
  symptoms. They now list every reset, the calendar feed and webhook first: the feed link stops
  working, webhook reminders are turned off and the endpoint removed, auto-fill of period days is
  turned off, tracking and display preferences and the saved time zone return to defaults, and other
  devices are signed out. They also name what is kept: account, email, password, two-factor sign-in,
  recovery code and interface language. After a wipe, the dashboard's "Enter the start date of your
  last cycle" is a link to the cycle settings that hold the start-date field.

- **The note and symptom-name counters now count what the field limit counts.** The counter beside
  the day note (dashboard and calendar editor) and beside a custom symptom name counted characters
  as code points, while the browser's length limit counts UTF-16 code units, in which an emoji
  outside the Basic Multilingual Plane takes two. A note of emoji therefore stopped accepting input
  while its counter still read half full. Both counters now count in the limit's unit, so the
  counter reads full exactly where typing stops; plain text is counted as before.
- **A dashboard left open past midnight moves to the new day instead of saving into yesterday.**
  Every day URL on the dashboard is fixed when the page is rendered, so a journal edit made after
  the owner's local date had changed was saved to the previous day with nothing on screen saying
  so. The page now notices the date change — on a once-a-minute check, when it is shown again after
  being in the background or restored, and when a field is focused or typed into — and reloads onto the new day.
  The reload waits while anything is outstanding: an edit already typed, still saving, or refused
  and waiting for a retry stays on screen, is saved to the day the page shows, and the reload
  follows that save. Typed text is never discarded for it.

The browser timezone sync no longer posts from pages shown before onboarding
completes, where the onboarding gate refused it with a 403 on every load; the
dashboard reached after onboarding performs the sync. Its once-per-tab mark is
now kept per account, so a second account signing in to the same tab after a
sign-out syncs its own timezone instead of keeping an empty one, which left its
reminders and calendar feed on the instance zone.

- **Long symptom names on Statistics now wrap instead of being cut off.** The full name was
  reachable only through a hover tooltip, which touch and keyboard users cannot open. The name now
  wraps in the symptom lists and the tooltip is gone.

- **The stats charts print numbers the way the page around them does.** The average line of the
  cycle-length chart reads 30,5 d for a 30.5-day average instead of 31 d, and the axis and
  baseline labels use the page language's decimal comma (German, Spanish, French, Italian,
  Russian) like the cards and captions beside them.

- **Stats numbers now follow the language and read the same everywhere.** German, French, Spanish,
  Italian and Russian print a decimal comma (30,5 d, 36,6 °C), a no-break space sits between a
  number and its unit, and Russian no longer ends a sentence with "дн..". The cycle-length card, the
  chart summary and the basal-temperature readings round through one rule (half away from zero,
  one decimal), so a 30.5-day average is no longer 30.5 on the card and 31 in the caption, and the
  median of 26 and 35 reads 30.5. A chart whose cycles are all the same length says so once instead
  of "Range 28 to 28".

- **The dashboard reads cleanly on small phones.** The status line keeps "Next period:" and "Ovulation:" whole and stacks its items on a phone instead of stranding a lone "·", the goal-mode panel stays inside the card at 320px, the late-cycle quick links have a 24px hit area, and a cycle longer than the 60-day ribbon now says the ribbon is cut off. Ribbon cells also name today and the fertile window.

The dark amber hint, the focus ring (now solid, and distinct on the today cell), the light calendar legend swatch borders and the out-of-month day numbers now meet WCAG contrast.

- **At 200% text zoom the page no longer scrolls sideways and the bottom tab labels no longer run
  into each other.** Tab labels now wrap inside their own tab, and the settings import file field
  is capped to its card instead of keeping its native width.

- **An import that adds nothing no longer creates custom symptoms.** A file whose days were all
  already in your journal still added its custom symptom names to your symptom list. Custom
  symptoms are now created only for days the import actually writes.

- **Stats progress meters are named for screen readers, and the two-factor pages carry the app name
  in their title.** The cycles-progress bar and each phase mood bar on Statistics now point at their
  visible label. The two-factor settings and sign-in pages read "Ovumcy | …" like every other page.
  The security headers no longer list `ambient-light-sensor`, a feature browsers have dropped, which
  logged a console warning on every page.

- **The calendar legend no longer promises a dash to owners who do not track temperature.** The dash marking an estimated ovulation with no temperature shift yet only ever appears for owners with BBT tracking on; everyone else keeps the solid dot, and their legend now reads "Estimated ovulation" without the dash swatch or the temperature wording.

- **The dashboard's "Saved" confirmation is visible on phones.** After an autosave the confirmation (and its Undo) used to sit below all the form sections, off-screen when you tapped a control near the top. On a phone it now pins itself above the bottom tab bar while a save is in flight or done.

- **Un-ticking a period start clears exactly the days auto-fill wrote.** Each day now records
  whether the period auto-fill wrote it (migration 042 adds a nullable `daily_logs.auto_filled`).
  Un-ticking the start used to guess this from flow, so a fill whose start was later edited to a
  different flow stayed behind, and a day ticked by hand with the start's flow was erased. Now
  fill-written days are cleared whatever the start's flow became, and any day saved by hand is kept,
  even when saved unchanged. Days written before this release carry no mark and are still judged by
  flow. Export does not carry the mark, and every imported day counts as entered by hand.

- **The next-period start window on the dashboard cycle ribbon now clears 3:1 over dark-theme
  luteal days.** In the dark theme the overlay marking the days the next period may start on measured
  2.49:1 against a luteal cell, below the 3:1 floor for graphical marks, and the same overlay was
  under 3:1 on follicular and withheld cells too. The dark-theme overlay is now deeper and clears
  3:1 on every phase it can land on. The light theme is unchanged.

The average period length no longer counts a period that is still running. A
period marked today counted as one day long, and a period entered at onboarding
that has not finished counted only up to today, so a single fresh mark could
shorten the average and every projected period on the dashboard, the calendar,
the stats page and the stats API. The running period now joins the average once
today is past its start plus the period length set in settings; a mood- or
notes-only entry saved meanwhile does not end it early. When it is the only
period recorded, the period length from settings is used, as before.

- **With automatic period fill on, ticking a later day of a period you are still logging now fills the
  days you skipped.** The fill stopped at today when you marked day 1, so marking day 4 later left days
  2 and 3 empty and each had to be ticked by hand. Now, when the day you tick lies within your period
  length of the period's first day, the days between your last period day and the one you ticked are
  filled as period days, and the fill that follows the ticked day stops at the period's own last day.
  A day that already has an entry of any kind stays exactly as you saved it, an un-ticked day or a day
  with only a mood or note included, and nothing is filled past today. Un-ticking the day later does
  not remove the days that were filled; you un-tick those yourself. With automatic period fill off,
  nothing changes.

- **A refused control in the calendar's day editor keeps what was typed.** When another account had
  signed in since the calendar page was opened, the cycle-start or delete control inside the day
  editor was refused — correctly — but the refusal notice replaced the whole editor, unsaved entry
  included. The notice now leads the open editor and the entry stays on screen; only an editor that
  had not opened yet gives way to the notice.

- **A stale dashboard tab no longer shows another account's status header.** After a pregnancy test
  result is saved, the dashboard fetches itself again to refresh its status header. If a different
  account had signed in from another tab in the meantime, that fetch returned the other account's
  page and its header was painted into the stale tab. The refreshed header is now used only when the
  fetched page was rendered for the same account as the open one; otherwise the header stays as it
  was.

- **Stats chart date labels no longer overlap mid-axis on a narrow screen.** The chart checked only
  the last two labels against each other, so a long label in the middle of the axis could print over
  its neighbours at phone widths. Every neighbouring pair is now measured, and the labels thin out
  until none of them touch.

- **Long symptom names on the Insights page wrap instead of being clipped.** The shared name style now wraps and breaks long words by itself, so a name no longer ends in an ellipsis with its tail unreadable on a narrow screen.

- The dashboard goal chip now closes on Escape and returns focus to its summary; ticking or unticking the period day refreshes the status header; the period toggle keeps one fixed label in both states.

The profile and change-password forms in Settings no longer raise a
toast on top of their inline success status. The toast mechanism opted into
by `data-success-toast` had no remaining user and is removed.

- **Clearing all data no longer leaves the time zone unsynced for the rest of the tab session.**
  Clear-data resets the stored time zone, but the tab's once-per-session sync mark outlived the
  wipe, so reminders and the calendar feed fell back to the instance zone until a new tab opened.
  An empty stored zone now always triggers the sync. (WEB-368)

Phone layout overlaps. After a positive pregnancy test the sticky autosave bar, which carries the bleeding/pain safety sentence, no longer runs under the tab bar: it stays fully above it. After an Interface language change the "settings saved" confirmation is scrolled clear of the sticky header instead of landing under it. At 320 px the Russian tab bar no longer breaks "Календарь" and "Аналитика" in the middle of the word.

Accessibility. At 200% text size on a 375 px phone the dashboard and settings pages no longer scroll sideways: long status labels, the goal chip, section headings and the dashboard card column now wrap inside the screen. Under a Windows contrast theme a selected mood or symptom chip, the current navigation link, the current tab and the active language button keep a visible outline or underline instead of looking the same as unselected ones.

- **The calendar explains its disabled Next at the future limit.** The furthest navigable month
  now carries a short notice linked from the disabled Next, matching the one Prev has at the
  history limit. (WEB-374)
- **The stats temperature summary no longer says "no shift detected" while the shift is not
  assessed.** During a pregnancy pause, in unpredictable-cycle mode and before the first cycle
  completes, the probable-ovulation marker is withheld. The summary used to say that no shift had
  been found, even when the readings showed one. It now says that the shift is not assessed for
  this cycle. (WEB-375)

- **A late period no longer sends the calendar and the `.ics` feed a month ahead.** Once a cycle had
  passed its usual length without a new period logged, the dashboard and the API kept naming the
  period that was due, while the calendar grid and the subscribed feed went on drawing the next
  cycles as if that period had started on time — two answers to "when is my next period" on one
  account. From the day after the usual length until predictions pause a week later, every surface
  now names only the period that is due, and nothing is projected from a start that has not been
  logged.
- **On the day a period is due, the dashboard no longer calls the cycle "1 day longer".** The
  excess over the usual range is now counted in whole days the cycle has run, so the notice starts
  the day after the predicted start.

- **No ovulation estimate is named from a period that is already due.** Next-period dates are
  projected from the median cycle length, while the out-of-date notice measures the average. On a
  history with one long cycle (27, 28, 28 and 36 days) the period was due on cycle day 29, but the
  dashboard, the API, the calendar, the `.ics` feed and webhook reminders still named an ovulation
  and a fertile window for the cycle that period would have opened — a date built on a start that
  has not happened. From the day the projected period is due, the fertility half is now withheld on
  every surface, with the late-cycle notice explaining why; the next-period estimate and the
  out-of-date notice are unchanged. Irregular range mode is not affected.

- **Irregular range mode no longer names an ovulation from a period that is already due.** Once
  the median-projected period was due (cycle day 29 on a 24–36-day history with a median of 28 and
  an average of 30), the dashboard named only the ovulation range placed from the recorded start,
  but the calendar still drew a fertile window and the `.ics` feed still sent an ovulation for the
  cycle that missed start would have opened. Neither is drawn or sent any more; the ovulation range
  and the next-period estimate are unchanged.

- Password recovery accepts the account password with surrounding spaces, as sign-in does.
- `ovumcy reset-password` reports a failure, not success, when the account was deleted between
  its lookup and the reset.

- **The recovery path for an OIDC-only account with TOTP is documented and tested.** An account
  created through the identity provider, with no local password, cannot be sent to the forced
  reset when its TOTP stops decrypting: the callback lands back on `/login` and the audit line,
  when the stream is on, records `cause=no_local_password`. The way back is the operator command
  `ovumcy reset-password --full <email>`, which clears the dead TOTP enrollment and the links and
  sets a temporary password; the owner signs in with it, changes it and relinks the provider. A
  code comment that promised a self-service escape for this account is corrected, and
  `docs/self-hosted.md` no longer claims such an account recovers on its own.

- A calendar feed whose disclaimer text is missing is now refused (it lists no events) instead of publishing events without the "not medical advice" line.

- **`ovumcy reset-password` now says what a plain reset leaves in place.** Without `--full` the
  command keeps the account's linked OIDC identities, two-factor sign-in, recovery code and webhook
  endpoint, and a linked identity can still sign in and replace the temporary password while the
  forced change is pending. The success message said none of this. It now names each of them that
  the account has, and points at `--full` for an account that may be compromised. An account deleted in the moment
  between the command's lookup and its write is now reported as not found, in the same words as an
  unknown account, instead of as a generic reset failure.

- **The compose files no longer understate what losing the fence volume costs.** Their comment
  still said it cost one round of re-generated subscribe URLs; since sessions, webhooks and local
  passwords joined the fence, losing it reads as a restore and signs every owner out, clears every
  webhook endpoint and revokes every local password, once. The self-hosting guide said the same in
  two places and is corrected too.

- **A calendar-feed rotation that fails to save no longer loses the pending reveal of the link
  still in use.** When the new token could not be stored, the response cleared the one-time reveal
  cookie the browser held for the previous, still-serving link, so a URL its owner had not yet seen
  was gone and only another rotation could show one. The failed rotation now leaves that cookie as
  it was.

- **A webhook reminder no longer leaves without its medical-safety disclaimer.** When the disclaimer
  copy resolved to an empty string, the reminder was still sent with a blank `disclaimer` field. The
  notify pass now withholds that owner's reminders, claims no watermark and logs the reason, the same
  rule the `.ics` feed applies; the reminder goes out on the first pass after the copy resolves. The
  payload shape is unchanged.

- **The egg-white badge names the observation wherever fertile details are held back.** Spotting
  logged on a day with egg-white mucus no longer shows "Best timing: fertile window" beside "Fertile
  details held back"; the badge reads "Egg-white mucus logged today", as it already did before the
  first completed cycle.

- **The egg-white badge drops its goal colour wherever fertile details are held back.** While the
  dashboard withholds the fertility half, the "Egg-white mucus logged today" badge renders in the
  neutral style for every goal: trying to conceive no longer shows it as positive, and avoiding
  pregnancy no longer shows it as a warning.

- **A bleeding day logged in the owner's own morning withholds the fertile window on every surface.**
  For an owner whose timezone runs ahead of the server's, the cycle derivation took "today" from
  the server's calendar day and dropped the owner's log for their own today. Bleeding logged
  outside a recorded cycle start that morning then failed to withhold the projected fertile window
  and ovulation, so the `.ics` feed and the webhook reminder pass could still announce them for
  a day recorded as bleeding. The derivation now takes the owner's day from the owner's timezone.

- **The dashboard and settings no longer scroll sideways at 200% text on a
  320 px phone in German, Spanish or Italian.** A long word ("Schwangerschaftstest",
  "personalizzato", "Fahrenheit") used to widen the status card, the day
  form, the symptom buttons or the temperature-unit switch past the screen;
  it now wraps or breaks inside its line, and the unit chips stack.

- **Statistics no longer scroll sideways at 200% text on a narrow phone.** The
  cycle chart's "Recent cycles" caption wraps under the chart title, and a
  symptom's frequency ("2 times (in 38 days)") drops under the symptom name
  instead of pushing the row, and with it the bottom tab bar, past the screen.

- **A page title made of one long word no longer widens a phone at 200% text.**
  The Italian statistics title ("Approfondimenti") ran past a 320 and 375 px
  screen; page titles and section headings now break a word only when it is
  wider than the line.

An oversized request (for example a language switch) no longer tells the user that "the file" is too large; the message is neutral in all six languages.

- **A projection that would fall after 9999-12-31 is reported as absent.** A cycle logged in late 9999
  projected its next period start, ovulation day and fertile window into year 10000, which no date
  field can spell: `GET /api/v1/stats/overview` could not answer it as a `format: date`, and the
  calendar feed wrote a nine-digit DTSTART. Such a date is now `null` in the API, exactly like a
  withheld projection, with `suppression` unchanged; a fertile window whose last day crosses the year
  is `null` as a whole. The dashboard, the webhook reminders, the calendar grid and the calendar
  feed leave the same dates out: the grid withholds such a window with its pre-fertile lead-in and
  marks nothing on the year-10000 days that close its last week, and the feed also drops an event
  on 9999-12-31, whose end date would fall in year 10000. The calendar page no longer navigates past
  9999-12, where its next-month link led to an invalid-month answer. Accepted day input is unchanged.
- **The 2FA enrollment docs name the status a wrong code gets.** `docs/openapi.yaml` listed `400`
  for `PUT /api/v1/users/current/2fa`; the server answers a wrong, missing or malformed code with
  `401` `totp invalid code`, and keeps `400` for a missing password. The spec now says so.

- **The December 9999 calendar no longer offers days it cannot open.** The grid's cells for
  9999-12-31 and the year-10000 days in its last week are drawn disabled, where a click used to
  request a day the server refuses.

- **An account subcommand no longer hangs on a database that stops answering its schema check.** `ovumcy users`, `reset-password`, `link-oidc-identity`, `webhook` and `notify` now give that check the same five-minute storage budget the server's boot passes have, and refuse instead of waiting forever.

- **Calendar days past the last accepted date look disabled.** In the December 9999 grid, the days
  that cannot be opened were already inert but kept the hover lift of a clickable day; they are now
  dimmed and ignore the pointer.

- **The stats cycle-length chart's caption now describes the bars it sits under.** The chart plots
  up to twelve completed cycles, but its caption took the range from the last six only, so a long
  history could read "Range 15 to 128 days" under a plotted 365-day bar. The caption's range now
  spans every plotted cycle, matching its count and its average, which already did.

A copy pass across all six locales. Every predicted-day line (the start window,
the Russian reminder banners and webhook messages) now carries an estimate word.
The rate-limit messages no longer promise a fixed wait, since the windows are
configurable. The downloaded recovery-code file follows the interface language.
French colons take their space and the short month names are the usual ones.
Italian gains "l'informativa", "Primo avvio" and "da … a …" in ranges, and the
German consent line reads as a sentence. "Save Changes" is now "Save changes".

The predicted fertile days go by one name, "Fertile window" (or the locale's
equivalent), in the calendar legend, the estimate caption and the dashboard
badges; English no longer mixes in "Fertility window" and "Fertile period".
Italian no longer addresses the reader with a feminine form in the delete
confirmation and the "stay signed in" option.

- **`GET /forgot-password` no longer renders the reset-request form for a signed-in owner.** It now redirects (`303`) to the same landing page `/login` uses; a guest still gets the form, and nothing about the session or the URL changes.

- **Three small server nits.** Every 404 answer now carries `X-Robots-Tag: noindex`; the retired `interest-cohort` token is gone from `Permissions-Policy` (browsers logged an "unrecognized feature" warning for it); a custom symptom icon of a bare `?` is refused, since `?` is the glyph the interface paints for an icon it cannot show.

- **The 2FA enrollment key is shown in groups of four with a copy button, and the language radios have real accessible names.** The seed on the enrollment page (still shown there, and only there) reads `ABCD EFGH …` and can be copied in one click; each language radio in interface settings is announced as e.g. "Deutsch (DE)" instead of a bare "de".

- **A save button now says "Saving…" while its request runs, and a form the browser refuses to submit withdraws the previous "Saved."** The `data-saving-label` the templates declared was never read, so the button kept its idle text; and native validation sent no request, leaving the earlier success standing beside the validation bubble.

- **A storage fault no longer signs the owner out.** When the database could not be read while a
  request was checking its session — a locked or briefly unavailable database — the check took the
  failure for an account that no longer exists: it cleared the session cookie and answered as if
  nobody were signed in, so a form sent without JavaScript landed on the sign-in page with a "not
  signed in" notice. That notice could then surface on a later, unrelated sign-in. A fault now
  answers as a server error and leaves the session in place; an account that is really gone is
  still signed out as before.

- **A form sent by an account the web app no longer serves lands on the sign-in page.** Such a
  session is ended on the spot, but a form submitted without JavaScript answered with a page whose
  link back to the form only bounced to the sign-in page with no explanation. It now goes straight
  to the sign-in page, which says why.

- **A day form sends the browser back to one page, whether it was saved or refused.** Without
  JavaScript, a day form names the page it came from, and the save read that name more leniently
  than a refusal did: deleting a day from a link spelled `?source=Calendar` returned to the
  calendar when it worked and to the dashboard when it was refused. Both now read it the same way.

- **A refused form no longer offers a language switch that is refused in turn.** When a form sent
  without JavaScript was turned away by the cross-site check or by a rate limit, the refusal page
  showed the language buttons in its header, but with no proof of origin left to send: every button
  led to the same refusal again. The page now offers the way back alone; a refusal the form reached
  past those checks keeps its working switch.

- **The calendar's account-changed notices are announced to screen readers.** They were inserted as
  new nodes already carrying a live role, which is not announced; the text is now written into a
  live region the calendar page renders up front. The region is emptied at once and filled a
  moment later (past one frame), so a notice repeating the previous one is announced again.
- **A refused calendar grid refresh has its own message** ("This calendar is out of date: ...")
  instead of reusing "This day can't be opened", in all six languages.

- **A refused settings restore now says why.** The restore page looked for the refusal key where the
  server never puts it, so every refusal showed "Restore failed. Please try again.". It now reads the
  key from the error envelope and shows the message for an invalid file, a file that is too large
  (including one over the request size cap), and a restore refused because another account is now
  signed in, which gets its own message in all six languages. Any other refusal keeps the generic
  message.

- **A day save no longer answers "changed on another device" after two of its own saves cross.** When
  an autosave and a Save of the same day were in flight together and the older one answered last, the
  page took that older answer as what the server held, and the next edit was refused as a conflict.
  The page now keeps the newest accepted save as its base, whichever answer arrives last.
- **A confirmed cycle start is sent with the save that confirmed it, not with every later save.**
  Ticking "this starts a new cycle" used to ride along on each later edit of that day, so an unrelated
  change could mark the day as a cycle start again after it had been un-marked.
- **A day save reads the onboarding period start inside its own write.** A mood or note saved on the
  onboarding start date, with no entry for that day yet, could turn it into a period day from a start
  Settings had just withdrawn; the start is now read in the same transaction as the day it shapes.

- **The JSON answer of `POST /api/v1/users/current/webhook` now reports the settings as saved.**
  `webhook_enabled`, `notify_period` and `notify_ovulation` were copied from the request, so a save
  with `webhook_remove_url: true` (which forces delivery off) could still answer
  `"webhook_enabled": true` while the stored row said otherwise. The three flags are now read back
  after the write. The set of fields is unchanged, and the endpoint URL is still never returned.

- **A cycle start recorded for a future day no longer ends a pregnancy pause early — and no longer
  ends it on the webhook alone.** A positive pregnancy test pauses predictions everywhere until a
  cycle start is logged after it, and a cycle start may be entered up to two days ahead. Those two
  rules met in the wrong place: the pause is resolved over whatever set of day logs the surface
  handed the shared derivation, and only some surfaces bounded that set at today. The dashboard,
  the calendar and the `.ics` feed fetch a range that ends today, so they went on showing the pause.
  The webhook notification pass loads the whole stored history, so tomorrow's start lifted the pause
  for it alone: an account could read "paused" on every screen while an outbound `period-soon`
  notification left for their endpoint saying the predictions had resumed.

  The bound now sits in the derivation every surface shares rather than in each caller's fetch, so
  the pause — and the cycle statistics around it — are resolved over one timeline that ends on the
  account's own today, in the account's own timezone. A day logged ahead of today still stores,
  still displays, and still counts from the day it arrives; it simply no longer decides anything
  before then. Nothing changes for an account whose recorded days are all in the past.

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

- **Breaking (API shape): a compressed request body that only passes the 16 MiB cap once decompressed
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

- **A 2FA sign-in can no longer undo a re-enrollment made at the same moment.** A successful 2FA
  check against a TOTP secret stored before per-account binding reseals that secret in place,
  without revoking sessions. The reseal was written unconditionally, after the code check, so a
  re-enrollment or a disable landing in between was overwritten by the OLD secret, and the retired
  authenticator passed 2FA again. The reseal now applies only while the stored ciphertext is still
  the one the check opened; when a re-enrollment or disable wins, the reseal is dropped and the
  check still succeeds.

- **A sign-in can no longer undo a password change made at the same moment.** A successful login
  against an account whose stored hash predates the current bcrypt cost upgrades that hash in place,
  without revoking sessions. The upgrade was written unconditionally, after a full cost-12 hash, so
  a password change or reset landing in between was overwritten by a fresh hash of the OLD password,
  and the replaced password signed in again. The upgrade now applies only while the stored hash is
  still the one the login verified; when a change wins, the upgrade is dropped, the login still
  succeeds, and the session it minted is revoked by the change as before.

- **A security change made from Settings no longer outlives a revocation that lands while it is
  being made.** Changing the password, enrolling a local password, regenerating the recovery code,
  turning two-factor sign-in on or off, clearing data, and linking or unlinking an OIDC identity
  each revoked the account's sessions by incrementing whatever version they found, so a sign-out
  everywhere or another device's password change committed after the request was authenticated
  was folded into the change's own bump, and the change went through on a session that had
  already been revoked. Each now revokes only from the version of the session that made the
  request: if the account's sessions were revoked in between, nothing is changed and this device
  is signed out. A link or unlink that a revocation follows is kept, and this device is signed out
  rather than handed a session that would outlive that revocation. The owner signs in again and
  repeats the change.

- **Breaking (API shape): a password-reset link no longer outlives local sign-in.** Turning local public authentication
  off (`OIDC_LOGIN_MODE=oidc_only`) stopped the recovery flow from *starting* — `/forgot-password`
  refused — but nothing stopped a reset that had already started. A token minted the minute before
  the switch still redeemed: it rewrote the password, turned the account's local sign-in back on,
  and handed out a session on an instance the operator had just closed to local sign-in. The redeem
  now follows the same gate as the flow that mints it, and `/reset-password` follows both. A
  **forced** reset is untouched: an account carrying "must change password" reaches that page
  through SSO, which is precisely what still works in `oidc_only`, so refusing it would strand the
  owner with no way to clear the flag.

- **The pages that spend a one-time value now check who asked.** `/register/welcome` exchanges the
  registration hand-off for a session, `/settings/calendar-feed` reveals the subscribe URL exactly
  once, and the recovery code is shown exactly once on its own page and inline after signing up; all
  of them are ordinary page loads, and a browser sends cookies with a page load started from someone
  else's site. Neither could be made to hand anything to that site — the
  single-use marks behind them already saw to that — but both could be made to *burn* a value the
  owner had not seen yet: a link that quietly finished her registration, or spent her one look at
  the subscribe URL or the recovery code. Each of them now checks who started the request and
  answers anything but a first-party one the way it answers a stale one, leaving the value unspent
  for the owner's own visit. A first-party app fetching those pages the documented way still gets them, and a browser
  too old to say who started a request is served as before.

- **A recovery reset no longer remembers a device nobody chose.** Signing in remembers a browser
  for 30 days only when the sign-in form's *Remember me* box is ticked, but two paths that carry no
  such box — finishing a password recovery and picking up a fresh registration — asked to be
  remembered on the owner's behalf. Both now keep their session for the browser session, the same
  default an unticked box gets. The same choice was also being thrown away in the other direction:
  changing the password, turning two-factor on or off, or clearing the data re-issues the session,
  and that re-issue always came back session-scoped — so a browser the owner had asked Ovumcy to
  remember quietly stopped being remembered, on exactly the screens where she was least likely to
  notice. The re-issue now carries the original choice forward, and ticking the box on the sign-in
  form works as it always did.

- **Restoring a backup no longer brings a revoked calendar feed back to life.** Revoking or
  rotating an `.ics` subscription cleared the feed columns and nothing else, so restoring a backup
  taken before that revocation returned them exactly as they were: the old subscribe URL served the
  owner's calendar again, under the same application secret, with nothing in the restored data
  recording that a revocation had ever happened. The behaviour was documented as a step the
  operator had to remember after every restore — re-check each owner's feed and revoke it again —
  and only the account itself can see or change its own feed, so an owner nobody told kept a live
  subscription they believed was gone.

  Ovumcy now settles it without the operator in the loop. A **restore fence** keeps a marker in two
  places, one inside the database and one in a file outside it, and advances both together on every
  change to the set of armed feeds. A restore rolls back only the copy inside the database, the two
  disagree, and the instance disarms every armed calendar feed on the boot that follows — before it
  accepts a request, so no poll can race it. Owners re-generate their subscribe URL from
  `Settings → Calendar feed`. This works on SQLite and on PostgreSQL alike, and does not depend on
  the application secret changing: the existing key-epoch sentinel covers a rotated `SECRET_KEY`
  and structurally could not cover this, because the epoch it compares is restored together with
  the feed columns it is meant to judge.

  **Operators: this needs one change to your deployment.** The fence file has to live somewhere your
  database backups do not capture, which is the whole mechanism. The bundled compose stacks now
  mount a separate `ovumcy_fence` volume for it, and the image points `CALENDAR_FEED_FENCE_PATH`
  there by default. Take that volume from the updated compose file for your stack. Never include it
  in a backup and never restore it beside the database — a fence restored together with the
  database agrees with it and detects nothing. It holds no health data and no secrets, but losing it
  reads as a restore: the next start disarms every feed, signs every owner out, clears every webhook
  endpoint and revokes every local password, once.

  **Run the operator CLI where the server's fence is visible.** `ovumcy reset-password` and
  `ovumcy users delete` both remove calendar-feed access, and now confirm and advance the same
  fence before that removal is allowed to happen — never merely warn about it. Through
  `docker compose exec` on the running container they already see it; from a host shell that
  does not, they refuse instead of completing, naming the variable and what to run instead, and
  change nothing.

  An instance with nowhere to keep the fence — the image pulled without the new volume, or the
  binary run outside compose with the variable unset — starts normally and keeps serving, but
  disarms every armed calendar feed on each start and says so in its startup log, naming the
  variable to set. Without a fence a restored backup cannot be told apart from the database it
  replaced, so the feed fails closed rather than quietly returning to the old behaviour. The two
  operator subcommands above are the exception to "keeps working": with no fence to advance,
  `reset-password` and `users delete` refuse rather than erase without a record, so an instance
  that has never started with `CALENDAR_FEED_FENCE_PATH` configured has to be started once with
  it before either can run. Full contract: `docs/self-hosted.md → Calendar Feed Restore Fence`.

- **A server that ran without a calendar-feed restore fence now says so in its own database, and the
  first start with a fence acts on it.** Until now, a database that had only ever run unfenced looked
  exactly like a brand-new one — both halves of the fence empty — so mounting the fence for the first
  time adopted whatever feeds it found. An owner who revoked their `.ics` subscription on the unfenced
  instance, on a database later restored from a backup taken before that revocation, got the old
  subscribe URL back and nothing recorded it.
- **Operators:** the first start after you mount the fence on an instance that had been running without
  one — over a database that holds no fence token — disarms every armed calendar feed once, and the
  startup line says that is what happened. It is expected; owners re-generate their subscribe URLs from
  `Settings → Calendar feed`, and later starts disarm nothing.

- **Breaking (API shape): credentials, sign-in flags and CSRF tokens are read from the request body only, never from the URL.**
  The sign-in `remember_me` flag, the 2FA challenge code, the 2FA enrollment code, the 2FA disable
  password, the CSRF token and the email echoed back on a failed forgot-password form were read
  through a lookup that searches the query string before the body. A link such as
  `/api/v1/sessions?remember_me=1` therefore turned a session-scoped sign-in into a 30-day one, and
  a code, password or token in a URL was accepted and left in logs, history and referrers. All of
  them now come from the JSON or form body, and a value in the query string is ignored:
  `POST /api/v1/sessions?remember_me=1` with a false or absent flag issues a browser-session cookie,
  a request whose CSRF token is only in the URL is refused with `403`, and a 2FA code or password
  only in the URL is treated as missing. The CSRF token belongs in the `X-CSRF-Token` header or the
  `csrf_token` form field, the only two the API reference declares. Browser forms are unaffected.
  `PUT` and `DELETE /api/v1/users/current/2fa` now also accept the JSON body the other
  password-protected settings calls take (`{"password", "code"}` and `{"password"}`), and the API
  reference declares it. Those inputs are read from JSON, urlencoded and multipart bodies only: sign-in,
  registration, password change, password reset and the 2FA calls now refuse a body of any other
  type (XML, CBOR, MessagePack, a vendor `+json`) instead of decoding it, and so does a
  password-protected settings call whose body the decoder could not read whole.

- **The calendar feed no longer receives a cookie it was never supposed to carry, on any spelling
  of its URL.** `GET /calendar/feed/:token.ics` is a cookieless, capability-token route by design
  — a calendar client presents no session and no CSRF token, and `docs/SECURITY_INVARIANTS.md`
  already required the identical `404` it answers an unknown token with to carry no `Set-Cookie`;
  that requirement now reads on every outcome of the route, which is what this fix makes true.
  Every GET or HEAD to the feed's own URL — whatever its case or trailing slash — used to be handed
  cookies anyway: an `ovumcy_csrf` cookie carrying a fresh CSRF token, and, whenever the request
  named a timezone, an `ovumcy_tz` cookie recording it, both set before the request had any reason
  to carry either. Nothing about who may poll the feed changes — a calendar client was never going
  to present a CSRF token or read a cookie back — so it now polls exactly as before, with two
  cookies fewer riding along for no reason.

  The exclusion is scoped precisely to the feed's own concrete URL: a path that merely starts with
  its prefix but names no real token — a bare `/calendar/feed/`, or a nested path segment — keeps
  answering the ordinary "page not found" screen with its language and CSRF token exactly as before,
  whatever its own case or trailing slash.

  One consequence is deliberate: the feed no longer reads a timezone from the polling request at all.
  The calendar day it renders comes from the owner's saved timezone, as before; an owner whose timezone
  the app never captured now gets the server's zone instead of whatever zone the request happened to
  name, so the same subscribe URL renders the same day in every client.

- **Go toolchain bumped to 1.27.1.** The module's `go` line (`go.mod`) and the runtime image's
  builder stage (`golang:1.27.1-alpine3.24` in the `Dockerfile`) both move to it, which clears the
  standard-library advisories govulncheck reported as reachable from this code on 1.26.5: quadratic
  complexity in `net/url` `resolvePath` (GO-2026-6218), JavaScript regexp context tracking in
  `html/template` (GO-2026-6091), unbounded post-handshake messages in `crypto/tls`
  (GO-2026-6090), missing recursion-depth guards in `encoding/xml` (GO-2026-6088) and
  `encoding/asn1` (GO-2026-5972), and ASCII-only Punycode labels in `golang.org/x/net/idna`
  (GO-2026-5026). Four of the six are reached only through outbound paths an operator configures —
  webhook delivery and the OIDC CA file. The final runtime stage is unchanged.

- **A release tag no longer publishes an image on its own say-so.** Pushing `v*` starts no CI run —
  CI is triggered by pushes to `main` — so the tag went straight to build-and-push with only the
  pre-publish vulnerability scan between it and the registry. A tag on a commit that never merged,
  or on one whose checks went red, produced a signed, attested release image regardless. The publish
  workflow now runs a gate first: the tagged commit must be contained in `main`, and the same five
  checks that `latest` waits on — `test`, `race`, `e2e`, `e2e-postgres-smoke` and `image-smoke` —
  must have passed on the run that judged it on `main` itself. A merge-queue run's verdict is not
  accepted in their place: it does not run the image or Postgres smokes at all. A tag that fails
  either condition fails the workflow and publishes nothing; there is no override. Publishing
  `latest` from `main` is unchanged — that path already waited on these checks.

- **The 2FA login challenge and the password-reset redeem now sit under their own edge rate
  ceiling, like every other credential-verifying route.** Both endpoints verify a credential — a
  TOTP code, and a signed reset token — but drew only on the general `/api` catch-all budget (300
  requests/min), with no ceiling of their own. Neither route has a service-level attempt budget
  behind it, so the catch-all was the only bound on guessing TOTP codes or reset tokens against
  them. Both routes now share the credential rate ceiling (`RATE_LIMIT_TOTP_CHALLENGE_MAX`/`WINDOW`,
  `RATE_LIMIT_PASSWORD_RESET_REDEEM_MAX`/`WINDOW`, defaulting to 8/15min like login) through the
  same `getCredentialRateLimit` check as login, registration and forgot-password.

- **The documentation no longer implies that `HOST_BIND_ADDRESS` limits where the server listens.**
  Only the base `docker-compose.yml` reads it, as the host side of the port publish. The binary
  ignores it and listens on `PORT` on every interface, both inside the container and when started
  directly or with `go run`, so `HOST_BIND_ADDRESS=127.0.0.1` in `.env` did not keep a direct run off
  the network. The README, the self-hosting guide, `.env.example`, the security invariants and the contributing
  guide now say this. They tell a direct run to block the port with a host firewall, and a plain `docker run` to
  publish it as `-p 127.0.0.1:8080:8080`. Runtime behaviour is unchanged.

- **A start without a usable calendar-feed restore fence now drops the database half of the fence
  marker as well as stamping the database.** An instance that WAS fenced and then lost
  `CALENDAR_FEED_FENCE_PATH` for a start — while its data volume, and the fence file on it, were kept
  — left both halves holding the same token: nothing advances either half while no fence is
  configured. A backup taken in that gap, restored beside the untouched file, therefore compared
  equal, the boot read continuity, and the stamp — consulted only where both halves are empty — was
  never reached. An owner who revoked their `.ics` subscription during the gap got the old subscribe
  URL back, serving again.
- **Operators:** the first start that has the fence back after a start without it disarms every armed
  calendar feed once — including feeds armed while the fence was missing — and then re-arms; the
  startup line names it. Owners re-generate their subscribe URLs from `Settings → Calendar feed`, and
  later starts disarm nothing. Until that start, `ovumcy users delete` and a forced
  `ovumcy reset-password` refuse, because the marker they confirm is not there yet.

- **`GET /calendar` no longer accepts an unbounded month.** A syntactically valid but far-future
  month (`?month=9999-12`) made `BuildCalendarDayStates` walk cycle predictions with no upper
  bound, costing 1.36 s and over 3.1M allocations per request on an otherwise ordinary page.
  Navigation is now clamped to three years ahead of today, mirroring the existing three-year lower
  bound: a request past the bound renders the clamped month instead of the one asked for, and the
  "next" link disables at the upper edge exactly as "prev" already does at the lower one.
  Independently of the clamp, cycle projection itself now stops after a fixed number of cycles
  regardless of how far the grid asked it to go, and the page has its own rate limit
  (`RATE_LIMIT_CALENDAR_MAX` / `RATE_LIMIT_CALENDAR_WINDOW`, default 300 requests per minute),
  keyed the same way as the other authenticated routes.

- **A crafted link can no longer plant its query parameters in the page it opens.** Every page
  renders the address it was requested at back into its own markup — the language switcher carries
  it as the `next` field, and the footer privacy link carries it into a new outgoing URL. That
  address was taken from the request verbatim, so opening `/login?email=someone@example.com` echoed
  that address into the page, and following the footer link carried it onward into browser history,
  the `Referer` header and the `/privacy` entry in the server access log. Nothing stored was
  exposed and the value was never trusted for navigation, but a value the visitor never typed had
  no business being shown or forwarded. The rendered address now keeps only the parameters the
  pages actually read — the calendar's `month`, `day`, `selected` and `edit`, the onboarding
  `step`, and the privacy page's `back` — and each surviving value must additionally look like what
  its own page accepts: a real month, a real date, a real step. Keeping the names alone was not
  enough, because the same address could simply be posted under an allowed name instead
  (`?step=someone@example.com` rendered exactly like `?email=` did). Anything else is dropped
  whatever it contains, as is a `#` fragment. Because `back` holds a path of its own, it is
  filtered by the same rules one level deeper and must point at a page this app actually serves;
  the privacy page's back link goes through that one decision too. A query the server cannot parse
  now yields the bare path rather than being passed through. Normal navigation is unchanged: the
  calendar keeps its month while you switch languages, and the privacy page still returns you where
  you came from.

- **Signing out now also clears the remembered interface language.** The `ovumcy_lang` cookie
  outlived a sign-out by up to a year, so on a shared or borrowed browser the login page still
  disclosed that Ovumcy is used on this device, and in which language, before anyone signed in.
  Ending a session on purpose — sign-out, or deleting the account — now retracts it along with the
  session cookies. A session the server merely refuses (an expired or invalid cookie) deliberately
  leaves it alone, so a visitor whose session lapses mid-use keeps reading the login page in their
  own language.

- **A "page not found" no longer echoes the address it was opened at.** Every page renders the
  address it was requested at back into its own markup — the language switcher carries it as the
  `next` field, and the footer privacy link carries it into a new outgoing URL. On a page that is
  not found there is no route behind that address, so all of it comes from whoever wrote the link:
  opening `/someone@example.com/nowhere` showed that address in the page and, through the footer
  link, carried it onward into browser history, the `Referer` header and the `/privacy` entry in
  the server access log. Nothing stored was exposed and the value was never trusted for navigation
  — it came from the visitor's own link — but a value nobody typed had no business being shown or
  forwarded. The not-found page now renders a fixed address of its own, so nothing from the request
  reaches the markup. Switching language there still returns you to the same page, and the footer
  privacy link still appears; because that fixed address names no section, the navigation marks no
  current section on a page that is not one, which is how it already behaved.

- **Confirming an OIDC identity link no longer hands out a session on an oidc-only instance.**
  `/auth/oidc/link/confirm` checks a local password to confirm a pre-existing account before
  linking it to an OIDC identity — the same anti-takeover step `Login`, `ForgotPassword`, and the
  auth pages already refuse to run once an operator turns `OIDC_LOGIN_MODE=oidc_only` on. This route
  was the one place that check still ran anyway, and a correct password still ended in a live
  session. The identity link itself stays reachable — it is the only way a pre-existing
  local-password account ever links to OIDC once local sign-in is off, and refusing it would strand
  that account for good — but the link no longer signs the browser in directly: the account signs in
  on its next OIDC round-trip instead, through the identity it just linked.

- **Breaking (auth behavior): signing in through OIDC to an account that already has TOTP enabled
  now requires the second factor, the same way local sign-in does.** The OIDC callback resolved an
  already-linked `(issuer, subject)` straight to a session — `CompleteOIDCLogin` had no TOTP check
  anywhere on that path, unlike the local login route, which redirects a TOTP-enabled account to
  `/auth/2fa` before ever issuing a cookie. `OIDCLoginService.Authenticate` now reports
  `RequiresTOTP` the same way `LoginService.Authenticate` reports it for local login, and the OIDC
  callback gates on it before `setAuthCookie`, ordered the same way: an account also carrying
  `must_change_password` still goes to the forced-reset flow first, never to the 2FA challenge —
  the sanctioned escape hatch for a TOTP secret that can no longer be decrypted after a `SECRET_KEY`
  rotation is unaffected. Operators whose SSO users have TOTP enabled will see those accounts
  challenged for a code on their next OIDC sign-in where they were not before.

- **The page that forwards you to your identity provider after sign-out now names the account it
  acts for.** That page runs when your session is already gone, so all it had to go on was the
  session identifier carried in its own sealed cookie — and it looked the stored sign-out record up
  by that identifier alone. On an instance hosting several independent owners, a cookie carrying one
  owner's browser and another owner's session identifier would have been handed the second owner's
  provider token and consumed their record in passing. The cookie now carries the account as well,
  and the record is found only by the two together: a mismatched pair gets an ordinary local
  sign-out and leaves the other owner's record untouched. A cookie naming no account at all is
  refused and cleared rather than treated as naming any account.
- **A sign-out started in the minute before this update finishes locally instead of at the
  provider.** The sealed cookie behind that page is valid for one minute, and one minted by the
  previous version carries no account. Rather than guess, the server refuses it, clears it, and
  signs you out here; the provider session ends at its own expiry or on the next sign-out. Nothing
  is left readable in the browser, and only cookies minted inside that one-minute window are
  affected.

- **The provider sign-out record is now read and deleted per account, never by session alone.** An
  instance can host several independent owners, and the record kept so you can be signed out of
  your identity provider holds the token that provider issued for that session. It was looked up —
  and deleted — by the session identifier on its own, with nothing tying it to the account it
  belongs to. There is no known way for one owner to obtain another's session identifier, so this
  closes the gap rather than a demonstrated leak, but a boundary that holds only because the key is
  hard to guess is not a boundary. Every read and every delete now names the account as well, and a
  request that names none is refused instead of matching them all. The one page with no session
  left to read the account from — the same-origin hop that forwards the browser to the provider
  after sign-out, which runs once the session is already gone — takes it from its own sealed
  cookie instead, per "A sign-out record that belongs to nobody can no longer be written."
- **A sign-out record that belongs to nobody can no longer be written.** Deleting an account erases
  these records by account, so one stored without an owner would outlive the account it came from,
  keeping the provider token for up to a week after the erasure. Both the place that creates one at
  sign-in and the storage layer beneath it now refuse to write a record with no owner.

- **Clearing your data no longer re-arms automatic period fill.** The reset written inside the
  erasure transaction set auto-period-fill back on, so an account wiped by an owner who had turned
  inference off resumed manufacturing period days the owner never logged. Erasure has to hold
  across the transitions that follow it; the wiped account now starts from the same default a new
  one does.

- **Signing out now retracts `ovumcy_tz`, the browser's timezone cookie, as it already did the
  language cookie.** Both are plaintext, neither is sealed or scoped to a session, and left
  behind on a shared or borrowed browser they tell the next visitor that this app is used here,
  in which language, and from which region — before anyone authenticates. Logout, account
  deletion and the other deliberate session ends now clear both; a session the server merely
  *refuses* still leaves them alone, which is the boundary that keeps a lapsed visitor's login
  page in their own language.

  The retraction needed its client half to mean anything: the timezone bootstrap script wrote
  the cookie synchronously on every page it loaded on, and every htmx request carried the
  `X-Ovumcy-Timezone` header the middleware re-issues the cookie from — so the login page the
  sign-out redirects to put the cookie straight back. The bootstrap script is now served only on
  a page rendered for a session, and the header rides only on those requests. On the pages a
  session renders the cookie is written and sent as before, and no anonymous surface renders a
  date to need it. (Corrected after release: this entry also said nothing changes for a
  signed-in owner, which is not so. The server does not fall back to the time zone stored on the
  account, so the first page after signing back in — the cookie retracted, the bootstrap not yet
  run — or in a fresh browser computes "today" in the instance's time zone, and a day saved from
  it can land on the neighbouring date for an owner whose zone differs.)

  Regressions: `TestLogoutRetractsTheTimezoneCookie` (with and without the request header),
  `TestSessionRejectionLeavesTheTimezoneCookieInPlace`, and
  `web/src/js/__tests__/timezone-cookie-signed-in-only.test.mjs`.

- **The single sign-on page must now belong to the configured provider.** Ovumcy already refused
  a provider whose discovery document placed the token and key endpoints on another host, but it
  still followed the sign-in page address wherever that document pointed — so a tampered document
  could send the browser, with the sign-in request attached, to a look-alike page on a host the
  provider does not own. The sign-in page is now held to the same rule: same scheme, host and port
  as `OIDC_ISSUER_URL`, or single sign-on stays unavailable. A provider whose sign-in page lives on
  a separate host needs `OIDC_ISSUER_URL` pointed at an issuer whose endpoints share its origin.
  The provider sign-out address saved for a session is checked against the configured issuer again
  before the browser is sent there, so after the issuer's origin (scheme, host or port) changes,
  signing out of an older session is local only. The address the provider returns the browser to
  after sign-out is now always the one configured now, never the one saved with the session.

- **An OIDC address written in non-ASCII characters is refused.** A host such as the full-width
  `０.０.０.０` passed the "must name a remote host" check, yet HTTP clients and browsers convert
  it to `0.0.0.0` before connecting. Any host containing a non-ASCII character is now rejected; an
  internationalized domain keeps working in its ASCII `xn--` form.

  **Upgrade note:** if `OIDC_ISSUER_URL`, `OIDC_REDIRECT_URL` or `OIDC_POST_LOGOUT_REDIRECT_URL`
  names an internationalized domain in its Unicode spelling (for example `https://idp.bücher.de`),
  Ovumcy now refuses to start with "host must be ASCII". Replace the host with its `xn--` form
  (`https://idp.xn--bcher-kva.de`) — the same address, in the spelling clients already connect to.

- **A day entry, custom symptom, or OIDC last-used stamp saved with no owner now refuses instead of
  silently doing nothing.** `DailyLogRepository.Save`/`UpdateSymptomIDs` and `SymptomRepository.Update`
  scope their write by the row's own `user_id` in the query — the guard that stops one owner's write
  from overwriting another owner's row — but a `UserID` of zero was never checked: `WHERE user_id = 0`
  ordinarily matches no row at all, so the write returned success having touched nothing. All three now
  refuse a zero owner with an error. `OIDCIdentityRepository.TouchLastUsed` gained the same owner-scoped
  `WHERE`, previously matching by identity id alone, so a stale or foreign identity id could stamp
  `last_used_at` on a row it does not own; the method now takes the caller's `userID` and combines it
  with the identity id before writing, matching zero rows for a foreign id instead of touching the wrong
  owner's row.

- **Refused logout requests no longer spend the per-IP logout budget.** The per-IP row on
  `DELETE /api/v1/sessions/current` refuses before the handler runs. Until now it counted every
  request, so anyone behind the same address (a household NAT) could spend it with sixty
  unauthenticated `DELETE`s. Every other owner's API sign-out was then refused for the rest of the
  window, with the session still alive. The row now counts only answers below 400. Successful
  sign-outs still spend it, and each account's own logout budget still bounds them per owner.
  Concurrent refused requests can still keep the row exhausted until the window ends: the limiter
  counts a request while it is in flight, and one it answers `429` itself keeps its count.
  Regression: `TestLogoutEdgeBudgetIsNotSpentByRefusedRequests`.
- **An API path spelled in another case is refused as an API request.** The router matches paths
  case-insensitively, but the session gate decided between a JSON refusal and the sign-in redirect
  on the raw path, so `DELETE /API/v1/sessions/current` without a session was answered `303` —
  below 400, so the per-IP logout row still counted it. A route behind the session gate reached
  with an uppercase letter in its `/api/` segment (`/API/…`, `/Api/…`) and no JSON `Accept` header
  is now refused with a `401`/`403` JSON answer instead of a redirect to `/login` or `/onboarding`;
  other spellings (`/api/V1/…`, a trailing slash) were already refused as JSON. One gap remains: the
  onboarding exemption still compares the raw path, so a session that has not finished onboarding
  gets `403` `onboarding_required` on `DELETE /API/v1/sessions/current` and `/API/v1/onboarding/…`,
  where the lowercase spelling succeeds. Regressions:
  `TestLogoutEdgeBudgetIsNotSpentByCaseVariantRefusals`,
  `TestAuthRequiredRefusesACaseVariantAPIPathAsAnAPIRequest`.

- **The container healthcheck/readycheck probe no longer trusts the port it checks to answer with a bounded response header block.** The probe's `http.Client` left `MaxResponseHeaderBytes` at Go's 10 MiB default, so anything able to bind the loopback port a healthcheck polls could make the probe buffer far more header data than a status-code check needs. The response header block is now capped at 16 KiB, matching the sizing already used for outbound webhook delivery responses.

- **`X-Ovumcy-Timezone` no longer costs a zoneinfo read on public or unauthenticated requests.**
  The header (and its `ovumcy_tz` cookie fallback) admits any short identifier, so a caller sending
  a distinct name on every request paid one zoneinfo file read and parse per request, on every
  route without a rate limit — every HTML page and the unlimited `/api/v1` handlers. Resolution now
  runs only once a request's session has verified, so an anonymous request (or one carrying a
  forged session cookie) takes the server's configured fallback zone instead of buying a zoneinfo
  read.

- **A cross-site hit on the OIDC sign-in return can no longer erase a pending settings or auth
  message.** `/auth/oidc/callback` cannot require a CSRF token — a provider has to be able to post
  back to it from another site — so an unrelated or malformed request arriving there used to
  overwrite whatever flash message a same-origin page redirect had just queued (a "settings saved"
  banner, a forced sign-out notice), and the owner's next page load showed the callback's refusal
  instead, or nothing at all. Refusals from that route, from every `/auth/oidc/*` path (including a
  sign-in rate limit), and from the other requests reachable without a CSRF token or a same-origin
  proof now travel on their own channel: a same-origin page's pending message is shown first, and a
  genuine provider refusal still reaches the owner whenever nothing else was pending.
- **Every rate limiter, not only the sign-in one, now answers a token-less refusal on the same
  channel.** Every login, registration, password-reset, logout and generic-API rate limiter runs
  ahead of CSRF checking, so any of them — not just the sign-in one the previous fragment named —
  could be tripped by a cross-site, token-less flood and used to plant a message on the same-origin
  flash slot; the password-reset limiter also echoed the request's own `email` field into that
  message. All of them now answer on the token-less channel described above, and the echoed email is
  dropped rather than carried across. The one visible change: a rate-limited password-reset
  submission no longer re-shows the email address the owner just typed on the redirect back to the
  form — she re-enters it once.

- **A revoking write against an account whose session-version counter was stuck at 0 (only
  possible on a hand-edited or restored row; every account has carried at least 1 since migration
  008) now actually signs out every other device.** The counter's own bump used to write `0 + 1 =
  1`, the exact version every session already granted against that row was already reading as
  current, so a password reset, a recovery-code regeneration, a password change, a two-factor
  change, a clear-data wipe, or an OIDC identity link/unlink on such a row left every other session
  signed in. A migration raises any row still at 0 or below to 1 on startup, and every one of those
  compare-and-set writers — the password-reset path and the one shared by the rest — no longer
  treats a stored 0 as a match for version 1.

  Operator note: once this migration is recorded, a rollback to a binary older than it is refused
  at boot; roll back by deleting the migration's `schema_migrations` row (it is idempotent and
  reapplies cleanly) or by restoring a pre-upgrade backup. If a row is later hand-edited back to 0
  or below, every session-revoking write against that account (password change, recovery-code
  regeneration, TOTP re-enrollment, clear-data, OIDC identity link/unlink) refuses until a
  sign-out-everywhere or logout moves the counter past 0; a `+1` write landing on such a row in the
  meantime revokes nothing.

- **Day, symptom and sign-in-identity rows are refused at write time when they name no owner.**
  `DailyLogRepository.Create`/`CreateBatch`, `SymptomRepository.Create`/`CreateBatch` and
  `OIDCIdentityRepository.Create` now return an error instead of writing a row that no
  owner-scoped read and no account erasure could ever reach again. The symptom rows seeded while
  a new account is created run the same check, so the registration path is covered along with the
  repository's own inserts. Onboarding's day-completion update is now also scoped by owner in its
  query, matching the rest of the day-write path, and `CompleteOnboarding` itself refuses a zero
  owner rather than reporting success for writes that touched nothing.

- **A zero owner id could no longer be told apart from a real one on the users table.**
  Every UPDATE against a user's row was scoped by `WHERE id = ?` with no guard on that id being
  zero: such a write matches no row and reports success, so a caller handed a zero id (an
  unauthenticated or mis-derived owner reference) would be told its write landed while nothing
  changed. All twenty-one of these writes — display name, timezone, interface language, reminder
  and webhook settings, calendar-feed token issue/clear, password/recovery/TOTP rotation and
  revocation, onboarding, clear-data, and the generic settings updater — now build their query
  through one helper that refuses a zero id up front, so the refusal cannot be dropped by a future
  call site without a test noticing. Three more writers that build a compound `id = ? AND ...`
  compare-and-set clause (webhook delivery mark, webhook watermark release, calendar-feed
  verifier-MAC backfill) shared the same hole — a zero id was indistinguishable from a normal
  zero-row CAS miss — and now call the same refusal directly before building their query.
  Calendar-feed token issue and clear now refuse a zero id before advancing the restore fence,
  not after: a fence advanced for a revocation that never happened made a restore from any
  earlier backup disarm every armed feed. Account deletion refuses a zero id for the same reason:
  it erased nothing, reported success, and still advanced the fence.

- **The ignore files now cover the sensitive paths the deployment documentation tells the operator
  to create.** `docker-compose.yml` has the operator place a file-backed app secret key under
  `./secrets/` and a private OIDC CA bundle under `./certs/`, and the backup contract writes
  database dumps and volume archives into `./backups/` while requiring they be treated as sensitive
  health data — none of those paths, nor the usual private-key and keystore extensions, were
  ignored, so following the documented procedure left a secret or a dump one inattentive `git add`
  away from a commit, and inside the context of a local `docker build`. Both `.gitignore` and
  `.dockerignore` now carry the class. A bare `*.sql` rule is deliberately absent: the tracked
  migration directory is SQL and is copied into the image, and an extension rule would swallow a
  new migration without saying so. No file of the class is tracked, and no tracked file became
  ignored; whether one was ever added and later removed is a question only a full-history scan
  answers, and that scan is a release step of its own.

- **Go toolchain bumped to 1.27.2 and `golang.org/x/net` to v0.60.0.** The module's `go` line
  (`go.mod`) and the runtime image's builder stage (`golang:1.27.2-alpine3.24` in the
  `Dockerfile`) both move, which clears the advisories govulncheck and Trivy reported as reachable
  on 1.27.1: HTTP/2 connection handling in `net/http/internal/http2` and `golang.org/x/net`
  (GO-2026-6617 / CVE-2026-78669), a crafted-request denial of service in `net/http`
  (CVE-2026-78667) and a `crypto/tls` denial of service (CVE-2026-97031). The server's listener
  and the outbound OIDC transport both reach the HTTP/2 path. The final runtime stage is unchanged.

- **Go toolchain bumped to 1.27.3.** The module's `go` line (`go.mod`) and the runtime image's
  builder stage (`golang:1.27.3-alpine3.24` in the `Dockerfile`) both move, which clears
  CVE-2026-111943 (HIGH) in `crypto/x509`: certificate chain verification could accept an
  untrusted intermediate, opening the outbound TLS connections — the OIDC provider's discovery,
  token and JWKS endpoints, and webhook delivery to an HTTPS receiver — to a man-in-the-middle.
  The final runtime stage is unchanged.

The HTML fragments the server builds by hand for HTMX requests (error and
success status messages on the settings, day, import and two-factor forms, and
the "page not found" fragment) are now sent as `text/html; charset=utf-8`.
They used to go out under the framework's default `text/plain`, so a browser
that opened one of those responses directly showed the markup as text. All such
responses now pass through one helper that sets the type, and a test fails if a
handler sends a hand-built body any other way.

- **An oversized JSON restore is now refused before its entries are decoded.** The 20 000-entry cap
  on `/api/v1/imports/json` was checked only after the whole payload had already been unmarshalled
  into fully typed day records, so a crafted file packing far more than the cap into many small
  entries paid for decoding every one of them before the size refusal ever ran. The entry count is
  now read from a first, cheap pass over the raw JSON array, and the per-entry decode — the actual
  cost, one struct with several nested slices per entry — only runs once that count has already
  cleared the cap. The 16 MiB request body limit already bounded the wire size; this closes the gap
  between "fits under that ceiling" and "cheap to look at". Wire contract is unchanged: same 413
  and `error_detail.key: "import file too large"` for an over-cap file, same acceptance for anything
  under it.

- **Linking a sign-in identity now asks for the account password, and a link can be removed.**
  Linking an SSO identity from Settings sent the owner to the identity provider to sign in again,
  which proved control of the identity being linked — but not of the account. Someone holding a
  stolen session could link their own provider account and keep a permanent way back in. The link
  now also requires the account's current password, and an account without one sets a password
  first. Linking an identity, and the new `DELETE /api/v1/users/current/oidc/identities/{id}`
  that removes one, both sign out every other session; `ovumcy link-oidc-identity` now signs
  out every session of the account it links. Removing the last identity is refused unless
  password sign-in remains available on the instance, so nobody can lock themselves out — and
  the check runs inside the removal itself, so two removals sent at once cannot take away both
  of an account's last two identities.

- **An identity token without a subject is refused.** Sign-in and linking key every identity on
  the provider's issuer and subject; a token with a blank subject is now refused when it is read,
  and a blank key is never looked up.

- **The post-logout address is checked as first-party each time it is used.** The address the
  provider sends the browser to after sign-out must share the origin of the configured OIDC
  callback; anything else ends the sign-out locally instead of handing the provider a third-party
  destination.

- **The Docker Hub mirror now carries a signature that verifies there.** `cosign verify
  docker.io/ovumcy/ovumcy-web:…` — the command SECURITY.md and README.md hand an operator who pulls
  from Docker Hub rather than GHCR — had nothing to read. The mirror is made with `cosign copy`,
  which walks the pre-v3 `sha256-<hex>.sig` layout, while the pinned Cosign v3 attaches a Sigstore
  bundle through the OCI referrers API and writes nothing at that tag: the manifest crossed and the
  signature did not. The image on Docker Hub was the digest this repository signed and attested, so
  the failure was an operator unable to confirm a genuine image rather than an unverified one
  standing as verified — but the documents promised a check that could not pass. The mirror is now
  signed on Docker Hub itself, over that same digest and under the same workflow identity.

  The release job also had the mirror in the wrong order — the aliases were written before the
  signing call, so a failure there would leave `latest` on a public registry with nothing to verify
  it by, which is the defect the publish order exists to refuse. The mirror now follows the order
  GHCR already follows: the bytes cross under no public alias, the digest is signed, that signature
  is read back, and only then is an alias written.

- **A mutation-testing shard that silently produced no report can no longer be folded into a
  "complete" combined report.** The per-shard guard that catches a dead shard (OOM, disk
  exhaustion, timeout) only failed that one shard; the merge step downstream ran regardless
  (`if: always()`) and its own upload was `if-no-files-found: ignore`, so a 4-of-5 result still
  published under the same canonical artifact name a complete run uses, with nothing recording the
  gap. `mutationmerge` now takes an `-expect <n>` count — read from the same shard-count registry
  `verify-shards` already checks the partition against — and refuses to merge (fails loudly) when
  fewer shard reports arrive than the registry says are owed.

- **The changelog-fragment gate no longer splices a PR's base-branch name directly into a shell
  command, and no longer goes green having verified nothing.** `git fetch --no-tags origin ${{
  github.event.pull_request.base.ref }}` interpolated an unescaped GitHub Actions expression
  straight into `run:`; it's now a quoted shell variable, matching the pattern this codebase
  already uses elsewhere (`ci.yml`'s `changes` job). The same raw-splice pattern was found, and
  fixed, at the same job's sibling in `ci.yml` (patch-coverage's base-branch fetch) in a companion
  PR. Separately, every real step in the job was gated on `env.RUN_GATE == 'true'` with no
  unconditional fallback — a `RUN_GATE` that ever evaluated to anything else would report success
  having run no check at all. A final step now asserts and fails on that case explicitly.

- **Every `actions/checkout` step across the workflows now sets `persist-credentials: false`.**
  It was present in exactly one of 26 occurrences across 8 files (`scorecard.yml`). None of the
  other 25 push or otherwise need the persisted git token afterward — confirmed no workflow greps
  for `git push`/`git commit`, and the repository is public, so the handful of anonymous
  `git fetch`-after-checkout steps work unauthenticated either way. The sharpest of the 25:
  `security.yml`'s trivy-image job checks out a PR's own ref and then builds and runs a container
  from that PR's own `Dockerfile`; a persisted credential sitting on disk during that step is an
  unnecessary exposure if a future PR changes what the build step actually reads.

- **A step-up hand-off no longer outlives the session that parked it.** A cross-site OIDC step-up
  parks a one-minute cookie sealing the whole step-up — the owner it names, the purpose, and an
  authorization code the provider has not yet redeemed. Signing out dropped the step-up cookie but
  left that hand-off, so within its minute a restored tab or a back-navigation could still finish
  the identity linking, local-password setup, data clearing or account deletion the ended session
  had started. A sign-in start left it behind too, which is the path taken by a session that lapsed
  rather than being signed out. Both now retract it alongside the step-up cookie. Nothing else
  changes: the leg that spends the hand-off still clears it, and a step-up carried through in one
  sitting is untouched.

- **A stray hit on the sign-in return address can no longer cancel a sign-in or a re-authentication
  in progress.** The return address cleared its one-time cookie before checking whether the request
  belonged to that flow at all, so any load of that address — a stale tab, a prefetch, a form
  another site submits — discarded what the owner was in the middle of and left her to start
  again. The cookie is now spent only for a return that answers the flow it was minted for; its own
  one-time nature and expiry are unchanged.

- **Every response Ovumcy reads from the OIDC provider is now size-bounded.** The discovery document
  and the signing-key set (JWKS) were read whole, with no size limit, so a compromised or
  misbehaving identity provider — or anything answering on its address — could make the server
  buffer an arbitrarily large response in memory. The token response was capped at 1 MiB, but by
  cutting it short silently, and a form-encoded token response cut short still reads as a complete
  one. Every OIDC request — discovery, JWKS and code exchange — now shares one limit of 512 KiB per
  response body and 64 KiB of response headers. A response over either limit fails the sign-in with
  an error instead of being truncated or buffered, so it behaves like an unreachable provider: the
  SSO attempt returns to `/login` with the generic error. Real discovery documents and key sets are
  a few KiB to a few tens of KiB, so no working provider is affected.

- **Linking an OIDC identity no longer outlives a revocation that lands while the link is being
  confirmed.** The link revoked the account's sessions by incrementing whatever version it found, so
  a TOTP re-enrollment or password change committed between the factor checks and the link write
  was folded into the link's own bump, and the session issued afterwards carried the version that
  revocation produced. The link now revokes only from the version its factors were verified
  against: if the account's sessions were revoked in between, nothing is linked and no session is
  issued. From Settings the owner signs in again and repeats the link; `ovumcy link-oidc-identity`
  reports the change and asks to be run again.

- The public `/auth/oidc/link-confirm` page can no longer complete an identity link under any
  configuration. Linking an OIDC identity is a permanent, password-change-weight binding, and an
  unauthenticated page cannot verify a factor "now" the way a live session or an operator can.

- **Breaking (API shape): the last two `oidc_only` paths stop issuing recovery codes nothing can
  redeem.** Under `OIDC_LOGIN_MODE=oidc_only`, completing an operator-forced password change after
  an OIDC sign-in still minted a recovery code and showed it on `/recovery-code`, and
  `ovumcy users create` still minted one. The forced change now saves the password and signs the
  user in with no code — `POST /api/v1/password-resets/redeem` answers `{"ok": true}` instead of
  pointing at `/recovery-code` — and `ovumcy users create` prints that no code was issued, even
  with `--show-recovery-code`. Like any reset, the forced change replaces the account's previous
  code, so one kept from an earlier `hybrid` period stops working; after a move back to `hybrid`,
  generate a new one in Settings. `hybrid` instances are unchanged.

- **Breaking (API shape): `oidc_only` no longer issues recovery codes nothing can redeem.** Both
  places a recovery code is used — the password-reset flow and local sign-in — are closed under
  `OIDC_LOGIN_MODE=oidc_only`, yet setting a local password in Settings still minted a code and
  showed it as "store this offline". Under `oidc_only` the step-up now saves the password, keeps
  this device signed in and returns to Settings without minting a code, and
  `POST /api/v1/users/current/recovery-code` answers `403 recovery code unavailable` instead of
  rotating one. `hybrid` instances are unchanged.

- **An OIDC step-up now requires the provider's `auth_time`; `iat` no longer counts as proof of a
  fresh sign-in.** Clearing data, deleting the account, enrolling a local password and linking an
  identity from Settings send the provider `prompt=login` and `max_age=0` and then check that the
  sign-in behind the returned ID token is under five minutes old. When the token carried no
  `auth_time`, that check fell back to `iat` — the time the token was minted, not the time the user
  signed in — so a provider answering from a cached session could pass it with a fresh token over a
  stale sign-in. A token without `auth_time` is now refused, and there is no setting to relax this.
  With a provider that omits `auth_time` under `max_age=0`, an OIDC-only account can no longer
  clear its data, delete itself or enrol a local password, and no account can link another identity
  from Settings; ordinary SSO sign-in is unaffected, and the operator commands
  `ovumcy link-oidc-identity` and `ovumcy users delete` still work. See *Provider requirement for
  step-up* in [docs/oidc.md](docs/oidc.md).

- **A sealed cookie the server refuses is retracted in the same response, by the reader that
  refused it.** The OIDC sign-in state, step-up, step-up continuation and pending-link cookies used
  to stay in the browser after the server had refused to read them — an unopenable envelope, a
  plaintext that is not the payload, a payload past its own expiry — and kept being sent until
  they expired on their own; the pending-link cookie, which carries the target account, the issuer,
  the subject and the asserted email, was cleared by each of its callers instead, so the next
  caller added would have carried the leak back. Each reader now clears the value in the response
  that refused it, and the callers no longer repeat it. A value the server still honours is left in
  place, so a stray or cross-site request to the callback path cannot cancel a sign-in, a step-up
  or a link confirmation in progress. The invariant is now pinned for every sealed cookie the
  transport layer declares, from a set derived from the source rather than a list.

An OIDC sign-in from an identity that is not linked to any account now gets
the same reply in full — status, redirect and message — whether or not a
local account holds the email the provider asserts; the two cases share one
error, and the unused "cannot link a new identity" message is gone from all
six languages. The operator's audit line still tells the two apart, but
now through `cause="link_required"` or `cause="account_unavailable"`: the
link-required line's `reason` is `sso sign-in unavailable`, no longer
`sso link confirmation unavailable`, so a log filter on the old reason
should match the new `cause` instead.

- **The provider's UserInfo address must belong to the configured provider.** Ovumcy now reads the
  `userinfo_endpoint` from the provider's discovery document and holds it to the rule the token,
  key and sign-in page addresses already follow: same scheme, host and port as `OIDC_ISSUER_URL`.
  An address on another host, without https, or one that names the machine Ovumcy runs on is
  dropped and never requested. Sign-in is not refused over it, so a provider that advertises such an
  address keeps signing in as before.

- **A UserInfo answer is used only for the sign-in it belongs to.** Ovumcy uses the email from
  UserInfo only when the answer names the same subject as the ID token, and, when it says which
  issuer sent it and which client it is for, only when those are the configured issuer and Ovumcy.
  Any other answer fails the sign-in, and so does a signed answer whose `exp` has already passed.
  The address and its verification flag always come from the same answer.

- **SSO sign-in no longer reveals whether an email holds an account.** An unlinked SSO sign-in
  whose email matched an existing account was refused with a different message from one whose
  email matched no account, which told anyone able to assert that email at the provider that the
  account existed. Both now show the same message; the account holder still signs in with their
  existing method and links the provider from Settings.

- **`ovumcy users delete` and `ovumcy reset-password` now refuse a calendar-feed revocation they
  cannot record outside the database, instead of completing it anyway.** The restore fence — a
  marker kept in two places, one inside the database and one in a file at
  `CALENDAR_FEED_FENCE_PATH` — advances on every write that arms, rotates or removes a feed, but
  the operator CLI's own writes only ever advanced the database half: an operator's shell rarely
  shares the server's `CALENDAR_FEED_FENCE_PATH`, so the file half stayed behind. A restore of the
  backup taken just before such a revocation then agreed with that unmoved file, and the feed the
  operator believed gone served the calendar again — the exact defect the fence exists to close,
  reintroduced through the one write path that never confirmed it. A second, narrower window let
  the same gap mask a genuine restore: an operator running the CLI with a *working* anchor, after a
  restore had landed but before the server had booted against it, minted a token that
  resynchronized the two halves, so the boot that followed read an ordinary restart instead of the
  restore that had just happened.

  Both commands now confirm the two halves already agree before advancing them, and refuse — the
  account or the password left exactly as it was — when they do not: no path configured, an
  unmounted volume, a file and marker that disagree, or a fence neither half has ever recorded.
  Every refusal but one leaves both halves untouched; the exception is a database write that fails
  right after the file half has already moved, which is named plainly instead of folded into the
  same wording, and points at a server restart rather than a bare rerun. The refusal otherwise
  names `CALENDAR_FEED_FENCE_PATH`, what continuing anyway would have cost, and what to run
  instead. Run either command through `docker compose exec` on the running container, or against
  the same fence path the server uses — the server's own file, never a copy of it — to reach it.
  Full contract: `docs/self-hosted.md → Calendar Feed Restore Fence`.

  The price is deliberate, and it falls on one recovery path. `reset-password` is the documented last
  way back into an account whose second factor died with a rotated or lost `SECRET_KEY`, and on a
  server that has never started with `CALENDAR_FEED_FENCE_PATH` configured it is now refused as well:
  configure the fence for the server, start it once, then run the reset. The bundled compose stacks
  mount the fence already, so `docker compose exec` there needs nothing extra. There is no flag to
  skip the check — a reset that revokes a feed nowhere a restore could contradict is the defect, not
  an inconvenience.

- **The patch-coverage gate now runs in the merge queue, not only on the pull request.** It is a
  required branch-protection check, but it carried `if: github.event_name == 'pull_request'`, so
  the merge-queue suite that actually decides admission reported it `skipped` — and a skipped
  required check counts as satisfied. A queued change could merge having never had its patch
  coverage judged there. The job now also runs on `merge_group`, with its base-ref resolution
  branched the same way the `changes` job already splits it (`pull_request.base.ref` does not
  exist under `merge_group`; the queue's base lives in `merge_group.base_sha` instead).

- **The three Trivy invocations that gate a build no longer hide unfixed CRITICAL/HIGH findings
  from themselves or from the Security tab.** `--ignore-unfixed` was set on the filesystem scan and
  the image scan (both required checks, both writing SARIF that gets uploaded), and on the
  pre-publish re-scan before `:latest`/a release tag goes out. A CRITICAL vulnerability with no
  upstream fix yet — the ordinary case for a fresh CVE — tripped none of them and never reached the
  Security tab either. All three now scan and gate on the full HIGH/CRITICAL set.

- **Turning provider logout off now takes effect at the next sign-out.** When an instance signs an
  owner in over SSO with `OIDC_LOGOUT_MODE=provider` (or `auto`), Ovumcy stores the material that
  sign-out would need — the provider's end-session address and the session's `id_token_hint` — for
  up to seven days. That record was being treated as the decision itself: after the operator
  switched the mode to `local`, or turned OIDC off, an owner who had signed in before the switch was
  still sent to the provider's end-session page for the rest of the record's life, exercising a mode
  the operator had turned off. The mode is now read when the sign-out happens, so such a session
  signs out locally at `/login` from the next request, and the record it would have used is
  discarded with the session rather than left to expire. Nothing was exposed by the old behaviour —
  the end-session address was already re-checked against the issuer configured now, and the return
  address always came from the current configuration — and a `provider` or `auto` instance is
  unaffected.

- **Breaking (API shape): a forced password-reset link minted by local sign-in no longer outlives the operator switching
  local sign-in off.** The earlier fix for this ("A password-reset link no longer outlives local
  sign-in") decided at redeem time from a `forced` flag carried in the sealed reset cookie — not
  forgeable, but not signed into the token either, and every mint path set it the same way regardless
  of *which* factor had actually been checked. A token minted by the plain local sign-in form set the
  same flag as one minted by an SSO sign-in, so once local sign-in was switched off, a reset started
  the minute before still rewrote the password, turned local sign-in back on, and handed out a
  session — on an instance whose operator had just closed local sign-in for that exact reason. The
  redeem decision now reads a purpose signed into the token itself: only a token minted by an SSO
  sign-in survives local sign-in being off; one minted by a local password is refused exactly like an
  ordinary password-recovery link. OIDC link-confirm's own password challenge mints the same
  local-password purpose, since it checks a local password too.
- **A reset link minted before this update stops working within its existing 30-minute lifetime**,
  rather than being reinterpreted under the new rule. No account action is needed; a reset started
  before the update and not finished within 30 minutes is answered "invalid reset token" and the
  stale cookie is cleared, the same way an expired link has always been answered.

- **The SSO client refuses an issuer or endpoint URL that does not name a remote host.** `https://:8443`,
  `https://0.0.0.0:8443` and `https://[::]:8443` all parse as valid absolute URLs that the network
  stack resolves to the machine itself — so a misconfigured issuer, or a discovery document built
  around one, would have posted the client secret and authorization code to whatever listens locally
  on that port. Configuration validation, every origin pin, the discovery `authorization_endpoint`
  the browser is sent to, and the stored provider-logout state now require a named host — the whole
  `0.0.0.0/8` block, a zone identifier and the IPv4-mapped spelling included, as is any numeric host
  Go does not parse as a canonical address (`0`, `0.1`, `00.0.0.0`, `0x0`, `192.168.001.010`), which
  a platform resolver may still read as one. A self-hosted issuer on `127.0.0.1` or a LAN address
  written in plain dotted-quad form stays supported.
- **Upgrade note:** an instance whose `OIDC_ISSUER_URL`, `OIDC_REDIRECT_URL` or
  `OIDC_POST_LOGOUT_REDIRECT_URL` names such a host — `https://0.0.0.0:8443` included — now fails
  configuration validation at startup (`… must name a remote host`) instead of starting. Replace
  the host with the real hostname or address the provider and browsers reach.
- **A provider discovery document without an `authorization_endpoint` now leaves SSO unavailable.**
  An empty authorize URL used to compose a relative redirect that sent the browser back into Ovumcy
  carrying the sign-in `state` and `nonce` instead of failing the sign-in.
- **A discovery document that only partly decodes no longer keeps an unpinned logout endpoint.**
  JSON decoding keeps a field it has already read when a later occurrence of the same key fails, so
  a document naming `end_session_endpoint` twice — once as an off-origin URL, once as a number —
  used to leave that URL in place while the error skipped the sanitizer. The sanitizer now runs on
  whatever decoded, and a logout endpoint it rejects degrades to local sign-out as an absent one does.

- **Flooding the attempt limiter no longer lifts a lockout or erases another budget.** The
  in-memory `AttemptLimiter` behind every per-account budget (login, recovery, TOTP, settings
  re-authentication) bounds its own memory: a periodic sweep drops entries whose window has lapsed,
  and above 1024 entries the coldest are evicted. Both bounds judged every entry by the budget of
  whichever failure triggered them. A login failure's sweep therefore erased a recovery lockout
  forty minutes before its own hour lapsed, and 1025 fresh login identities — the login form
  accepts any email — evicted the coldest entries first, which is exactly a locked-out victim,
  since the lockout is what stopped the attacker from touching that key; the same flood reached
  a partially spent TOTP budget. Every entry now carries the scope, limit and window it was
  recorded under: the sweep uses each entry's own window, the cap is enforced per scope, and an
  entry at its limit — an active lockout — is pinned until it expires. Regressions:
  `TestAttemptLimiterChurnDoesNotLiftActiveLockout`, `TestAttemptLimiterChurnDoesNotEraseTOTPBudget`,
  `TestAttemptLimiterSweepRespectsEachEntryWindow`.
- **A spent per-account logout budget no longer keeps a session alive.** `DELETE /api/v1/sessions/current`
  checked the per-account logout budget before revoking the session, so once the budget was spent
  the owner could not sign out at all — on a shared device the session stayed usable until the
  window lapsed. The revoke and the cookie retraction now come first and the budget check second:
  a refused sign-out still ends the session and answers `429` (a browser form is sent to the
  sign-in page with the refusal as a flash) with the cookies already gone; what it withholds is
  the provider sign-out bridge. The per-IP `RATE_LIMIT_LOGOUT_*` row on `DELETE /api/v1/sessions/current` is
  edge middleware and still refuses before the handler runs, so whoever shares the address and
  spends that row holds an API client's sign-out until its window lapses; the browser form
  (`POST /logout`) is not behind that row. The budget stays keyed on the **owner** and deliberately does not
  name the session: revoking bumps the account's session version, so no session reaches the route
  twice and a session-keyed budget would record one attempt per key and never trip, leaving the
  browser sign-out route with no account-side cap. What the session used to carry is instead fixed
  in the client bucket, now `(address, account)` rather than the address by itself, which had run a
  second per-address cap of 20 under the documented per-IP row of 60 and refused a household's — or
  a test run's — 21st sign-out from one address. A logout is recorded
  against this budget only — never as a failure against, nor a reset of, the login, recovery or
  TOTP budgets. Regressions:
  `TestLogoutRevokesTheSessionBeforeTheBudgetIsChecked`,
  `TestLogoutBudgetIsKeyedByTheOwnerNotTheSession`,
  `TestLogoutBudgetIsSpendableAcrossSuccessiveSessions`,
  `TestLogoutAccountingNeverTouchesAnotherBudget`.
- **Every `RATE_LIMIT_*` setting has a ceiling.** A `*_MAX` accepted any positive integer and a
  `*_WINDOW` any duration of a second or more, so `RATE_LIMIT_LOGIN_MAX=100000000` switched the
  sign-in limiter off and a window of a year kept a refused address refused until restart. Each
  `*_MAX` now has a ceiling — 100 for login, registration and password reset (each request costs
  a bcrypt compare), 600 for the per-IP logout row, 200 for the per-account logout budget, 3000
  for the API catch-all and 120 for the calendar feed — and each `*_WINDOW` must lie between one
  second and one day. A value outside its range is logged at boot and the default is used, as an
  unparseable value already was. Operators who had set a `*_MAX` above its ceiling get the default
  from this release; on the three credential endpoints a shorter window does not widen the budget
  either (see the 30-requests-per-minute rate ceiling entry). The bcrypt cost stays the load-bearing limit and is unchanged.
  Regressions: `TestRateLimitMaxSettingsHaveCeilings`, `TestRateLimitWindowSettingsHaveCeilings`.

- **The Scorecard workflow no longer grants `read-all` at workflow scope.** The scope only reaches
  a job that does not inherit from it: `analysis` already declares its own full permission set
  (`security-events: write`, `id-token: write`, `contents: read`, `actions: read`), and a job-level
  `permissions:` block replaces the workflow-level one rather than adding to it. Workflow scope is
  now `{}`.

- **A refused sign-in or recovery reset now costs the same whether or not the address has an
  account — including accounts created before the password hashing cost was raised.** The
  equalizers that make an unknown address spend the same bcrypt work as a wrong password were
  pinned to the current cost, while the real comparison spends only the cost stored in the row. An
  account still holding a hash from before the raise was therefore refused about four times faster
  than an address with no account at all, and that gap told anyone measuring it which addresses are
  registered. A refusal now buys the missing work at the missing cost steps, derived from the
  configured cost and the stored one rather than from a fixed pair, so the totals match again.
  Recovery codes are the case that could not heal itself: their hashes are never re-minted on use,
  so a cost written years ago stays where it was.
- No stored hash is rewritten by this change and nothing about signing in changes for an owner: a
  successful sign-in still upgrades a stale password hash in place, as it has since the cost was
  raised.

- **The calendar-feed reveal now expires on the server, not just in the browser.** The sealed
  `ovumcy_calendar_feed` cookie, which carries the full `.ics` subscribe URL for its shown-once
  reveal, carried no expiry of its own, so its twenty-minute lifetime lived only in the
  `Set-Cookie` `Expires` attribute — a hint the client is free to ignore. A browser that kept the
  sealed value and never spent the reveal could present it back on the owner's own session and be
  shown the subscribe URL — a bearer capability token — months later, until the token was rotated,
  the feed revoked, or `SECRET_KEY` changed. The payload now carries the moment it is honored
  until, and the reveal page verifies that against the clock before it displays anything.

  This is the bound on a reveal that never happened; the server-side consumption mark
  (`users.calendar_feed_revealed_at`) remains what refuses a reveal that did.

  A cookie minted before this change carries no expiry and is **refused**: an absent bound is
  invalid input, not permission to display. So is a reveal minted in the twenty minutes before the
  upgrade. The refusal costs the display only — the feed itself keeps working, the owner keeps
  their session and lands on `/settings`, where an absent reveal cookie lands, and a rotate mints a
  fresh URL and re-arms the reveal in the same write. A cookie refused for either reason is
  retracted in the same response, and a visit that revealed nothing logs no reveal.

- **Password recovery now requires your password as well as your recovery code.** A recovery code
  on its own used to be enough to reset the password on `/forgot-password`, receive a signed-in
  session, and from there switch two-factor authentication off — so a single captured secret was a
  full account takeover, on every release that has ever shipped TOTP. The recovery code is now what
  it was always meant to be: a stand-in for the **second** factor, never for the password. The
  recovery form asks for both, and a submission with a wrong or empty password is refused exactly
  like a wrong recovery code — same answer, same rate-limit cost, no account existence revealed.
  (A programmatic caller that omits the field entirely is answered differently, by name; see the
  API entry under Changed.)
- **The cost, stated plainly:** if you have forgotten your password and hold only a recovery code,
  you can no longer recover the account yourself. Your fallback is the operator CLI,
  `ovumcy reset-password <email>`, which needs shell or container access to the instance. The path
  this route was written for still works unchanged: an owner whose TOTP stopped working after a
  `SECRET_KEY` rotation knows their password and only lost the second factor, so password +
  recovery code gets them back in and lets them re-enrol TOTP.

- **The recovery-code reveal now expires on the server, not just in the browser.** The sealed
  `ovumcy_recovery_code` cookie carried no expiry of its own, so its twenty-minute lifetime lived
  only in the `Set-Cookie` `Expires` attribute — a hint the client is free to ignore. A browser
  that kept the sealed value could present it back on the owner's own session and be shown the
  recovery code again, indefinitely, until the code was regenerated or `SECRET_KEY` changed. The
  payload now carries the expiry it is honored until, and the reveal page verifies that against
  the clock before it displays anything.

  A cookie minted before this change carries no expiry and is **refused**: an absent bound is
  invalid input, not permission to display. The refusal costs the display only — the page already
  required an authenticated session, so the owner keeps their sign-in, lands on the same continue
  path they reach when the cookie is absent, and can regenerate the code from Settings. A cookie
  refused for either reason is retracted in the same response.

- **A shown-once secret is now consumed on the server, so replaying the reveal cookie shows
  nothing.** Both surfaces that reveal a secret exactly once — the recovery code (its page and the
  inline block after sign-up) and the calendar-feed subscribe URL — enforced that by writing a
  cleared cookie into the reveal response and nothing else. Clearing a cookie asks a browser to
  forget a value it was handed, and binds nothing that kept it: a client holding the sealed value
  could present it again on the owner's own session and be shown the secret a second time. For the
  feed URL there was no bound at all — the window closed only when the token was rotated, the feed
  revoked, or `SECRET_KEY` changed.

  Each reveal now claims a per-account mark on the `users` row (`recovery_code_revealed_at`,
  `calendar_feed_revealed_at`, migration 036) with a compare-and-set before it renders anything. A
  replay, a reload and a second tab all lose that race and are refused; the refusal retracts the
  cookie in the same response, costs the display only, and lands the owner where an absent cookie
  lands. A refused feed reveal also records no second reveal in the audit stream, so an operator
  counting reveals counts disclosures.

  Re-issuing the secret re-arms exactly one reveal: every write that mints a fresh recovery code or
  a fresh feed token clears the matching mark in the same statement, so generate, rotate,
  regenerate and reset all keep working as before. Because the mark is a database row rather than
  process state, it survives a restart, a second replica and a backup restore.

  **Operators:** migration 036 adds two nullable timestamp columns to `users` on both SQLite and
  PostgreSQL. Nothing already revealed is retroactively refused, and no data is lost in either
  direction — reverting the code leaves the columns in place, unread.

- **The webhook private-address block now recognises every IPv6 form that hides an IPv4
  destination inside it** (GHSA-hg2x-v5cc-m384). With `WEBHOOK_BLOCK_PRIVATE_ADDRESSES=true`
  the guard decoded only RFC 6052 NAT64 (`64:ff9b::/96`), so the same internal target written
  as 6to4 (`2002:7f00:1::` is `127.0.0.1`), IPv4-compatible (`::10.0.0.1`), Teredo or SIIT
  IPv4-translated passed the check. All five forms are now decoded and classified by the IPv4
  they wrap — a wrapped **public** address still routes to the public internet and stays
  allowed, unchanged. Four ranges the guard never covered are refused too: RFC 1122
  `0.0.0.0/8` beyond the unspecified address alone, deprecated IPv6 site-local `fec0::/10`
  (outside Go's `fc00::/7`), and the RFC 8215 local-use NAT64 block `64:ff9b:1::/48`.
  Terminal classification runs before decoding, so `::1` and `::` — which sit inside the
  IPv4-compatible prefix — are never re-read as public addresses.

  The gate remains **off by default**: a self-hosted instance delivering to ntfy or Gotify on
  the same LAN is unaffected, and no configuration changes. Reported by tonghuaroot.

- **Breaking (API shape): settings actions that ask for the current password no longer say outright that an account has
  no local password set.** Clear-data, delete account, enroll two-factor sign-in, change password,
  regenerate the recovery code, and link or unlink an OIDC identity used to answer "local password
  required" (403) for that account state but "invalid password" (401) for a wrong password on an
  account that has one — two differently-shaped refusals a live, unauthenticated-for-this-purpose
  session could use to learn whether an account has a local password at all. All of them now answer
  identically: 401, the same error key, the same form target. The account-state distinction is
  still written to the security log for the owner's own audit trail, via a new `reauth_cause`
  field (server log only, never the response) so an operator can still tell "wrong password"
  apart from "no local password" now that the caller-visible refusal is byte-identical. Removing
  an account's last sign-in method and the OIDC-only password-change redirect keep their own,
  unrelated 403 answers, since neither compares the submitted password against the stored one.
  Disabling two-factor sign-in is unaffected by this change: it was already merging both cases
  into one generic "invalid credentials" answer at the service layer, and logs no account-state
  distinction either way.

- **A published image tag now exists only once the image under it is signed.** Every release image
  is Cosign-signed and carries a SLSA build-provenance attestation and an SBOM, and the verification
  commands in the README and the security policy are the way to check it. The publish job produced
  that state in the wrong order: it pushed the image under its public tags — `vX.Y.Z`, `latest`,
  `main`, `sha-…` — and installed Cosign, signed and attested afterwards, with a check that the
  image pulls anonymously in between. Anything failing after the push left the workflow red and the
  release already resolvable by anyone, unsigned and without provenance. A red workflow retracts
  nothing; nothing deletes a tag that already answers.

  The image is now pushed by digest and given no tag at all. It is signed and attested at that
  digest, both are read back from the registry, and only then are the public tags written — from the
  manifest bytes that already hash to the signed digest, so a tag cannot come to point at anything
  else. Every tag is then checked to be anonymously pullable and to resolve to that same digest.
  What changes is that no failure can produce a tag pointing at an unsigned image: one before the
  tags are written leaves an untagged manifest and no public alias at all, and one during the write
  can leave the aliases written before it — each of them resolving to the digest that was signed.

  Verifying a release is the same procedure it was, against the same tags, and both documented
  commands were tightened: the signer-identity pattern now escapes the dot in `github.com` as it
  already escaped the two after it, and the security policy checks the build provenance against this
  repository rather than against every repository in the account. An operator holding a copy of the
  older commands is running a looser check than this release documents.

- **Upgrading signs everyone out.** Session tokens and password-reset tokens are now signed under
  separate keys derived from `SECRET_KEY`, and each carries its own audience and type, so a token
  minted for one purpose can never be accepted as the other. Every token minted before the upgrade
  stops verifying: **all sessions end, open password-reset links stop working, and a sign-in
  waiting at the two-factor step has to start again.** Nothing needs re-configuring; owners sign
  in again. Rolling back to the previous release makes the old tokens valid again.

- **A reset link or a pending two-factor step no longer outlives the credential behind it.** A
  password-reset link, and the step between the password and the two-factor code, now die when
  the account's password, recovery code, two-factor setting or sessions change after they were
  issued — including a change that lands while the reset is being submitted. An account an
  operator has flagged for a forced password change can no longer finish a sign-in through a
  two-factor step started before the flag.

- **Two submits of the same reset link now get a clear answer.** The one that loses gets the
  same "invalid reset link" answer as a replay, and its reset cookie is cleared.

- **Messages carried across a redirect expire on the server.** A kept message cookie is ignored
  once its few minutes are up, and signing out clears it.

- **A successful sign-in forgives only its own client.** A sign-in, recovery-code sign-in or
  two-factor step that succeeds clears the failure count of the browser that succeeded, and no
  longer the count other clients built up against the same account; that count ages out on its
  own. Checks that already need a signed-in session — the password re-check in settings, the
  password change and turning two-factor off — still clear the account's count on success, so an
  owner's own typos never add up to a lockout. A sign-in with an address that normalizes to
  nothing is refused before any lookup.

- **A settings or onboarding page left open can no longer write to the account that signed in
  after it.** The browser keeps one session, and a page rendered for one account keeps working after
  another account signs in elsewhere in the same browser, so a stale tab could save its form onto
  the second account. The cycle settings and the restore were bound to their rendering account
  earlier; the rest of the class now is too: the profile, interface, tracking, reminder, timezone,
  webhook and calendar-feed writes, the custom-symptom writes, and every onboarding step. A write
  from a page that names another account, or from a page that names none, is refused with 409 and
  changes nothing ("You're now signed in to a different account. This change was not saved. Reload
  the page."). The browser's own timezone sync sends the binding too. API clients that render no page
  and send no binding keep their behaviour; a binding they do send is still checked.
- **The language switcher no longer stores its choice on the wrong account.** The switcher is shown
  on the onboarding pages and on signed-out pages, and both outlive a sign-in elsewhere in the same
  browser, so a stale tab could save its language onto whichever account was signed in by then. The
  choice is now stored on the account only when the form carries the binding of the account now
  signed in; any other switch changes the language cookie alone, and the answer is the same either
  way.

- **A timezone header no longer costs a zoneinfo load on every request.** The zone named by the
  `X-Ovumcy-Timezone` header or the `ovumcy_tz` cookie is resolved on every request, including
  routes with no rate limit, and each resolution used to read and parse a zoneinfo file. Loaded
  zones and names the tz database does not know are now remembered in a bounded in-process cache,
  so a repeated value costs a map lookup; which names are accepted and the fallback are unchanged.

- **Turning off two-factor sign-in now checks the password of the signed-in account itself.** The
  password confirming the disable was checked by looking the account up again by its email, not
  against the account the session belongs to. On a database whose email index is missing and where
  two accounts share an address, that lookup refused every attempt, so the owner could never turn
  2FA off and each try counted against the disable attempt limit. The password is now checked
  against the signed-in account's own stored password. The attempt limit, the "invalid
  credentials" answer and its timing are unchanged. As on sign-in and the other Settings
  confirmations, spaces around the password are ignored, and a disable no longer upgrades an older
  stored password hash (sign-in still does). The failed-attempt count is cleared only once the
  disable has gone through, not when the password alone was right.

- **An account whose TOTP secret can no longer be decrypted (a `SECRET_KEY` rotation) now reaches the
  operator-reset flow automatically instead of being stranded at a 2FA challenge no code could ever
  pass.** Every session-issuing path — local login, OIDC sign-in, and OIDC account-link confirmation —
  now derives TOTP verifiability by attempting the decryption instead of trusting the `totp_enabled`
  column alone, and routes an enrolled-but-unverifiable account to the same forced-reset escape hatch
  an operator-flagged account already used, without waiting for that operator action.

- **A tag named `origin/main` can no longer stand in for `main` at the release gate.**
  `verify-release-tag` refuses a release tag whose commit `main` does not contain, and it read
  `main` by the short name `origin/main`. Git resolves `refs/tags/<name>` before
  `refs/remotes/<name>`, and the gate's full-history checkout fetches every tag, so pushing a tag
  literally named `origin/main` onto an off-main commit made that commit its own "main" and let
  the ancestry check pass. The gate now reads `refs/remotes/origin/main` by its full name in both
  the lookup and the ancestry test. The release-gate suite runs the step against a fixture origin
  carrying such a tag and requires the refusal to name `main`'s real tip.

- **An email address two accounts share is refused on every web sign-in path instead of being
  resolved to one of them.** A database whose unique index on the normalized email was dropped or
  restored away outside the app can hold two accounts on one mailbox (the migration itself refuses
  to build the index over duplicates). Password sign-in, recovery reset and SSO sign-in resolved
  such an address to whichever row the query returned first, so the account a sign-in reached
  depended on storage order, not on who was signing in. Such an address is now refused: sign-in and
  recovery answer exactly as for an unregistered address, with the same response and the same
  password-hashing work, and SSO answers as it does for an existing account the identity is not
  linked to, linking and creating nothing. Operator commands already refused it and name the
  matching account ids, so the duplicate can be resolved by id.

- **A burst of wrong passwords or codes can no longer outrun an account's attempt limit.** The
  per-account budgets (sign-in, recovery, the 2FA challenge, settings re-authentication and the 2FA
  enrollment code) read the count, ran the password or code check, and only then booked the
  failure. Requests that arrived while a check was running — a full bcrypt for sign-in — all read the
  same count, so a burst from several addresses could try far more guesses than the limit. The
  attempt is now reserved in the same step that checks the limit, before the check runs, and a
  correct password or code gives its slot back. Brute-force protection is tighter under load: the
  limit now bounds the guesses that run, so a real sign-in that arrives in the middle of a burst
  against the same account can be refused until the burst's attempts age out. A storage error
  during recovery-code redemption, or while the 2FA challenge reads the account, now keeps its
  attempt, as it already did at sign-in; the challenge answers such a fault as an internal error
  and leaves the pending sign-in in place. A 2FA code
  that is not six digits is refused before the attempt budget is consulted, so it neither draws an
  attempt nor, once the budget is spent, clears the pending sign-in.

- **Turning two-factor sign-in off now shares the settings re-authentication budget.** The
  password confirmation of `DELETE /api/v1/users/current/2fa` used to draw a budget of its own
  (5 failures / 15 minutes) beside the one every other password-gated settings action draws, so a
  signed-in session could make twice as many password guesses against one account by spreading
  them over both. Both now draw one per-account budget of 5 failures / 15 minutes: wrong passwords
  on any of these endpoints refuse the others with `429`, the correct password included, until the
  window passes. The sign-in budget is separate and unchanged, and the response each
  endpoint gives is unchanged.

- **The vulnerability scan that gates a published image now judges every platform of the digest
  that gets signed.** It used to scan a separate single-platform rebuild: `linux/amd64` only, from
  bytes that shared a build cache with the pushed image but were never shown to be it, so the
  `linux/arm64` image was published without a scan of its own. The publish job now pushes the image
  under no tag, scans each platform's manifest out of that exact digest with the same scanner,
  threshold and exit code as the required image-scan check, and only then signs, attests and tags
  it. A finding on any platform leaves the digest unsigned and with no public tag. The platform list
  is declared once and read by both the build and the scan, so a platform added to one cannot miss
  the other.

- **A rotated or revoked calendar link no longer keeps answering from a browser's cache.** The
  `.ics` feed was served cacheable for an hour, so a browser that had opened the old link could
  show the old feed for up to an hour after the link was replaced or turned off. The feed is now
  never stored.

- **A calendar feed event's identifier no longer names the cycle phase.** Each `.ics` event's
  `UID` read `period-…`, `ovulation-…` or `…-window-…` followed by the date, which calendar apps
  show in event details and exports even though the title stays neutral. The `UID` is now an
  opaque value keyed per owner from `SECRET_KEY`, still stable from one poll to the next. After
  upgrading, a subscribed calendar app replaces every Ovumcy event once: the old events disappear
  and new ones appear, and some apps briefly show both until their next refresh. Alarms or notes
  added to an Ovumcy event in the calendar app do not carry over. Rotating `SECRET_KEY` renames
  the events the same way, and it already requires re-subscribing to the feed.

- **The settings egress ledger now tells two webhook endpoints on one host apart.** The destination
  line shows `host[:port]` plus a masked path: the first path segment in clear, then `/…/`, then
  the last four characters (for example `hooks.slack.com/services/…/abcd`). A first segment that
  looks like a credential — it contains `:`, or is 20 or more letters, digits, `_` or `-` — is
  masked too, so a Telegram bot URL shows `api.telegram.org/…/sage` and never the bot token. A
  single-segment path is never shown whole. The query, fragment and userinfo are never shown.

- **A page left open for one account can no longer restore a file into, or change the cycle
  settings of, another account signed in later in the same browser.** The session cookie is
  browser-wide and the CSRF token is not bound to an account, so the settings restore (`POST
  /api/v1/imports/json`) and the cycle settings save (`PATCH /api/v1/users/current/cycle`, also the
  dashboard's usage-goal switch) wrote into whichever account was signed in at that moment. Both
  now carry the binding of the account that rendered the page, as the day forms already do, and
  a request whose binding names another account, or a request from a page that sends none, is
  refused with 409 and writes nothing. A JSON API client that sends no binding keeps its
  behaviour.

- **Revoking a calendar feed now also clears its detailed-titles choice.** Revoke, clear-data, a
  recovery-code regeneration, a password reset and the boot-time disarms removed the subscribe
  link but left the "name the estimate type" opt-in on the account. A release older than v2.0.0
  mints a link without touching that setting, so after a downgrade, a new link there and an
  upgrade back, the new link sent "Period (estimate)" and "Ovulation (estimate)" titles the owner
  never chose for it. Every disarm now resets the choice to neutral titles. A link an older
  release rotates in place stops working after the upgrade back instead, and the owner generates
  a new one; `docs/self-hosted.md → Downgrade Caveats` describes both cases.

- **A calendar subscribe link spelled with an upper-case `.ICS` is masked in the logs too.** The
  log redaction that hides the feed token in a request path matched only a lower-case `.ics`
  suffix, while the feed itself and its rate limit accept any case, so a path that fell back to
  the raw request URL could carry the token verbatim. The mask now ignores the suffix's case.

- **Account deletion no longer outruns a sign-out everywhere.** Deleting the account from Settings
  now runs only from the session version its re-authentication was checked against, as clearing
  data already did: a password change or a sign-out everywhere landing in between leaves the
  account in place and signs the device out. The operator's `ovumcy users delete` is unchanged.
- **Behind a proxy that forwards `X-Forwarded-For`, a stranger can no longer lock a victim's
  address out of sign-in.** The per-client sign-in, 2FA, recovery and re-authentication budgets
  now key on the same rightmost untrusted hop the edge rate limiters use, not on the leftmost
  entry the client writes itself.
- **A link on another site can no longer cancel a step-up in progress.** Opening the SSO sign-in
  start from another site leaves an in-flight step-up to its own ten-minute expiry; only a sign-in
  started from Ovumcy itself drops an abandoned one.

- **A crash during a password reset, recovery-code regeneration, clear-data or account deletion
  can no longer leave a calendar feed restorable.** These operations remove the owner's
  calendar-feed link and record that removal in the restore fence, but they recorded it only
  after their own write had committed. A crash in between left the fence agreeing with a backup
  taken before the removal, so restoring that backup brought the removed subscribe link back.
  They now record the removal first, as revoking the feed itself already did. A fence that cannot
  be written still never blocks the operation.

- **Restoring a backup no longer brings back sessions that were signed out, or webhook endpoints
  that were removed — and the restore fence is now required.** A sign-out everywhere, a password
  change or reset, an operator reset, an account deletion and a webhook removal are all recorded
  in columns a database restore rolls back, so restoring a backup taken before one of them made
  every sign-in cookie minted before the backup point valid again and sent reminders to the
  removed endpoint. The first start after a restore already disarmed calendar feeds; it now also
  signs every owner out and clears every stored webhook endpoint. Owners sign in again and
  re-enter their webhook URL in Settings; their reminder choices are kept. A lost or recreated
  fence volume reads as a restore and costs the same.

  **Upgrading: a v2.0 deploy without the fence mount stops at boot.** Without a fence a restore
  cannot be seen at all, so the server refuses to start with an error naming
  `CALENDAR_FEED_FENCE_PATH` and `docs/self-hosted.md → Calendar Feed Restore Fence`, and writes
  nothing. Take the `ovumcy_fence` volume from the updated compose file for your stack. To start
  without it anyway, set `CALENDAR_FEED_FENCE_OPTOUT=true`: feeds are disarmed on every start and
  the log warns, but a restore on that instance can revive sessions and webhook endpoints.

- **A password-reset hand-off no longer waits half an hour in a shared browser.** The cookie that
  carries a reset in progress — a forced reset after sign-in, or one started with a recovery code —
  was kept for 30 minutes, and on its own it was enough to set a new password and get signed in.
  Whoever used the same browser next could finish it. The cookie now lasts only for the browser
  session, and the reset token inside it is valid for 10 minutes instead of 30.

- **A refused session takes its leftovers with it.** When a session is refused — expired, revoked or
  invalid — the browser is now also told to drop the one-time recovery-code page, a pending
  two-factor enrollment and the one-time calendar-feed link that only that session could use. A
  reset or two-factor sign-in already under way is left alone.

- **A session token without a session version is refused.** It used to be read as version 1. No such
  token can be made without the server's key; the parser now simply refuses it, as the other
  sign-in grants already did.

- **The erasure re-authentication buttons check which account the page was opened for.** Starting
  the single-sign-on confirmation for clearing data or deleting the account from a settings page
  opened under another account in the same browser is refused, like every other settings write.

- **Linking a sign-in identity from Settings now asks you to confirm it before anything is
  linked.** After the identity provider re-authentication, the app shows a confirmation page
  naming the provider and the returned identity (its email when the provider marks it verified,
  otherwise its account identifier), and links it only when you press the confirm button. Before, the
  provider callback linked whatever identity it received on arrival; because that callback has
  to accept posts from the provider's site, someone who obtained the authorize link of your
  step-up could sign in as their own provider account and post the result into your browser,
  attaching their sign-in to your account. The confirm step is a same-origin, CSRF-protected
  form bound to your account, so that chain stops at the page. No email match is required: an
  account whose provider email differs from its own still links the same way.

- **The `reset_required` audit line now names its cause** (audit stream on with
  `AUDIT_LOG_ENABLED=true`; it is off by default). `auth.login` and
  `auth.oidc_callback` record `cause=must_change_password` for an account an operator flagged
  with `ovumcy reset-password`, and `cause=totp_unverifiable` for one whose stored TOTP secret
  no longer decrypts. The second is what a `SECRET_KEY` rotation leaves behind: every
  TOTP-enrolled account then signs in with its password alone and is sent to a forced password
  change instead of a 2FA challenge no code can answer. That trade-off is deliberate — refusing
  those sign-ins would lock every such owner out after a rotation — but until now nothing told
  the operator it was happening. After a rotation, expect `totp_unverifiable` lines and have the
  affected owners re-enrol TOTP.

- **Restoring a backup is held to the same date bound as saving a day.** A JSON restore could
  write a period or a pregnancy-test result for any future date, which then fed the predictions.
  A file whose new days record either more than two days ahead is now refused whole, and nothing
  of it is restored. Days already present are skipped as before.
- **Clear data on Postgres no longer keeps a day saved while it runs.** A day written by a request
  still in flight when the wipe started could survive it. The wipe now waits for such a write and
  erases what it left.
- **Erased data no longer lingers in the SQLite file.** Clear data and account deletion now
  overwrite the erased rows with zeros (`secure_delete`), so the deleted health records cannot be
  read back from the database file's free space.

- **`ovumcy reset-password --full` recovers a compromised account completely.** The operator
  reset replaced the password and signed every device out, but left two-factor sign-in, the
  recovery code and linked OIDC identities in place, so an identity or authenticator an intruder
  had added still worked after the reset. `--full` clears all three in the same write that resets
  the password and revokes the sessions. Without the flag the command behaves as before.

- **Changing `SECRET_KEY` and then changing it back no longer brings an old calendar link back to
  life.** A key change refused every armed calendar link but left its row in place, so returning to
  the earlier key made a leaked subscribe URL answer again. The first start under a changed key now
  clears every armed link, whatever its age, and each owner generates a fresh one from Settings.
- **Generating or rotating a calendar link can no longer end with the old link dead and the new
  one never shown.** The one-time reveal is now prepared before the new link is saved, so a failure
  there leaves the previous link working. The reveal is also tied to the link it was prepared for:
  a reveal left over from a link that has since been replaced shows nothing and no longer uses up
  the replacement's one view.

- **A restored backup no longer brings back a password an operator reset replaced, and
  `ovumcy notify` no longer delivers from a restored database before the server has seen it.**
  A restore already signed every owner out and cleared every webhook endpoint on the next start,
  but the password hash came back as the backup held it: an account that was compromised and then
  force-reset accepted the compromised password again. Setting a forced password change would not
  have helped — the forced-change route accepts that same password, and it ranks above two-factor
  sign-in. So the start that detects a restore now revokes every local password in the same step.
  An owner with a linked OIDC identity signs in through it; for everyone else, run
  `ovumcy reset-password <email>` and hand the owner the new password, which they are made to
  replace at their first sign-in. The startup line names how many passwords were revoked.

  `ovumcy notify` now checks the restore fence, read-only, before it reads any endpoint, and
  refuses — sending nothing — until the server has started once over a restored database, so a
  cron job or sidecar cannot post to an endpoint an owner removed after the backup point. Run it
  where the server's fence file is visible, as `reset-password` already requires.
  `CALENDAR_FEED_FENCE_OPTOUT=true` lets it run without a fence, never over a disagreeing one.
  Removing a webhook endpoint or saving webhook settings now advances the fence too, so a restore
  across nothing but a webhook change is detected.

  A fence that can be read but not written no longer repeats the restore revocation on every
  start: the start ends unanchored, disarms the calendar feeds as before, and revokes nothing
  else. Full contract: `docs/self-hosted.md → Calendar Feed Restore Fence`.

- **A momentary failure to write the calendar-feed restore fence no longer locks every owner out.**
  When the fence file could not be written for a moment (a full disk, an I/O error), an owner's
  calendar-feed or webhook change still went through and only the database half of the fence moved.
  The next start could not tell that from a restored backup and revoked every session, webhook and
  local password on the instance, leaving the operator to run `ovumcy reset-password` for every
  account. Those changes are now refused instead, with the fence untouched, and succeed on retry.
  A password change, recovery-code rotation, clear-data or account deletion is still never refused
  over the fence; if one meets the same failure, the next start still runs the full revocation,
  because a restore across it could not be told apart otherwise.

- **`ovumcy webhook set` now confirms the restore fence before it writes.** Run from a shell that
  could not see the server's fence (`CALENDAR_FEED_FENCE_PATH` unset or a different file), it used
  to withdraw an endpoint in the database alone, and a restore of an earlier backup let
  `ovumcy notify` deliver to it again. It now refuses with "Nothing was changed" unless it can
  advance the server's own fence first, as `ovumcy users delete` and `ovumcy reset-password` already
  do. `--dry-run` still works anywhere.

- **A restore across nothing but a sign-out, password change, TOTP change or OIDC link is now
  seen.** The first start after a restore signs every owner out and revokes local passwords, but
  it could only tell a restore by the fence, and these writes did not move it: restoring a backup
  taken just before one of them brought back the signed-out cookie, the old password, the removed
  or missing second factor, or the unlinked identity. Every write that signs an account out or
  changes how it signs in now records itself in the fence first, as feed and webhook changes
  already did. `ovumcy users set-email` now needs `CALENDAR_FEED_FENCE_PATH`, like `users delete`
  and a forced `reset-password`, and refuses without it.

- **A start refused for a fence it cannot write no longer costs every owner their password on the
  next start.** Without `CALENDAR_FEED_FENCE_OPTOUT`, a start whose calendar-feed restore fence can
  be read but not written — a bind mount the container user cannot write to, a full disk — was
  refused correctly, but only after it had already marked the database as having run without a
  fence. Once the operator fixed the mount, the next start read that mark as a possible restore and
  signed every account out, removed every webhook endpoint and revoked every local password. The
  refused start now writes nothing, so the start after the fix is an ordinary first start.

- **`ovumcy reset-password --full` now also removes the account's webhook endpoint.** Someone who
  took over an account could point its reminders at an endpoint of their own, and the reset that
  recovers the account left that endpoint in place, so `ovumcy notify` kept posting period and
  ovulation reminders to it. `--full` now withdraws the endpoint in the same write that resets the
  password: the URL is cleared, delivery is switched off, and a reminder pass already running stops
  before its next request. The owner adds the webhook again from Settings. The plain
  `reset-password` keeps it, as before.

- **Cancel on the "link this sign-in?" page now actually declines the identity.** The button was a
  plain link back to Settings, so the identity just declined stayed parked for the rest of its
  five-minute window: opening the page again offered the same confirm button, one mis-click away
  from linking a sign-in the owner had rejected. Cancel now clears the pending link, and the page
  has nothing left to confirm. A plain visit to Settings from another tab still leaves a pending
  link alone.

- **Confirming a link from before a password change no longer signs this device out.** A link
  parked before the password was changed in another tab is stale, but confirming it was answered
  as if the sessions had been revoked under the link: this device was signed out and a revocation
  was logged for a link that was never written. It now answers like any other expired link —
  nothing is linked, the device stays signed in, and the owner is asked to start the link again.

- **Breaking (API shape): linking an OIDC identity whose holder sign-in would not reach now gets
  the account-unavailable reply.** `POST /api/v1/users/current/oidc/link/confirm` (and the link
  step-up's `/settings` flash) used to answer every cross-user claim with `409` "That identity is
  already linked to another account." Two holder cases now get `403` "sso sign-in unavailable"
  instead: a legacy row whose role is not `owner`, and an account sent to a forced password reset
  that has no local password to issue it for. The refusal no longer names an account sign-in does
  not reach. An identity held by an owner
  account sign-in reaches is still refused by name with `409`; that wording is a declared exception
  in `SECURITY.md`.

- **The rate limiters no longer let a client choose its own bucket behind a proxy that appends a
  port.** With `PROXY_HEADER=X-Forwarded-For`, a trusted proxy that writes the client as
  `address:port` had that entry dropped before the limiters read the chain, so they keyed on the
  client-written entry to its left and a new value per request bypassed the per-address limits and
  the per-client sign-in attempt budget. The limiters now read every `X-Forwarded-For` line
  themselves, accept `address:port` and `[IPv6 address]:port` with a numeric port, and fall back
  to the direct peer when an entry does not parse, or is empty, instead of stepping past it.

  **Operators behind a proxy that writes something other than an address into
  `X-Forwarded-For`** (`unknown`, an obfuscated `_hidden` name, `unix:` from a unix-socket
  listener): every client of that proxy now shares the proxy's own rate-limit bucket, so busy
  instances will see `429`s where they saw none before. Set `PROXY_HEADER=X-Real-IP` and have the
  proxy write the client address there — the bundled proxy examples already do.

- **Cloud metadata stays unreachable through a local-use NAT64 prefix.** With
  `WEBHOOK_BLOCK_PRIVATE_ADDRESSES` off, a webhook target in `64:ff9b:1::/48` was delivered to,
  so a network with a translator on that prefix could route a reminder to `169.254.169.254`. The
  whole prefix is now refused in that mode, as it already was with the setting on.

- **A first start that fails to record its restore-fence marker no longer costs every owner their
  password on the next start.** A first start of v2.0 wrote the calendar-feed restore fence file and
  then had its database write fail — a locked or full database, a transient Postgres error. The next
  start read the lone file marker as a restored backup and signed every account out, removed every
  webhook endpoint and revoked every local password, on an instance that had never served. The
  first start now marks its file write as unfinished until the database holds the marker, so the
  next start simply completes the first start.

- **An operator command moves the restore fence once.** `ovumcy users delete`, a forced
  `reset-password`, `users set-email` and `webhook set` confirmed and advanced the fence, and the
  write that followed advanced it a second time. A fence file that failed on that second write left
  the two halves disagreeing, and the next start revoked every session, webhook endpoint and local
  password on the instance, including the password the operator had just set. The confirmed
  advance now covers that write.

- **A sign-out or credential change made while the restore-fence file briefly failed can no longer
  be undone by restoring a backup.** A sign-out, password change or reset, TOTP toggle, OIDC link or
  unlink or recovery-code rotation that met a failing fence-file write recorded the change in the
  database alone. If a backup taken before it was restored before the next start, the restored
  database agreed with the untouched fence file, and the signed-out cookie or replaced credential
  worked again. These writes now retry the fence-file write, and when the fence volume stays
  broken they are refused with nothing changed and can be retried; a refused sign-out keeps the
  session so it can be signed out again. Clear-data and account deletion still go through, as
  before, after the same retries.

- **A calendar-feed or webhook revocation made while the restore-fence volume was unmounted can no
  longer be undone by restoring a backup.** Revoking or rotating a feed, or saving or removing a
  webhook, over a fence volume that had dropped out of a running server recorded the change in the
  database alone. If the volume was mounted again and a backup taken before the change was restored
  before the next start, the restored database agreed with the old fence file, and the revoked
  subscribe URL or removed webhook endpoint worked again. These writes now retry the fence-file
  write like sign-outs do, and when the fence file stays unwritable, reads as missing or cannot be
  read, they are refused with nothing changed and can be retried.

- **A forwarded hop with port `0` or a zero-padded port no longer counts as an address.** The
  rate-limit key reads such an `X-Forwarded-For` entry as unparsable and falls back to the
  direct peer, as it already did for an empty or non-numeric port. The security notes now name
  the one NAT64 case the webhook metadata guard does not decode: an operator-chosen
  network-specific prefix.

- **An OIDC sign-in under closed registration answers like any other refused sign-in.** This is
  defence in depth, not a fix for a leak in a running instance: startup refuses
  `OIDC_AUTO_PROVISION=true` unless `REGISTRATION_MODE=open`, so no deployment could reach this
  path. If that combination were ever allowed, an unlinked identity whose email no account held
  would have been refused with "sso temporarily unavailable", and one whose email an account held
  with "sso sign-in unavailable". That difference would tell anyone with an identity at the
  configured provider whether an email has an account. Both now get "sso sign-in unavailable", and
  the audit line records the refusal as `cause=registration_closed`.

- **The server no longer starts on a database that has lost its normalized-email unique index.**
  `idx_users_email_normalized` is the only thing that stops two accounts from sharing one email
  address; sign-in already refused an address two accounts share, but that locks both accounts out
  after the fact rather than preventing the duplicate. The index can only go missing when it is
  dropped outside the application or lost in a restore, and it never comes back on its own. After the
  migrations apply, the server now reads the index's definition from the database (`sqlite_master` on
  SQLite, `pg_class` and `pg_index` on Postgres) and refuses to start if it is missing, is not the
  unique index on `lower(trim(email))` that migration 002 builds, or, on Postgres, is marked invalid
  (as a failed `CREATE INDEX CONCURRENTLY` or `REINDEX` leaves it). Nothing is repaired at startup.
- **Operators:** the refusal names the index and the fix. With the server stopped, run
  `DELETE FROM schema_migrations WHERE version = '002'` (after `DROP INDEX idx_users_email_normalized`
  when the refusal says the index has a different definition or is invalid) and start again:
  migration 002 is re-applied and re-creates the index, or refuses and names the address if two
  accounts already share one.

- **Mistyped passwords while turning off two-factor sign-in no longer lock out other accounts on
  the same network.** The password check that confirms turning off two-factor sign-in counted
  failed attempts per network address, so on an instance shared behind one address (a household
  router) one owner's mistakes could block another owner from turning it off. The count is now
  kept per account and address, the same way the other password-confirmed Settings actions keep
  theirs. The attempt limit per account is unchanged.

- **Breaking (API shape): a settings password check no longer accepts a password sign-in would
  refuse.** Signing in with a password is refused for an account whose local sign-in is turned off,
  even when a password hash is still stored for it. The settings actions that ask for the current
  password again — clearing data, deleting the account, regenerating the recovery code, turning
  two-factor on or off, and linking or unlinking a single-sign-on identity — checked only that a
  hash existed, so on such an account they accepted a password that could no longer sign in. They
  now answer that account the way they answer one with no local password at all: the same refusal
  a wrong password gets, at the same cost, drawn from the same attempt budget. Changing the password
  is unaffected; it already sends such an account to the single-sign-on step-up. None of the
  application's own sign-up, sign-in or settings flows leave an account in this state; it arises
  only from data changed outside them.

- **A correct current password no longer clears the re-authentication attempt count when the
  password change it confirmed is then refused.** Changing the password in Settings draws on the
  same attempt budget as the other password-confirmed Settings actions, and a correct current
  password cleared it as soon as it was checked, before the new password was saved. The count is
  now cleared only once the new password has been saved, so a change refused on the way to
  storage leaves the earlier failed attempts counted. Responses are unchanged.

- **A correct Settings password no longer clears the re-authentication attempt count when the
  change it confirmed is then refused.** Clearing data, deleting the account, regenerating the
  recovery code, turning on two-factor sign-in and unlinking an OIDC identity each ask for the
  current password against one shared attempt budget. A correct password cleared the wrong
  guesses counted before it as soon as it was checked, even when the change itself then failed
  (a sign-out everywhere landing mid-request, a storage error, a recovery code that could not be
  delivered). The count is now cleared only once the change has been saved, so a refused change
  leaves the earlier failed attempts counted. Responses are unchanged. The clear-data password
  pre-check clears the count once the password is confirmed, and the start of linking an OIDC
  identity once it has redirected to the provider; neither saves anything itself.

- **A correct 2FA code no longer clears the attempt count when the sign-in it earned fails.** The
  2FA challenge forgave the client's earlier wrong codes as soon as the code checked out, before
  the session was issued and before an SSO sign-in's provider-logout state was moved onto it. When
  either step failed, the sign-in answered with an error and the count was already gone. The count
  is now cleared only once the session has been issued; a sign-in that fails after the code check
  keeps the wrong codes counted before it.

- **A wrong 2FA enrollment code now draws its own per-account attempt budget.** Confirming
  two-factor sign-in checked the password against the settings re-authentication budget, which
  counts only a wrong password, so the code itself was not counted. Each wrong code now counts
  against a budget of 5 per 15 minutes per account; once it is spent the confirmation answers
  `429`, even for the correct code, until the window passes, and a successful enrollment clears
  it. A missing or wrong-length code is not counted.

- **Operator commands now refuse a database the server refuses to start on.** `ovumcy users`,
  `reset-password`, `link-oidc-identity`, `webhook` and `notify` run the same normalized-email index
  check the server runs at startup, and answer with the same `schema check failed: …` message before
  they ask for a password or read or write an account. Before, `users create` or `users set-email` could put a second
  account on one email address while the server was down because the index was missing.
- **Operators:** remove any duplicate accounts in SQL with the server stopped, then restore the index as
  the refusal describes. `ovumcy repair` is unaffected.

- **A correct password no longer clears the attempt count when the sign-in it earned fails.**
  Password sign-in forgave the client's earlier wrong passwords as soon as the password checked
  out, before the session, 2FA or forced password-reset step was issued. When that step failed,
  the sign-in answered with an error and the count was already gone. The count is now cleared only
  once the sign-in has moved on to its next step; a sign-in that fails after the password check
  keeps the wrong passwords counted before it.

- **The code that confirms a 2FA enrollment can no longer be replayed at the first sign-in.**
  Enabling two-factor sign-in reset the account's TOTP replay floor to zero, so the confirmation
  code, still inside its ±30-second validity window, passed the next sign-in challenge as well.
  Enrollment now records the time step that code matched in the same write that enables 2FA,
  and the sign-in challenge refuses it like any other reused code. Disabling 2FA still resets
  the floor along with the secret.

- **The migration refusal over duplicate rows no longer prints the conflicting keys.** When a
  migration adds a unique index to a table that already holds colliding rows, the refusal ends the
  boot and is logged on every restart. It used to spell out the keys — an owner id with symptom
  names, or an email address. It now reports only the table, the index and how many conflicting
  groups there are (capped at "more than 5"); the rows themselves are read offline with
  `ovumcy repair`, as the runbook describes. No migration SQL or numbering changed.

- **A dashboard left open after a different account signs in no longer receives that account's
  status header, and says so.** After a save the Today page fetches the dashboard again to refresh
  its status header. That request now names the account the page was rendered for, and the server
  refuses it with the same "this page belongs to another account" answer the calendar gives, sending
  none of the other account's page. The tab shows the account-changed notice under the journal
  instead of dropping the refresh without a word. Opening the dashboard by its address, reloading
  it, following a link and navigating inside the app are unchanged.

- **A request the server cannot read no longer receives a cookie.** When a request's headers
  overflow the server's read buffer (`431`), are malformed (`400`) or time out (`408`), Ovumcy
  answers without having read the request at all, yet that answer still carried a fresh
  anti-forgery cookie, minted for a request that never existed — and it replaced the browser's own,
  so a form already open in another tab went stale. Those answers now set nothing.

- **The onboarding gate decides on the path the router dispatches on.** For an account that has not
  finished onboarding, the gate exempted the onboarding page and API by the prefix of the raw request
  path, so `/api/v1/onboarding/../days/<date>` read as an onboarding request. That was safe only
  while the router took the `..` segment literally and matched no route; a router release that
  removes dot segments before matching dispatches it to the days route. The gate now reads the
  routed path, and refuses any request whose path carries a `.` or `..` segment, an empty segment,
  or was rewritten by the router on the way — such a path is never produced by the app itself.
- **The request read buffer is pinned at 4 KB.** A request head (start line plus every header,
  cookies included) larger than that keeps answering `431` with the `request_headers_too_large`
  envelope, instead of depending on the framework default, which a newer Fiber release raises.

- **The settings page no longer shows any part of a webhook endpoint's path.** The destination
  line showed the first path segment in clear, and for a Telegram bot URL
  (`https://api.telegram.org/bot<id>:<token>/sendMessage`) that segment is the bot token. The line
  now shows the host and, when it is not the default, the port — nothing else.
- **A removed webhook endpoint can no longer be re-armed by a settings save that was already on
  its way.** A save that keeps the stored endpoint read it, then wrote it back; a removal landing
  between the two was undone. Every webhook-settings save is now pinned to the settings it was
  decided from, and one that finds them changed writes nothing and asks the owner to check the
  current settings and try again (HTTP 409 for API clients).
- **A webhook URL field holding only spaces no longer deletes the stored endpoint.** It is now
  read as the blank "keep the stored endpoint" field.

- **Open registration now blocks private-address webhook egress by default, and cloud metadata is
  refused in every mode.** `WEBHOOK_BLOCK_PRIVATE_ADDRESSES` defaulted to `false`, so on an
  instance with `REGISTRATION_MODE=open` anyone who signed up could make the notify pass POST to
  loopback, RFC 1918 or `169.254.169.254`, and read the 2xx back from settings as a liveness and
  port oracle for the operator's network. Unset, the guard now follows the registration mode: on
  under `open`, off under `closed`, where household owners target their LAN. An explicit value
  still wins. Whatever it says, `169.254.0.0/16`, `fd00:ec2::254` and `fd20:ce::254` are
  refused, literal or resolved, through the same resolve-and-pin dialer, which now tries IPv4
  records first. The server boot and `ovumcy notify` resolve the gate through one function.

  **Upgrading to 2.0.0:** an open-registration instance whose owners deliver to LAN ntfy, Gotify
  or Home Assistant stops reaching those targets until you set
  `WEBHOOK_BLOCK_PRIVATE_ADDRESSES=false` (the startup warning then names the exposure) or close
  registration. A closed instance with more than one owner should set it to `true`. An externally
  scheduled `ovumcy notify` reads `REGISTRATION_MODE` from its own environment, and unset means
  `open`: give the cron or systemd job the server's value, or its pass refuses the LAN. Webhook
  delivery ignores `HTTPS_PROXY`; delivering through an egress-only proxy is unsupported and now
  documented as such.

- **Turning a webhook off now also stops the reminder pass that is already running.**
  The pass reads every owner's notification settings once, at the start of its run, and sends each
  reminder some time later. Everything an owner can do to narrow or revoke it — switching delivery
  off, replacing the endpoint, removing it, shortening how far ahead reminders arrive, clearing all
  their data — changed only settings the running pass had already read past, so a pass in that
  window could still POST a period or ovulation
  reminder to the endpoint the owner had just been told was gone. The per-reminder watermark could
  not catch it: a settings save deliberately leaves the watermarks alone, and a clear-data wipe
  clears them, which put the pass back on its "nothing sent yet" path and let it send.

  Each of those writes now advances a per-owner revocation counter in the same statement that
  performs the write, and the claim the pass takes immediately before every request pins the counter
  its own snapshot carried. A pass holding settings the owner has revoked loses that claim and
  delivers nothing. The clear-data case advances the counter rather than resetting it, because a
  reset would have handed the stale pass its own value back.

  One limit is worth stating plainly, because it cannot be closed from here: a request that has
  already left cannot be recalled. If the pass had sent the POST at the moment the setting changed,
  that one request completes. What is guaranteed is that no new request begins under settings the
  owner has revoked.

  The counter also moves for a webhook-settings save that edited nothing, so such a save landing
  while the pass runs costs that pass its remaining reminders for that owner. They arrive on the
  next run — except at a lead time of zero days, where a reminder is due on a single calendar day
  and no later run still covers it, so that cycle's reminder is skipped. (The lead-time form skips
  the write when the submitted value matches the one the page was rendered with, so re-submitting
  it unchanged normally costs nothing.) `docs/notifications.md` states both limits where an owner
  will meet them.

  Schema: adds `users.webhook_config_version` (migration 038, both engines).

- **The webhook response envelope now bounds response headers, not just the body.** The body has
  always been capped at 8 KiB, but headers are read before that cap applies and took Go's 10 MiB
  default — three orders of magnitude past anything a webhook acknowledgement needs, read from an
  owner-controlled endpoint. Headers are now capped at 16 KiB, and the TLS handshake and the wait for
  response headers each get their own 5 s bound so one slow phase cannot spend the whole delivery
  budget. The total per-delivery timeout is unchanged.

- **The webhook private-address block now refuses every address the IANA special-purpose
  registries record as not globally reachable.** With `WEBHOOK_BLOCK_PRIVATE_ADDRESSES=true` the
  gate knew only the ranges that had been reported to it, so a reminder aimed at RFC 2544
  benchmarking space (`198.18.0.0/15`), IETF protocol assignments (`192.0.0.0/24`), the reserved
  `240.0.0.0/4` including the broadcast address, multicast, or documentation space (`192.0.2.0/24`,
  `198.51.100.0/24`, `203.0.113.0/24`, `2001:db8::/32`, `3fff::/20`) was delivered as if it were the
  public internet — the first three of those route on real networks. All of them are refused now,
  along with the IPv6 discard-only, benchmarking, ORCHIDv2, drone-remote-ID and SRv6-SID blocks. The
  special-purpose prefixes the registries do record as globally reachable — AS112, AMT and the
  `2001:1::1-3` anycast addresses — stay deliverable, as does the rest of the public internet; the
  one deliberate exception is the PCP/TURN anycast pair `192.0.0.9`/`192.0.0.10`, refused together
  with the `192.0.0.0/24` block that contains them. An
  operator who had the gate on and a webhook endpoint inside one of those ranges will see that
  endpoint refused from this release; the gate remains off by default, so nothing changes for
  anyone who has not opted in.
- The webhook delivery dial (DNS plus TCP connect) is now bounded by the 5-second per-phase budget
  rather than the full 10-second envelope, so a hung connect can no longer consume the whole budget
  and leave the TLS handshake and the response-header wait with nothing. The total is unchanged.

### Internal

- **CI: a push to `main` now runs the unit and race lanes itself whenever the merge-queue run did
  not execute them (proven-tree skip)**, so the release gate finds a successful
  `test-go-shard (1)` and `race-db (1)` on the tagged commit. (Added to this release's notes
  after it was published: the change shipped in the 2.0.0 tree, but its entry was written after
  the notes were assembled.)

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

- The JSON restore's audit line moved onto the typed mutation helpers. It was
  the one health-data line assembled by hand — it needed to append the additive
  counts, and the helpers took no extra fields, so it wrote `domain` and
  `target` out itself and would have been skipped by any later change to how
  those tags are produced. The helpers now accept extra fields, exactly as their
  egress counterparts already did. The emitted line is unchanged: same action,
  same tags, same counts, and the emitter's alphabetical field order makes the
  text byte-identical.

- **The auth policy document is now read back by a test.** `docs/security/auth-policy-and-rate-limits.md`
  states the per-IP rate-limit budgets, the environment variables that tune them, and the per-account
  lockout thresholds; nothing read any of those numbers back, so the document could drift from the
  server without a single check going red. Three tests now pin it in both directions: a budget stated
  in the document must match what the boot resolves, a `RATE_LIMIT_*` variable read by
  `cmd/ovumcy/config.go` must have a row in the document, and the ceilings and window bounds the
  document states must be the ones `config.go` enforces. The per-account thresholds are compared
  against what `bootstrapOptions` actually wires rather than against the service constants they fall
  back to — the recovery budget ships as the one-hour `RATE_LIMIT_FORGOT_PASSWORD_WINDOW` default,
  not as the fifteen-minute constant behind it, and pinning the constant would have asserted a value
  no request ever meets.

- Characterisation tests pin the window's behaviour across skipped and disturbed days, and pin
  that a rise climbing in steps below the margin is not confirmed.

- **The boot sequence is swept for unbounded passes rather than trusted.** A guard reads the window
  in `main()` between the repositories being built and the dependencies being wired — which is what
  makes something a boot pass: the database is open, the migrations have applied, no listener exists
  — and holds every call it finds there to taking its context from the shared budget. A fourth pass
  added to that window is covered without anyone remembering to extend a list. The guard fails if
  the window yields fewer calls than the passes known to run there, so a sweep that reached nothing
  cannot report success, and a companion test runs both checks against synthetic sources to prove
  they can separate a bounded pass from one reaching for `context.Background()`.

- **The barrier that guards this class now reads the third shape it comes in.** A sweep over the
  shipped source already failed the suite for building a calendar day as an instant in a timezone,
  and for ordering a location-anchored day against a UTC-anchored one. It did not read the shape
  that produced the fixes above — a day STEPPED from a location anchor — and its own classifier
  asserted that such a step "keeps the anchor", which is untrue on exactly the dates that matter.
  The sweep now flags the stepping shape, carries a floor so it cannot pass by reading nothing, and
  states in its blind-spot list that it only sees a step whose anchor is built in the same function.
  One site is allowlisted with its reason: a step of whole years producing the lower bound of a log
  fetch window, where the one-sided slip widens the window by a day and never narrows it.

- **Seven files are `gofmt`-clean again.** The release checklist declares a formatting gate before
  a tag, and the tree failed it: `gofmt -l` over the scoped package list named two `internal/api`
  regression tests, four `internal/services` files and `scripts/changelogd/main_test.go`. Six were
  struct- and comment-alignment drift; `webhook_reminder.go` also gains the blank comment lines
  gofmt inserts between the items of a list in a doc comment. No behaviour changes — `git diff -w`
  is empty except for those comment lines.

- Twenty-five locale keys left unrendered by the settings density pass are
  deleted from all six catalogues.

- **The changelog-fragment gate now passes dependency bumps without a fragment.**
  The check stays required for every pull request and keeps its `merge_group`
  trigger; it returns success immediately when the pull request's author is
  `dependabot[bot]`, which cannot write a fragment. Dependency updates therefore
  no longer reach `changelog.d/`. At release time the operator compiles a
  release's `### Dependencies` section by hand from `git log <last-tag>..HEAD`
  over the dependabot merges and enters it as one fragment before running
  `changelogd assemble`, which itself reads only fragments and the frozen
  `[Unreleased]` body.

- **Config entries that can no longer match anything are gone.** Trivy's filesystem scan skipped a
  `.tmp_docker_ctx` directory nothing has ever created; `.dockerignore` and `.github/CODEOWNERS`
  still named a `docker/` tree removed in `90d85a4`; `.dockerignore` excluded a
  `tailwind.config.js` that Tailwind v4 does not use; and both ignore files carried `.tmp_*/`
  alongside the `.tmp*/` that already subsumes it.
- **Two build outputs are now marked generated.** `web/static/js/theme-bootstrap.js` and
  `timezone-bootstrap.js` are written by `scripts/build-js.mjs` from `web/src/js/`, and were the
  only build products missing `linguist-generated` in `.gitattributes`.
- **The mutation shard guard's comment now describes the failure it prevents.** It cited a
  `.mutation/<slug>.md` "pending first weekly CI run" state that exists in no file and under no
  slug spelling; what the guard actually stops is a shard passing green having uploaded nothing.

- **CI no longer caches `golangci-lint`'s analysis cache.** The cache key could
  only name `go.sum` and `.golangci.yml`, never the source tree, so a branch
  whose dependencies were unchanged but whose commits had moved was linted
  against facts computed from a different tree — which produced six
  deterministic phantom `staticcheck SA5011` findings on a dependency bump that
  reproduced on no other machine. Restoring the cache was measured at 1–5 s on a
  job of about six minutes, so removing it costs effectively nothing.

- **Merge-queue validation runs are judged by the same docs/meta relevance diff as pull requests.**
  The `changes` job only computed a diff for `pull_request`; every other event, the merge queue's
  own validation run included, fell into the catch-all branch and paid the full battery. So a
  change already judged irrelevant on its own pull-request run still ran `test-go`,
  `test-frontend` and `race` a second time inside the queue — `race` being the lane the queue's
  60-minute status-check timeout is measured against. The queue branch is its base plus every
  pull request batched into the group, so one diff against `merge_group.base_sha` judges the
  whole batch. Push, release and `workflow_dispatch` still run in full: the post-merge safety net
  is unchanged. Undetermined always means run — an unknown event, a missing or unreachable base,
  a failed diff and an empty file list are each written out as their own branch, and the
  consuming jobs repeat the fail-safe once more (`!= 'false'`, plus `!cancelled()`), so a
  detection job that dies can no longer skip a required check into a satisfied state.

- **The repo-root `osv-scanner.toml` is documented as live configuration, with its consumer named.**
  It was unreferenced anywhere in the tree, so its relevance kept being re-opened. The OpenSSF
  Scorecard workflow's Vulnerabilities check embeds the osv-scanner library and scans the
  checked-out tree; with no config path passed, osv-scanner resolves its config next to the scanned
  manifest, which for the repo-root `go.mod` is that file. Verified by running the same
  osv-scanner version against `go.mod`: it loads the file and filters `GO-2026-5932`, the advisory
  that covers every `golang.org/x/crypto` version with no fixed release. The file now says which
  workflow consumes it, and `docs/SECURITY_INVARIANTS.md → CI` records the suppression alongside
  the digest-pinning exception.

- **The `test` gate reports a verdict instead of disappearing when a unit half fails.** The job had
  no `if`, so a failed dependency skipped it — and GitHub reports a skipped job as a satisfied
  required check. Measured on a throwaway branch carrying one deliberately failing Go test:
  `test-go` failed, and `test`, `e2e` and `patch-coverage` — three of the fourteen required
  contexts — all reported SKIPPED. `race` gates on the `changes` job rather than on `test`, so it
  ran and passed, and nothing else covered the unit suite. The job now runs under `!cancelled()`
  and turns its two dependencies' results into its own verdict: `success` and `skipped` pass —
  the latter is the docs-only decision not to run the battery — and anything else fails the
  context, so a failing unit suite is red where branch protection looks.

- **The day panel's projection check searches the out-of-date suppression reason among the strings
  that must name its words.** The word "stale" was found only in an unrelated settings error key,
  so rewording that key would have failed the check for a reason it does not test. The
  `cycle_data_stale` reason is now one of the searched strings; the check is satisfied by any
  string that names the word, so the reason is an additional anchor, not the only one.

- **`brace-expansion` lifted to 5.0.9 in the lockfile**, closing CVE-2026-69152
  (HIGH) and turning the required `trivy-fs` check green again. The package is a
  transitive dev-only dependency of the build toolchain and is not part of any
  shipped artifact, so no running instance was exposed. The `overrides` entry
  stays the floor `^5.0.8` — 5.0.9 satisfies it, so only `package-lock.json`
  changed.

- **The cold-start theme claim is now measured rather than asserted in a comment.** A browser
  check opens a page with a dark theme already stored and compares the moment `html[data-theme]`
  is written against the browser's own first-paint entry; a structural check pins the bootstrap
  script ahead of the stylesheet and free of `defer`/`async`. Both pass as they stand — no flash
  was found — and stay as the guard against a reordering that would introduce one.

- Browser checks pin the calendar phase cells, the selected tiles, the language pill, the slider
  target size and the closed confirm dialog; template checks pin the decorative-glyph rule, the
  named navigation landmarks, and the two audit findings that needed no change — the
  password-manager username fields and the closed confirm dialog are already outside the
  accessibility tree.

- **`docs/self-hosted.md` miscounted the OIDC cookies in its `431` arithmetic.** The section said
  "the two OIDC cookies only apply on the callback path"; the app defines four —
  `ovumcy_oidc_auth` and `ovumcy_oidc_stepup` on `/auth/oidc/callback`, `ovumcy_oidc_link_pending`
  on `/auth/oidc/link-confirm`, `ovumcy_oidc_logout_bridge` on `/auth/oidc/logout`. The exclusion
  argument the section rests on gets stronger, not weaker: each one is `Path`-scoped to the single
  endpoint that consumes it, so none reaches an ordinary page request.
- **Both operator-facing measurements now carry their date and the release they were taken on.**
  The SQLite write-concurrency table was a drill of 2026-07-28 on the then-current release image
  (v1.9.2); the cookie byte counts were measured from the running app on 2026-07-25 against the
  same release. Neither said so, so a reader could not tell which build produced them or whether
  they had gone stale — and nothing in the repository re-derives either.

- **`docs/architecture.md` and `docs/gdpr.md` stop contradicting the security suite.** Accuracy
  pass from the 2026-08-28 documentation audit. `architecture.md`'s no-secret-in-transport bullet
  claimed recovery codes never appear in HTML and named the calendar-feed token as the single
  sanctioned carve-out — both halves false against the suite's own ledger; it now defers to
  `docs/SECURITY_INVARIANTS.md` and names the feed token (the one exception usable on its own),
  the TOTP enrollment seed (the second, narrower declared exception) and the two sanctioned
  one-time HTML reveals. `gdpr.md`: the `oidc_logout_states` parenthetical still described
  pre-031 NULL rows aging out by TTL — migration 033 purged them and `Save` refuses unattributed
  writes, so erasure covers the whole table immediately; the Art. 16 row no longer claims every
  profile field is editable (the email address has no in-app rectification control, and the
  operator column now says what fulfilling that request actually takes); the retention advice no
  longer prescribes "an operator cron job calling the same internal repository methods" — those
  are unreachable from outside the binary, so the doc now names what exists (scoped direct SQL,
  or a feature request) and scopes "no built-in scheduler" to retention, since the reminder
  scheduler exists; the restore-drill anecdote, which no dated record backs, is replaced by the
  mechanism statement; the `/privacy` paragraph defers the egress enumeration to the canonical
  statement in `docs/security/data-handling.md`. No behaviour changes.

- **The auth-policy and data-handling concern docs stop misdescribing the code.** Accuracy pass
  from the 2026-08-28 documentation audit. `auth-policy-and-rate-limits.md`: the recovery-code
  offline-guessing bound no longer names `SECRET_KEY` — the hash is plain bcrypt and verification
  compares it directly, so a database leak on its own already permits offline candidate testing
  (regenerate outstanding codes after any database compromise); the settings re-auth budget's
  enumeration gains the two password-gated actions it omitted (`PUT …/2fa` enrollment
  confirmation, `POST …/recovery-code` regeneration); the single-document-era pointers ("see
  *Cookies* above", *Session Invalidation*, *Field-Level Encryption*) become links to the sibling
  concern docs they moved to. `data-handling.md`: the canonical egress statement now covers the
  pull-based `.ics` feed alongside the push integrations; the `users`-table inventory gains the
  migration-036 reveal consumption marks (`recovery_code_revealed_at`,
  `calendar_feed_revealed_at`) and the clear-data contract records two facts the code already
  held — the wipe blanks the stored `timezone`, and the reveal marks deliberately survive it;
  the *Logging Policy* and *Field-Level Encryption* pointers become file links. No behaviour
  changes.

- **The security documents now state the calendar feed's real response contract.**
  `docs/SECURITY_INVARIANTS.md`, the `SECURITY.md` matrix and `docs/openapi.yaml` all claimed the
  feed answers its rejections as a bare status with no body — a body-less `404`, and a bare `429`
  carrying only `Retry-After`. The code never matched the first half in its detail — `c.SendStatus`
  has always filled the fixed status text, so the `404` is a cause-free `Not Found` body, identical
  for a malformed token, an unknown selector, a wrong verifier and a disabled feed (pinned by
  `TestCalendarFeedReturnsBare404WithoutOracleForBadTokens`) — and deliberately stopped matching
  the second when the feed's `429` joined the shared limiter path like every other route
  (`RespondCalendarFeedRateLimited`, required by the rate-limit envelope regressions). The three
  documents, the `envelopeExemptRoutes` reason string and the test comments now describe that
  contract: the envelope exemption covers the feed's not-found answer, never the route wholesale.
  The no-oracle property itself was never wrong and is unchanged. In the same pass:
  `SECURITY_INVARIANTS.md`'s opening quantifier now scopes the test-enforcement promise to
  test-enforceable entries (the matrix's own carve-out); `SECURITY.md` states the rolling support
  policy explicitly (a fix ships in the next release, never as a backport), scopes the Art. 15 row
  to the day-level export `docs/gdpr.md` describes, enumerates what Art. 16 actually lets an owner
  edit (not the email address), qualifies "egress paths are exactly two" to standing third-party
  paths, and uses a `vX.Y.Z` placeholder in the verification examples. No behaviour changes.

- **`CHANGELOG.md` and eight `changelog.d/` fragments stop misdescribing what shipped.** Accuracy
  pass from the 2026-08-28 documentation audit (package D). In `[Unreleased]`: the release-image
  publish entry no longer claims tagged releases "publish from their own tag push as before" — the
  release-image-gate fragment landing in the same version makes that untrue, since a tag now gates
  on the same checks `latest` waits on; and the OpenAPI-exemptions entry no longer counts the
  calendar feed's per-IP `429` among the bare-body exemptions — a later entry in the same
  `[Unreleased]` section moves that `429` onto the shared envelope, leaving only the feed's `404`
  path exempt. In `changelog.d/`: a stray `</content>` tag dropped from the API-duplication sweep's
  fragment; a fragment whose entry had become moot rewritten to the bare `none` marker; a
  plain-paragraph entry wrapped as a bullet, the form every released `CHANGELOG.md` section uses;
  two positional cross-references ("as the next entry describes", "the entry below") replaced by
  naming the target entry's own title instead — one of the two pointed the wrong direction once
  fragments are sorted by filename for release assembly; the release-image gate's Dependabot claim
  corrected — the pre-release-tag gap is closed by a CI check, not a Dependabot `ignore` clause, which
  `dependabot.yml`'s own comment explains cannot express "no `rc` tags" without risking every
  ecosystem's feed; two source-line counts and the ratio derived from them re-measured against the
  current tree; and a dark-theme fix's cycle-ring contrast numbers marked superseded by the wave-4
  cycle ribbon that replaced the ring, with its corner-glow claim corrected now that a later pass
  gave the dark theme its own tinted glow instead of leaving the corner bare. No response, template,
  or behavior changes; the link-block item this audit package also named (F0) had already shipped in
  #629.

- **The changelog's link-reference block knows about 1.9.1 and 1.9.2.** `[Unreleased]` compared
  `v1.9.0...HEAD`, so its diff carried two released versions' changes as if unreleased, and the
  `[1.9.1]`/`[1.9.2]` headings rendered as literal bracketed text with no target. Both patch
  releases are tagged; the block now defines them and `[Unreleased]` starts at `v1.9.2`.
  Assembly never maintained this block — it is a per-release manual step that was missed twice.
- **Release 1.3.0 is dated 2026-06-15**, the date of the tag and of the very commit that wrote
  the heading (`63ad241f`); it was written two days early at authorship. Every other release
  heading matches its tag.
- **`[Unreleased]` carries one `### Added` section, not two.** The OIDC self-erasure entry sat
  in a second `### Added` after `### Changed`, which the declared Keep a Changelog format does
  not do. Assembly already merged repeated headers, so this only ever affected the rendered
  file — the entry text is moved, not edited.
- **The brand README documents the dark-background tints.** `ovumcy-icon-dark.svg` and
  `ovumcy-logo-horizontal-dark.svg` use `#CFBBFF` and `#FFA2AF`, which the palette never
  mentioned — a designer recreating a dark asset from this file reached for the light-background
  values.

- **`CONTRIBUTING.md` now describes the changelog gate's Dependabot exemption.** The guide still
  said every pull request adds a fragment and that the check fails any pull request without one,
  which stopped being true when the gate began short-circuiting for `dependabot[bot]`.

- **The security docs now declare the TOTP enrollment seed as the second bounded exception to *no
  usable secret in transport*.** The enrollment page has always rendered the seed — as the QR code an
  authenticator scans and as the manual-entry string beneath it, which is the only way an
  authenticator can be provisioned — while the invariant text named TOTP secrets flatly among the
  values that never reach transport and called the `.ics` capability token the single exception. The
  code was right and the text was short: `docs/SECURITY_INVARIANTS.md`, `docs/security/cryptography.md`
  and the `SECURITY.md` matrix now state the exception together with the bounds that make it safe —
  minted fresh on every visit and never re-served from storage, held only in the sealed owner-bound
  five-minute enrollment cookie until the first code verifies, written to the account (field-encrypted,
  AAD-bound) only by that verification, and shown only on an authenticated owner page. The `.ics`
  token remains the only exception for a value usable on its own; the enrollment seed is a
  not-yet-active credential, inert until the account confirms it. No behaviour changes.

- **Removed `docs/screenshots/login.png`** (307 KB), which nothing referenced.
  The README shows six screenshots and the login screen is not among them, and
  the file sits outside the reusable asset set `docs/hero-demo.md` defines. The
  directory's other screenshots are all rendered by the README; the two files that
  are not, `ovumcy-promo-card.svg` and `ovumcy-social-preview.png`, are outbound
  assets consumed outside the repository and carry no in-repo reference by design.

- **The export doc described the CSV `Cycle factors` cell as machine keys.** It emits the same
  human-readable labels the app shows — `Stress`, `Sleep disruption` — joined with `"; "`, not
  the lowercase enum strings the JSON export carries; a consumer coding against the doc
  mis-parsed every non-empty cell. The separator nit is fixed on the `Other` column too, and
  the JSON `cycle_factors` field no longer calls its five-key catalog "free-form".
- **The documented `Content-Disposition` header now matches the one the server sends** — the
  filename is unquoted on both the JSON and the CSV response.
- **`docs/cycle-prediction.md` documents the first-cycle floor.** The document claimed to
  describe the estimates "in full" while the fertility half — ovulation date, fertile window,
  peak band, feed events, webhook reminder, dashboard banner — has been withheld until one
  cycle completes since the floor landed. The math and every worked example were and remain
  correct; what was missing is the gate that decides whether those numbers reach a surface.
- **`docs/hero-demo.md` names the whole pack and the scripts that generate it.** `docs/demo.gif`
  (with `demo.mp4` behind it) is the README's hero and the doc never mentioned it, nor
  `scripts/take-screenshots.mjs` / `scripts/record-demo.mjs`, so a reader regenerating the pack
  from the Capture Checklist would leave the one moving asset stale and unreviewed for PII.

- **The cycle-prediction doc stated the personalized round trip as an equality it does not always
  hold.** "An ovulation observed on cycle day N predicts cycle day N again" is true only while the
  luteal phase that observation implies still fits the cycle; past that the reserve clamp applies and
  the prediction is marked non-exact, which the step immediately above it already documented for its
  own clamp. The invariant now carries the exception, and the worked-example table carries a row for
  it — an ovulation observed on day 4 of a 15-day cycle, where the implied 11-day luteal phase is
  clamped to 10 and ovulation is estimated on cycle day 5. That row is asserted by the same reference
  test as the others, so the exception is demonstrated rather than only described. No behaviour
  changes: the clamp is what the code already did.

- **`docs/cycle-prediction.md` now describes what the inferred luteal phase actually does with an
  out-of-range cycle.** It said the observed length is "clamped to a physiological 10–20 day
  range"; the code discards such a cycle instead, and falls back to the fixed 14-day default
  unless at least two cycles survive the filter. The difference is visible to an owner — a clamp
  would let one implausible reading pull the estimate to 10 or 20, and none does.

- **The security documents now state, and map to their tests, the rule that predictions are
  withheld rather than softened.** The Test Enforcement Matrix is the source-of-truth mapping from
  a claim to the Go test guarding it, and its medical-safety section listed only the disclaimer's
  wording — while the other half of that invariant, withholding a window the recorded data cannot
  support, had enforcing tests and no row. `docs/SECURITY_INVARIANTS.md` had the matching gap: its
  medical-safety section stated the disclaimer alone, though production code and the sweep's own
  failure text send readers there for the suppression floor, so the pointer resolved to a document
  that did not carry the claim.

  Both are fixed. The matrix row credits both tests that hold it — the sweep that fails when an
  expression outside the file declaring the predicates combines two suppression signals by hand,
  and the check keeping that sweep's list of signals answerable to the predicates themselves — since
  a claim held by two tests that credits one invites the deletion of the other. The row also states
  what the sweep cannot see: it reads one Go expression at a time, so a gate assembled across
  intermediate variables, or in a template, is outside its reach.

  Both documents state the rule at one strength, and it is the strength that holds today: every
  surface asks one predicate per half rather than rebuilding the rule, so a new signal is added
  once. Neither claims that the surfaces therefore agree — they feed that predicate different
  inputs, and reconciling those is a separate change.

  No behaviour, test or invariant changes.

- **Two `.mutation/` sections that read as current backlogs are framed as history.**
  `internal-api.md`'s "Survivors — not yet triaged" describes 128 untriaged survivors and a
  79.22% efficacy in the present tense; the same file's 2026-07-12 pass triaged every survivor
  of the later v1.8.0 baseline and the post-hardening re-measure (97.3%, 18 lived) is the
  canonical figure. `internal-security.md`'s "Documented survivors" presents 8 mutants with
  "line numbers track the current `oidc.go`" while the canonical state is 3 lived. Both keep
  their reasoning; only the headings and their prefaces change, so neither reads as a count
  of the tree today.

- **The OwnerOnly invariant now states its exclusions where the rule is stated.** The published
  rule — every state-mutating `/api/v1/*` endpoint chains `handler.OwnerOnly` — was written
  without exception, while the five endpoints it does not cover (register, login, the 2FA
  challenge, and the two password-reset routes, all of which run before any session exists)
  appeared only as keys in a map literal inside a test. `docs/SECURITY_INVARIANTS.md` and
  `SECURITY.md` now name that set, the test builds its allowlist from the one place the set is
  declared, and a new guard fails when the document and the declaration disagree in either
  direction — so a sixth exclusion cannot be added in a diff that reads like a test-fixture edit.
  The exclusion set is unchanged and no route changed: the five were always registered without
  `AuthRequired`.
- **The invariant now cites the test that can actually fail on it.** It named the role matrix in
  `internal/api`, which observes `AuthRequired` and stays green when a route drops `OwnerOnly`;
  the route-table guard in `cmd/ovumcy` is what enforces the chaining, and is now what the
  document points at.
- **The architecture map names the OIDC query-mode callback and the background scheduler.** The
  trust-boundary diagram showed only the `form_post` callback, though a `GET` callback on the
  same path is registered and supported under `OIDC_RESPONSE_MODE=query`; and the layering
  section listed no entry point other than HTTP, though `internal/reminders` drives the daily
  webhook notify pass with no request behind it and `internal/cli` reaches services and
  persistence directly under operator shell access. Both are now on the map, with `internal/apideps`
  and `internal/testdb` recorded as deliberately omitted wiring.

- **`docs/notifications.md` no longer claims no third party is involved in any channel.** The
  same document tells owners to subscribe the feed from Google Calendar, Apple Calendar or
  Thunderbird, and a webhook URL is validated only as absolute http/https, so an owner can and
  does point either at a hosted service. The wording now says no third-party service is
  *required or built in*, and names the owner's own choice as the thing that decides who
  receives the reminder data.
- **`docs/self-hosted.md` describes the rate limiter the code actually has.** It told operators
  the app keys per-IP limits on the leftmost, spoofable `X-Forwarded-For` entry; the edge
  limiters key on the rightmost untrusted hop and are not spoofable. The recommendation
  (a header the proxy overwrites) is unchanged; what is corrected is the mechanism, the
  residual (`c.IP()` feeding the secondary per-client auth buckets), and the claim that the
  app's default is `X-Real-IP` when it is `X-Forwarded-For`.
- **The self-hosted OIDC section no longer restates the account contract lossily.** Its
  "falls back to a verified email match" omitted the mandatory password-plus-TOTP confirmation
  — the exact takeover the gate exists to stop. The section is now the operator's decisions
  plus a pointer to `docs/oidc.md`, which owns the contract.
- **`docs/oidc.md` keeps the operator flow and points at the invariant's home.** The pending
  cookie's TTL, the retry behaviour and the already-claimed refusal had three prose homes; a
  TTL change had to be made in all of them. `docs/security/oidc-and-sessions.md` owns them now.
- **`/readyz`'s documented body is the JSON the handler writes** (`{"status":"ok"}` /
  `{"status":"unavailable"}`), not "a fixed one-word body" an operator might match on.

- **The documented local Go command now declares the timeout budget CI declares.**
  All four copies of `go test ./cmd/... ./internal/... ./migrations/... ./scripts/...
  ./web/...` — `TESTING.md`'s run block and its patch-coverage recipe, `CONTRIBUTING.md`,
  and `README.md` — pass `-timeout 20m`. Go's default is 10 minutes *per package* and
  `internal/api`'s DB-integration suite runs at that edge, so the documented command
  aborted with `panic: test timed out after 10m0s` and a goroutine dump that reads like
  a product bug; the CI workflow has budgeted every one of its own `go test` calls since
  the same measurement, and the docs handed contributors the unbudgeted spelling.
- **`TESTING.md` no longer states test counts that go stale.** The header said "2,300+ Go
  test functions across `internal/` and 29 Playwright specs"; the tree holds 2,720 and 33,
  and the spec figure had already been corrected once. Both numbers are replaced by the
  two commands that re-derive them, plus the fact that makes the spec listing authoritative
  (`playwright.config.ts` sets only `testDir: 'e2e'`, with no `testMatch`/`testIgnore`).
  The mutation section's "`internal/services` is some 40% bigger in source lines" —
  measured at 18,582 non-test lines against `internal/api`'s 11,453, so nearer 62% — loses
  the percentage for the ordering claim the sentence actually rests on.

- **The README no longer promises data never leaves your server.** The storage FAQ answered
  "and nowhere else", which the product's own optional features falsify: a webhook reminder
  POSTs the predicted period/ovulation dates to whatever URL the owner configures, and a
  calendar app subscribed to the `.ics` feed pulls those dates onto its own servers. The
  answer now names both paths as owner-enabled egress instead of denying them.
- **The README's CI sentence describes the events CI actually runs on.** It said staticcheck,
  vet, tests and the frontend build run "on pushes and pull requests"; the push lane on `main`
  clears `run_core` precisely because the merge queue already ran that suite on the same
  commit, and executes only the cross-browser e2e, image-smoke and Postgres-smoke lanes the
  queue skips.
- **`Go 1.26+` is now `Go 1.26.6+` in both places** (badge and Requirements) — `go.mod` requires
  `go 1.26.6`, which bites a `GOTOOLCHAIN=local` toolchain below that patch.
- **`CONTRIBUTING.md`'s local check list runs what CI's frontend lane runs.** It listed
  `npm run lint:js` and `npm run build` only, so a type error or a JS unit failure —
  `lint:types` and `test:unit`, both required in CI — went green locally and red in CI.
- **The untranslated-string instruction names a mechanism that can exist.** It asked for a
  `TODO(<locale>)` inside the locale file; those files are strict JSON parsed at boot, so a
  comment breaks startup, an added key fails `TestLocaleKeysParity`, and a marker inside the
  value stops being the verbatim English string the same sentence demands. Copied keys are
  listed in the pull request description instead.
- **The Dependabot changelog exemption names a real assembly step.** It said the
  `### Dependencies` section "is assembled from the dependency bumps that merged" — no such
  mechanism exists; `changelogd assemble` reads fragments and the frozen `[Unreleased]` body
  and nothing else. The section is compiled at release time from `git log <last-tag>..HEAD`
  over the Dependabot merges and entered as one fragment before assembly.
- **`TESTING.md`'s stale-profile warning attributes the false pass to its real cause.** It
  blamed Go's test result cache, which cannot produce the effect: the cache is keyed on the
  content-hashed test binary, every production file is an input under the documented
  `-coverpkg`, and the documented command passes `-count=1` anyway. `patchcov` reads whatever
  `coverage.out` is on disk without checking how it was produced — a profile from before the
  edit, or over a narrower package set, is what passes lines no test ran.
- **Mutation testing is weekly in the two places that still said nightly** (`TESTING.md`'s
  command comment and `scripts/mutation.sh`'s mode help); the workflow runs Mondays at
  04:23 UTC and the same file says weekly twice already.
- **The efficacy history no longer reads as if all three packages fell from ~99%.** Only
  `internal/services` was ever there; `internal/security` (93.4%) and `internal/api` (79.2%)
  rose to their current figures through the hardening pass.

- **`scripts/mutation.sh`'s header comment now describes the script that
  ships.** It documented a `merge-api-shards` subcommand (the real one is
  `merge-shards <base>`), claimed `verify-shards` proves the `internal/api`
  partition alone (it proves every entry in `SHARDED_PKGS`, and
  `internal/services` has been registered there since it was sharded too), and
  named two identifiers that do not exist — `API_SHARD_COUNT` and
  `api_shard_files`, against the live `SHARDED_PKGS` and `shard_files`. It also
  called `internal/api` the largest mutation target; `internal/services` is
  larger (11,555 source lines against 8,099), while `internal/api` remains the
  slowest, which is the fact the budgeting advice actually rests on.
- **`scripts/readmeversion/main_test.go` no longer names a release tag in its
  own doc comment.** It said "currently v1.8.0" while the latest tag is v1.9.2.
  The assertions are version-agnostic, so the version was dropped rather than
  bumped: a comment that has to be updated on every release is the drift this
  test exists to catch.

- **The security concern docs stop misdescribing the code they document.** Accuracy pass from the
  2026-08-28 documentation audit over three of the `docs/security/` concern files. `logging.md`:
  the audit-line example uses the route's real `PUT` method; the `health_data` target enumeration
  names all eleven values instead of seven; the `/lang` + interface-endpoint sentence no longer
  claims nothing is persisted — both persist `users.interface_language` (migration 034) for a
  signed-in owner, and the doc now records the audit exclusion as a declared decision about a
  presentation preference; the always-on diagnostics section counts six owner-naming lines, states
  that the webhook watermark is claimed before the send (so there is no post-success write to
  fail), and discloses the host-only webhook delivery skip/failure lines. `known-disclosures.md`:
  the registration timing equalization runs at bcrypt cost 12, not 10; the register probe's
  follow-up `GET /register/welcome` carries no limiter of its own — the probe is bounded
  transitively through the rate-limited POST that mints each single-use pickup; the
  calendar-feed-polling entry no longer claims every other human-facing health read is audited
  (in-app page reads are not audited at all); the clear-data description enumerates what the wipe
  resets — including the stored timezone — and what deliberately survives it (identity, language,
  TOTP, an OIDC link, onboarding), and the `requires_totp` accepted disclosure moves here from the
  cryptography doc, where it sat under Field-Level Encryption. `cryptography.md`: `ovumcy_csrf` is
  a persistent one-hour cookie refreshed per response, not a browser-session cookie; the
  total-secret-loss paragraph stops claiming an OIDC-only account loses self-service — the
  `(issuer, subject)` link is plaintext and the state cookies are minted under the current key, so
  a fresh provider sign-in still works, and such an account even clears a dead TOTP enrollment
  itself by minting a local password behind a provider step-up; the operator path remains for
  local-auth accounts that can no longer present both operands `/forgot-password` requires — and
  now also names the
  other casualties of a lost key (armed feed MACs, the feed key epoch, the in-memory rate-limit
  identity counters). The same correction lands in `docs/self-hosted.md`'s secret-loss and
  rotation bullets, which asserted the retired claim in the operator runbook. `SECURITY.md`'s
  known-disclosures index gains the moved entry, and the single-document-era "see *Threat Model*
  below" pointer becomes a file link. No behaviour changes.

- **A pointer in `docs/security/known-disclosures.md` named a file that is not in this
  repository.** The sentence about OIDC link-confirm requiring a TOTP code in the same
  submission ended in a parenthesis a reader had nothing to open. It now names the code
  path the sentence is about — `CompleteOIDCLinkConfirmation` in
  `internal/api/handlers_auth_oidc_link_confirm.go`, where the submission is gated on a
  TOTP code whenever the target account's second factor is verifiable. The claim is
  unchanged; only where the reader is sent to check it.

- **`SECURITY.md` now links the auth-cookie scoping note.** The per-concern index under
  `docs/security/` listed every note except `auth-cookie-scoping.md` (SEC-L9), so a reader of the
  policy had no way to reach it; the index gains that one line.

- **The security docs now cite the file each control actually lives in.** Five references still
  pointed at `cmd/ovumcy/main.go` for the CSP, `securityHeadersMiddleware`, CSRF and
  `rateLimitKeyGenerator`, all of which moved out of it — into `cmd/ovumcy/server.go` and
  `cmd/ovumcy/ratelimit.go` — when the server wiring was split out. The controls themselves are
  unchanged; only the paths a reader follows were wrong.
- **The per-account rate-limit list now includes recovery-code redemption.** `POST
  /api/v1/password-resets` has always carried an identity-keyed budget on top of its per-IP row,
  and the recovery-code section pointed at that list for it, but the list itself did not name it.

- Both corrections are held by a test rather than by the next reader. `internal/db` captures one
  database twice, live with a checkpoint forced into the window between the main-file read and the
  sidecar reads, and again with the connection closed: the live capture restores the day written
  before the previous checkpoint and loses the one written after it, the stopped capture restores
  every day, and stopping is the only difference between the two. `scripts/backuprestoredoc`, which
  already executes the runbook's commands, now also holds both ends of the new cross-reference — the
  link in the checklist and the heading it resolves to — so neither can go missing on its own.

- **`TESTING.md`'s sealed-cookie section now matches the codec that ships.** It said eleven AEAD
  purposes and its table omitted `ovumcy_calendar_feed`, which the codec security test has
  exercised since the `.ics` feed landed. The mutation section's claim that `internal/api` is the
  largest package went with it: `internal/services` is bigger and is sharded the same 5 ways. The
  header's test counts were corrected in the same pass; they are dropped entirely by the companion
  entry "`TESTING.md` no longer states test counts that go stale.", which supersedes that half.

- **The security docs now declare both password-reset paths that issue a session without a TOTP
  challenge.** `docs/security/known-disclosures.md` carried no TOTP entry at all, while two routes
  end at the same handler that goes from a completed reset straight to an auth cookie. They are
  written down separately, because they are different decisions. The **operator-forced reset**
  outranks the second factor by design — it is the way back in for an owner whose `users.totp_secret`
  stopped decrypting after a `SECRET_KEY` rotation, which is exactly what the rotation runbooks
  instruct, so making TOTP win would lock out precisely those accounts; on the OIDC link-confirm
  path, which gates a TOTP-enabled target on its own code before either branch, what a reversal
  would drop is the routing to `/reset-password`, not the second factor. The **recovery-code
  reset** is stated as intended design: the code stands in for the second factor and never for the
  password, so the route costs two secrets, and the entry names the property that bounds it — a
  recovery code outlives routine password changes until it is redeemed or regenerated. Both paths
  still bump `auth_session_version` in the same write that rewrites the password hash. The entry
  states the forced reset as the one sanctioned exception to session-issuance parity rather than
  as parity preserved, and `SECURITY.md` now indexes it alongside the other accepted signals.
  Documentation only; no behaviour changes, and the precedence is already pinned by test at the
  service and at both of its callers.

- **OIDC linking and recovery docs now describe what the code actually does.** Five places still
  described the public `/auth/oidc/link-confirm` password-and-TOTP page as the way to finish linking
  an identity: `docs/oidc.md`'s Current Contract summary, `docs/self-hosted.md`'s OIDC section,
  `docs/security/threat-model.md`'s account-takeover mitigation entry, and two supporting mentions in
  `docs/security/auth-policy-and-rate-limits.md` and `docs/security/known-disclosures.md`. That page
  has been retained-but-unreachable since issue #701 — the callback never mints the cookie it reads —
  and the real paths are the authenticated Settings step-up (current password, then a fresh
  `prompt=login` re-authentication at the provider; no separate TOTP challenge) and the operator CLI's
  `link-oidc-identity` for an account with no working sign-in. `README.md`'s own environment-variable
  summary carried the same stale claim in miniature — "falls back to a verified email match" — and is
  now aligned with `docs/oidc.md`'s wording. `docs/oidc.md` and
  `docs/self-hosted.md` also undersold `OIDC_LOGIN_MODE=oidc_only`'s local-recovery guarantee as
  hiding buttons "from the browser UX"; `Login`, `Register`, and `ForgotPassword` actually refuse the
  request server-side before doing anything else. `docs/security/oidc-and-sessions.md`'s "same
  session" language already matched the code (an account-id comparison, not a session-id one) and
  needed no change. No Go changes; the callback, the step-up handlers, and the CLI command behave
  exactly as before.

- **The webhook egress invariants get their own section in `docs/SECURITY_INVARIANTS.md`.** They had
  accreted into a single 2400-character bullet — twice the file's next longest — fusing five separate
  topics: envelope hardening, the private-address block, host-only logging, the write-only URL in
  settings, and the operator CLI's output discipline. Split into flat bullets under
  *Webhook notifications (outbound egress)*, mirroring how the other egress subsystem, the `.ics`
  calendar feed, is already laid out. No invariant changes meaning.

  The same pass carries two claims the public mirror was missing, both already true in code and
  already recorded in `SECURITY.md`: that a saved endpoint must name a host and an in-range port,
  re-checked before delivery, and that the response header block is explicitly capped.

- **The 2FA login-redirect e2e test runs on WebKit again.** Its wait now binds to the click's own
  `POST /api/v1/sessions` and asserts the `303 → /auth/2fa` the server answers, instead of polling
  only for the landed URL. That race was the whole reason the test carried a permanent WebKit skip,
  so the skip is gone and every browser project covers the challenge redirect.

- Layout is now measured in every shipped language, not only in English, so a translation
  longer than its English source can no longer push a control off a 390px screen unnoticed.

- **The button family lost the two utilities that encoded one intent twice.** `btn-warning` and
  `btn-secondary-caution` both described a cautionary secondary action and neither had a call
  site; the destructive role covers what they were for. `btn--disabled`, the only double-dash
  modifier among the sheet's utilities, is now `btn-disabled`.
- **The primary-action contrast gate also holds the role.** It measured each phase tint
  separately, and every tint cleared the bar on its own, so the gate could not see that there
  were four of them; it now reads the painted fill across every phase and fertility value and
  fails on a second one.

- **Four dead style declarations are gone, and the rules that replaced them state their intent.**
  A component rule that ties with a utility on the same element is decided by the bundle's emission
  order, so one of the two has always been dead: the confirmation dialog's message, the segmented
  date field's part labels and the two-factor code field each carried such a pair and now carry one
  declaration that says what it means. The dead compact-step font sizes left by the type scale in the
  calendar day panel and the symptom frequency row are removed as well.

- **The clearance rule under the mobile tab bar that never applied is gone.** It declared
  `padding-bottom` on the element that also carries the `py-8` utility, and the equal-specificity
  tie has always resolved to the utility. The footer clearance is what meets the bar at the
  document bottom; the neighbouring scroll-padding and scroll-margin rules keep working.

- **The pre-032 calendar-feed backfill is now pinned not to run on a refused token.**
  `ResolveFeed` verifies a row minted before migration 032 through bcrypt and, on success,
  writes that row's MAC in from the presented verifier — the only moment the verifier plaintext
  exists. The refusal itself was already covered one layer down, where a tampered token is
  checked against a hash-only row; what nothing observed was the refusal at `ResolveFeed`, which
  is where the backfill sits: every backfill case there presented the correct token, and the
  bad-token sweep runs against an armed post-032 row whose non-empty MAC skips the branch, so a
  write reached without a passing verification would have installed a wrong verifier as the
  row's authenticator with the suite green.
  `TestResolveFeedRefusesAWrongVerifierOnAPre032RowAndWritesNoMAC` presents the real selector
  with a wrong verifier and pins the refusal — no feed, no error — the absent owner-scoped log
  read and the untouched row, with a positive anchor in the same case that serves the correct
  token and compares the stored MAC with the one generation derives. The `SECURITY.md` row for
  the pre-032 fallback cites it. No production change.

- **A cycle-start mark whose implantation policy could not be resolved says so in the audit stream.** The caution stays suppressed — the safe direction for a prediction claim — but the mark's existing `health.cycle_start_mark` line now carries `cycle_start_policy="unresolved"`, so an operator can tell a caution that was not warranted from one that could not be computed.
- **A flash cookie that cannot be written reports it.** The encode and sealed-write failures were both discarded, so a user could be redirected after an error with no explanation and no operator signal. Each now emits one always-on diagnostic naming the reason, never the payload (`docs/security/logging.md`).
- **Four test guards tightened.** The calendar mapper has an exhaustiveness barrier derived from the producer's own sentinels; the template walker sees chained calls (`{{ (dict "k" .V).k }}`) and compares its walk against the embed rather than a hand-maintained floor; the recovery-code regeneration regression compares the owner's whole settings row instead of two sentinel columns; and the settings form helper closes the response body it opens.

- **The cycle-ribbon contrast gate now measures all four phases, and sees `background-image`.**
  `theme-dark-mode.spec.ts` checked only the `menstrual` phase, and `measureGraphicContrast` read
  only `background-color` — the predicted-flow hatch and the start-window gradient are both painted
  as `background-image` on top of the phase fill, so a cell carrying either flag was measured as if
  it did not. The independently recomputed ratios were already correct (light theme: menstrual
  3.39:1 and ovulation 3.61:1 clear the 3:1 floor by design; follicular 1.71:1 and luteal 2.32:1 sit
  under it deliberately, per the sanctioned exception documented beside the phase colours in
  `input.css`; dark theme clears the floor on all four) — the gap was in the tool, not the picture.
  A new `'background'` mode on `measureGraphicContrast` resolves the graphic's own
  `background-color` and `background-image` stops together; the spec now measures a clean (no
  overlay) cell of each phase and keeps the same pass/exempt split the design already carries.

- **Two e2e specs no longer fail in the month-end window.** Both calendar auto-fill tests
  anchored on `today−30` and asserted the four following days in the anchor month's calendar view;
  near month end (the 28th onward in a 31-day month, as early as the 25th in February) those
  neighbors spill into the next month, whose day cells the anchor month's grid does not always
  render (2026-08-28: anchor Jul 29, neighbor Aug 2 — July's Sunday-start grid ends Aug 1, so all
  retries failed identically). The anchor is now the 5th of the month holding `today−30`, which
  keeps the whole auto-fill window mid-month; the window's last day stays at least ~19 days from
  `lastPeriodStart = today−3` and well clear of the predicted next period. The same class lived in
  the baseline-period test in `bugs.spec.ts`, whose `preFertileDay = today+1` crosses into the
  next month at month end and is unrendered whenever the old month's last day closes the grid's
  final week (2026-02-28 was such a Saturday; the next is 2026-10-31) — that assertion now runs in
  `preFertileDay`'s own month view, where the cell always exists and the per-day state is
  unchanged. Test-only
  change.

- **`scripts/readmeversion` now also guards the example env templates against release-tag drift.**
  Nothing re-checked those pins when the release tag moved, which is why they were six minors
  behind. `TestExampleEnvImagePinsMatchReleaseTag` walks every `.env.example` under
  `docs/examples/`, and requires an active `OVUMCY_IMAGE` assignment to name the published image at
  the exact release tag README.md asserts. It fails closed the way the neighbouring guards do: no
  templates, no pin in any of them, or a pin that is not an exact release tag (`:latest`, a digest)
  each fail rather than pass quietly.

- The three suppression signals every projected surface shares (`PredictionsSuppressed`) and the
  first-cycle fertility floor on top of them (`FertilityProjectionSuppressed`) live in one place
  each, instead of being written out once per surface — which is how the floor came to be missing
  from four of them without a failing test. Surfaces built from the dashboard cycle context read the
  decision that context already resolved rather than recombining the signals for themselves, so a
  future disjunct reaches all of them at once.
- The stats service doubles now record the owner id of all four owner-scoped reads (ranged logs, all
  logs, frequency calculation, symptom catalogue) and can serve different data per owner. A
  two-owner stats render pins each seam: every one of the four could previously have been re-pointed
  at a constant owner with the whole stats selection still green.

- The day-service test doubles for the user repository now record the owner id of
  every settings read and write, and the day tests assert that every such call
  targets the acting owner. The id used to be discarded, so a settings write
  aimed at another account — the luteal phase, the period tip, the long-period
  warning — passed the whole day suite unnoticed.

- **`dashboard.owner_only_details` is now defined in all six catalogues.** A shipped template names
  it and no catalogue answered it, which is the same defect as the two-factor hint above — but this
  one is not on screen: its only markup is the branch that runs when the reader is not the account
  owner, and the web tier admits no such reader, since `owner` is the only role it accepts and every
  authenticated route refuses the rest before a handler runs. Nothing renders differently; the key
  is defined because a template names it.
- **`dashboard.fertile_window` was declared twice in every locale file; the first declaration is
  removed.** `json.Unmarshal` into a map keeps the last member of a duplicated name, so the
  surviving declaration is the one the loader was already using and nothing renders differently.
- **Three locale guards.** The suite now fails when a shipped template passes `t` or `tn` a literal
  key that any shipped catalogue cannot answer — a blank value counts as unanswerable, matching the
  lookup the templates go through, and a plural base is answerable only through `tn`, since `t` is a
  flat lookup that resolves no category; when any embedded locale file declares the same key twice, read
  as a JSON token stream rather than unmarshalled into a map, which cannot see a duplicate at all;
  and when the seven validator sentences shared by the onboarding step-2 form and the cycle settings
  card fall out of step, or lose one half, in any locale.

- **`docs/openapi.yaml` can no longer regain the `nullable` keyword.** The nine `nullable: true`
  occurrences were converted to the OpenAPI 3.1 form (`type: [X, "null"]`) previously, but nothing
  stopped the keyword coming back. A new test scans the spec for it and names the file and line;
  it runs automatically wherever `./scripts/...` already runs in CI.

- **The GDPR cross-reference claimed the opposite of what the code does, and its cited test could
  not tell.** The Art. 9(2)(a) row said deployments "rely on operator-captured consent" and that the
  codebase "exposes no third-party transmission" with "no external network calls" as the control.
  Registration refuses an account whose consent field is falsy, and two paths carry health-derived
  data off the instance: webhook reminders to the endpoint an owner configured, and the read-only
  `.ics` calendar feed, whose subscriber is commonly a third-party calendar service receiving that
  owner's predicted days. The row now states the consent gate and both egress paths with the
  containment each has, and cites tests that can observe them. The security-document pair guard
  grows past the webhook rows to hold it there: consent capture, the egress set and the
  privacy-by-default settings are read out of the code that decides them — the feed's from the
  route that mounts it — and the table is held to what they say, in both directions, so naming one
  egress path no longer passes for naming them all.
- **The one premise behind an accepted schema divergence is now asserted.** Migration 035 moves the
  `auto_period_fill` column default on PostgreSQL; SQLite cannot restate a default without
  rebuilding the account table, which is the parent of every foreign key in the schema, so the old
  literal stays and is harmless only because every insert path names the column. A test inserts a
  row that omits it on each engine and pins what each stores, so a change that starts relying on
  the column default fails on a test instead of on an owner's data.
- **The browser suite arms automatic period fill instead of inheriting it.** Nearly every
  Playwright scenario reaches the dashboard expecting the onboarding period window to hold days,
  and it held them only because the default was on — a dependency no spec stated. The two shared
  onboarding helpers now turn the toggle on deliberately and assert the resulting state, so the
  arming is visible where the days come from and the helper still fails loudly if the control
  disappears. A spec whose subject IS the default drives onboarding itself. In the same pass,
  clear-data's browser regression was inverted to match the product: it arms auto-fill before the
  wipe and asserts it off afterwards, which is what makes the reset observable at all.

- **The three writers of the derived luteal-phase cache now share one rule.** A day save, a bulk
  restore and the new boot recompute each decided for themselves what the cached estimate should be
  — run the inference, fall back to the default — and a disagreement between the stored values and
  the inference that produced them is exactly the defect the recompute exists to repair. All three
  now call one unexported helper, so no two of them can drift apart again. The boot pass resolves
  each owner's calendar day through the same `resolveOwnerLocation` the webhook pass and the
  calendar feed use, rather than adding a second owner-timezone resolver.

- **The migration replay path is swept rather than sampled.** A new `internal/db` test deletes
  each `schema_migrations` row in turn from a fully migrated database, reopens it through the real
  boot path, and requires that the reopen cost neither a column nor a stored value — passing on
  either outcome, a completed boot or a refusal by name, since both are non-destructive. Two cases
  spell out the failures behind it: removing only the row for 003 on a current database, and
  losing the whole ledger, each with a seeded day carrying a value in every column added after
  003. It is deliberately narrower than "the schema is unchanged", and the file says so:
  indexes are out of scope, because migrations 001 and 024 both end with
  `CREATE INDEX IF NOT EXISTS idx_daily_logs_date`, which migration 025 later drops, so replaying
  either recreates a redundant index — write amplification, never a column or a row. Because the
  sweep runs on SQLite, where the rebuild pattern lives, a second test asserts the premise that
  makes that enough: the Postgres tree drops no table at all today, and a rebuild landing there
  later fails until the sweep is extended to it.

  The shapes the embedded tree does not contain are covered by synthetic migrations run through
  the same code path: both SQLite rebuild idioms, each in a narrowing and a preserving variant so
  the refusal cannot be credited to a check that refuses everything; a table removal with the
  marker, without it, with a marker naming a different table, and with the marker words buried in
  prose; an explicit column drop and a column rename, both of which must still apply; and a
  rebuild that renames one column while quietly losing another, which must still be refused,
  naming the column that was really lost and not the renamed one.

- **An account with a role this product does not have is refused where it is written.** Ovumcy is
  owner-role-only — every account is the sole owner of its own data, and there is no viewer or
  partner role — but the `users` column has carried `CHECK (role IN ('owner', 'partner'))` since
  the initial schema on both engines, while nothing else in the tree mentions `partner`: no
  constant, no handler, no template, no locale key. The database would have accepted such an
  account and only the login policy would have turned it away afterwards. Both account-creating
  repository methods now reject any role other than owner before the insert, so the refused
  account leaves no row and no seeded symptom behind; an unset role still means "the column
  default", which is written out as owner rather than left to the tag and the migration agreeing.
  The constraint itself is left alone on purpose and the reason is recorded next to the check:
  SQLite has no `ALTER` for a `CHECK`, so narrowing it means rebuilding `users` — and with
  `foreign_keys=ON` a dropped table fires every `ON DELETE CASCADE` hanging off it, which would
  make a migration that tightened one unused constant delete every day log on the instance.

- The CSV header row is handed out as a copy instead of as a shared package-level slice, and the
  export panel's date comparison reads the same operands in both of its arms.

- **A migration that adds a unique index stops rather than making room.** On a database that
  already holds rows the new index cannot cover, the migration refuses and names every
  conflicting group — the key value and how many rows share it — instead of creating the index.
  It never deletes, merges or rewrites a row: this instance stores one person's health history,
  and a schema change is not consent to lose part of it. Nothing is executed, the migration's
  transaction is rolled back, and the message says how to resolve it with the instance down —
  the refusal is also what stops the server, so there is no running application to resolve it
  in. The check reads the key expressions out of the `CREATE UNIQUE INDEX`
  statement itself, so it covers the next unique index as well as this one, and it excludes
  `NULL` keys because both engines treat those as distinct in a unique index. A statement it
  cannot read — a partial index, or anything trailing the key list — is left to the engine,
  which still refuses a genuine conflict with its own message.

  Rolling this migration back is `DROP INDEX idx_symptom_types_user_name_unique`. It writes no
  data at all, so dropping the index restores the previous state exactly.

- **A migration can no longer cut its own prose in half.** The runner splits statements on every
  `;` without stripping comments, so a semicolon in the middle of a comment line turns the rest
  of that sentence into the next statement, and the engine answers with a syntax error naming
  English. Every migration file opens with a long prose header, and the rule against it lived
  only in the closing paragraph of the files that happened to repeat it. A sweep over both
  dialect trees now enforces it, narrowed to the form that actually breaks: a semicolon that ends
  a comment line is harmless and three shipped files rely on that, so only a semicolon with text
  after it on the same line is refused.

- **The duplicate-name class is held by a barrier test rather than by timing.** Two goroutines
  are released together and rendezvous on the return of the catalogue read, so both hold a
  decision made from the same snapshot before either writes — starting two goroutines together
  is not enough, since one usually finishes its insert before the other reads, and the guard
  then passes on a tree with no constraint at all. One case covers the create path and one the
  built-in seeding path; a third pins what the index does and does not normalize, and a fourth
  pins that the name stays per owner and stays taken across archival.

- **A template barrier fails on human-readable copy typed into the markup.** It reports an `alt`,
  `title`, `placeholder`, `aria-label`, `aria-description` or `aria-placeholder` value — the closed
  set of attributes a person reads, named one by one rather than matched by shape, since `class`,
  `data-*`, `id` and `pattern` may hold English forever — and any visible text node, whose text
  carries letters and no template action — the one class neither existing locale sweep can see, since both start
  from a `t`/`tn` call and this copy never went through one. The reader works on the source regions
  the template parser attributed to text with every action masked out, so a value assembled from
  the catalogue leaves no letters behind to find. Two exceptions are declared with their reasons:
  the recovery-code mask on the forgot-password form, and the `°C` / `°F` unit symbols, which read
  the same in all six languages and are rendered beside their translated names.

- **`scripts/archcheck` enforces the architecture contract against the tree.** Two invariants
  that were checked only as text — nothing under `internal/api` or `internal/apideps` imports
  persistence, and no schema is migrated at runtime — are now decided by parsing the module and,
  where the answer needs it, type-checking it: a `gorm.DB` reached through a wrapper is caught,
  and a same-named method that gorm did not declare is not. It runs in CI, refuses a commit
  through `.githooks/pre-commit`, and reports nothing about a tree it could not read rather than
  reporting it clean. Adds `golang.org/x/tools` as a direct dependency.

- **staticcheck, golangci-lint and gosec are built against `golang.org/x/tools` v0.51.0 in CI.** Go
  1.27.2 writes export data v5, which the x/tools their latest releases pin cannot read; a shared
  `go-install-on-xtools` action builds each pinned tool in a throwaway module that lifts x/tools,
  and the cache key and the restored-binary checks name that version too.
- **`source-map-js` lifted to 1.2.2 in the lockfile**, closing CVE-2026-93749 (HIGH). The package
  is a transitive dev-only dependency of the CSS build and of `jsdom`, not part of any shipped
  artifact. It lands with the Go bump because the required `trivy-fs` check scans both
  `go.mod` and `package-lock.json`: neither fix passes it alone.

- **The backup and restore runbook is now executed, not just written.** Both documented procedures —
  the Postgres one (`pg_dump`, then a schema drop and a `psql -v ON_ERROR_STOP=1` replay) and the
  SQLite named-volume one (`tar czf` through Alpine, then `docker volume rm`/`create` and `tar xzf`)
  — existed only as prose: nothing in the repository ran a single one of those commands, so an
  incident would have been the first time either ran end to end. `scripts/backuprestoredoc` reads
  them out of `docs/self-hosted.md` and runs them verbatim against ephemeral resources, substituting
  only what would otherwise reach a real deployment: the compose transport becomes a direct
  `docker exec` against a throwaway container, and the data volume name becomes a throwaway volume.
  That second substitution is safety, not convenience — the documented restore begins by deleting
  the volume — so an executed command is refused outright if either literal survived it. The two
  `docker compose` lifecycle lines are asserted present and then skipped: the guard never starts the
  application, it writes the volume and reads it back itself.

  Each half seeds a generation, backs it up, deliberately drifts the database — a row written, a row
  changed, a row deleted — restores, and only then compares, so a restore that moved nothing cannot
  pass; both then read the owner and the day logs back through the repositories. The Postgres half
  closes with the runbook's own failure row as a counterfactual. The SQLite half writes its fixture
  with the connection held open, which puts the rows in `ovumcy.db-wal` rather than in the database
  file, and so checks the runbook's promise that the whole-volume archive captures `ovumcy.db`, its
  `-wal` and its `-shm` together — an archive missing the sidecars restores the clean, empty instance
  the Post-Restore Verification section warns about, and nothing checked that before.

  CI's relevance filter learns the one consequence: a diff that touches only the runbook now forces
  the Go lanes, which would otherwise be cleared as documentation — 10 of the 22 commits that touched
  it since June carried nothing else. The browser lanes are deliberately not forced with them: the
  guard does not ride there.

- **The per-operation status guard now judges redirects instead of excluding them.**
  `TestOpenAPIOperationsDeclareEveryStatusTheirOwnHandlerChainCanEmit` listed `303` as
  cross-cutting, on the argument that any handler may reach the shared content-negotiation helper
  regardless of its own logic. That is true of the helper and false of a handler that falls through
  to `c.Redirect()` with nothing answering the JSON caller first — which is how both 2FA mutations
  redirected an `Accept: application/json` client to a page, undeclared, on a green `main`. The
  exclusion is gone and the surface question is answered where it is asked: the walk now reads an
  `if acceptsJSON(c) { … return … }` as ending the JSON caller's request, so later statements in
  that block are the HTML surface, and reads `switch responseFormat(c)` the same way, walking only
  its JSON arm. Measured on `10d4b7c8`, before either fix: dropping the exclusion alone reddened 20
  operations, of which the two 2FA ones were the only real findings; with both narrowings, none.
  - Two things stop the quieter guard from being a blind one. A new test,
    `TestStatusReachTellsARedirectJSONCallersGetFromOneTheyDoNot`, runs the analyser over source
    written for it — a bare fall-through, an `acceptsJSON` early return, the shared switch written
    both qualified and not, that switch with an initializer, and an `acceptsJSON` arm that does
    *not* return — and each case also asserts a status the walk must still find, so a run that
    reached nothing cannot pass as a run that found nothing. The switch narrowing matches its JSON
    arm by the constant's NAME rather than one spelling of it, and walks the initializer, because
    it is the one narrowing here whose misses hide a JSON status instead of an HTML one. The sweep
    itself now fails when it judged fewer `/api/v1` operations than `docs/openapi.yaml` declares —
    a floor read out of the spec rather than written beside it, because a route table that failed
    to load and a clean tree print the same nothing.

- **`THIRD_PARTY_LICENSES.md` now delivers the completeness its scope sentence promises.** The
  file claimed to list the third-party software redistributed in the built application while
  inventorying only htmx and dismissing Tailwind as "not redistributed" — the shipped
  `tailwind.css` is git-tracked, embedded via `go:embed` and served with Tailwind's MIT banner
  preserved on line 1, and ~45 Go packages under MIT/BSD-3-Clause/Apache-2.0 notice obligations
  were statically linked into the released binary with no attribution anywhere. The file now
  carries a generated Go-module table (one row per license-bearing directory of the binary's
  build graph, named by the deepest common ancestor of the packages sharing that license file:
  package, license, link to the pinned version's license text), produced by the new
  `scripts/licensesdoc` from a `go-licenses` report and verified by a new `test-go-analysis` CI
  step that regenerates the report and refuses a stale committed block — so a dependency bump
  cannot silently un-true the inventory again. The Tailwind entry now states the compiled file
  IS shipped and that its preserved `/*! tailwindcss … MIT License */` banner is the notice, and
  a runtime-image section attributes what the scratch image copies in: the Mozilla CA
  certificate bundle (MPL-2.0), the IANA tz database (public domain), and the two account files
  built on `alpine-baselayout` (GPL-2.0-only) bases. A refuse-by-default override table
  hand-corrects the rows `go-licenses` gets wrong — `modernc.org/mathutil` (unclassifiable
  standard BSD-3-Clause text), `modernc.org/libc` (misreported as MIT via a notices file) and
  `modernc.org/memory` (linked to the Go project's LICENSE-GO instead of the module's own
  grant) — with each override pinned twice, to the exact reported row and to the version go.mod
  requires (the second pin is what makes a bump loud even for a row reported as
  version-free `Unknown`); the generator also refuses any Unknown or non-https license link
  rather than committing a dead or machine-local one. The report is taken over the shipped
  binary's build graph (`GOOS=linux`, `CGO_ENABLED=0`, the Dockerfile's build environment) —
  the graph is GOOS-dependent, so an unpinned host graph differs by a row. An inventory-only
  edit forces the Go lanes on, so a hand edit cannot dodge the gate. No behaviour changes.

- **The layer directions and the no-inline-suppression rule are checked mechanically instead of
  by eye.** `scripts/archcheck` gained a `layer-imports` rule: `internal/services` and
  `internal/db` may not import transport, `internal/db` may not import `internal/services`, and
  `internal/models` may not import any other package in the module. It is stated as denials rather
  than as an allow-list of what each layer may reach, so a new legitimate edge — `internal/i18n`
  from a service, say — is not refused, and it reads every file in the tree rather than the
  packages the current platform builds, so a file behind a build tag is covered too. Test files
  stay outside it, because a fixture proves a repository against the service that owns it and the
  reverse. Separately, `nolintlint` is enabled in `.golangci.yml`, which fails the lint job on a
  `//nolint` directive that is bare, unexplained, or unused for a linter this config runs; two
  directives naming `gosec` — a linter golangci-lint does not run here, so they suppressed nothing
  on any path — were removed by hand, since the linter does not see that shape. No production
  behaviour changes.

The local OIDC provider used by the browser tests now serves a UserInfo
endpoint, and `OIDC_TEST_PROVIDER_EMAIL_CLAIMS=userinfo` makes it behave like
Authelia: the ID token carries no email and UserInfo supplies it. A new browser
test walks a first-time SSO sign-in through that mode and checks that it ends
signed in with no email or auth parameter in any app URL.

- **README asks for a star and gains a "Support the Project" section.** A star prompt under the
  introduction and after Quick Start, plus a section listing ways to help (star, issues,
  translations, word of mouth, a contact address for donations). No behaviour changes.

- **Transport-layer duplication swept out of `internal/api`, with three sweeps to
  keep it out.** A tagless switch that classifies domain sentinels may no longer
  carry a default arm identical to one of its own cases (16 sites folded, from
  `mapDayDeleteError` — whose entire switch had one case byte-identical to its
  default — to the auth, symptom, export, recovery and password mappers); a call
  site that wants its own fallback for a missing translation now asks the new
  two-value `lookupMessage` instead of comparing `translateMessage`'s result to
  the key it just passed in (19 sites, six of which spelled the key a second
  time as a literal); and the shared test helpers reach the app secret and the
  auth-cookie name through their constants rather than through a second copy of
  the value. Four duplicated or vacuous test cases were removed or re-pointed:
  the privacy-route test now builds its app with the shared recipe instead of a
  divergent one, the partial-template test asserts the one define `base.html`
  actually declares, the onboarding picker test derives its 24 expected strings
  and its locale list from the i18n catalogue, and the settings-symptoms HTMX
  test now reads the mutated row back out of the rerendered section. No rendered
  string, status code or error key changes.

- **Localized date names moved to one table per language.** The five parallel month/weekday maps in
  the services layer became a single fixed-size entry per language, and a new sweep requires a
  complete entry for every locale the i18n manager reports — a seventh locale can no longer be added
  and silently render English dates on the calendar header, the day label and the dashboard. Every
  rendered date is byte-identical to before.
- **Dead code retired in the services layer.** The unused singular `AttemptLimiter` methods are gone
  and the lockout tests now drive the plural methods production actually calls; the Russian plural
  rule has one home again (`i18n.PluralCategory`); the unreachable nil-user guards in the day-feedback
  policy and a French count-word branch with identical arms were removed. `User.LongPeriodWarnedAt`
  is now `User.LongPeriodWarningCycleStart`, the name its column and its date-only type always had.
  No stored data, API payload or rendered string changes.

- **A day's symptoms are counted once at every read site.** Three of the six
  surfaces that count symptoms per day walked the stored id slice through a
  dedup helper and three walked it raw, so a slice repeating an id would have
  counted that day twice — a phase percentage above 100, a skewed frequency list
  and a skewed picker ranking. Validation already deduplicates before persisting,
  so nothing rendered wrong; the read side no longer depends on that. There is
  now one dedup helper, a second that filters unknown ids on top of it, and a
  sweep that fails on the next bare loop the moment it is written. The sweep
  found a seventh site the audit had not, and it is exempted by name with its
  reason: the export flag builder writes booleans and a name set, where a repeat
  changes nothing. No rendered number changes.
- **The fuzz harness is held to one story across its three copies.** The native
  `go test -fuzz` targets, the ClusterFuzzLite harness behind the `gofuzz` build
  tag, and the build script's target list had to agree by hand, and the tagged
  file is the one file the default build, vet, the linters and coverage never
  see. A barrier now reads all three and fails when their target sets, seed
  corpora or oracle bodies stop agreeing.
- **DayHasData and the auto-fill candidate check share one field list.** The two
  predicates test the same eight day fields with opposite returns, and nothing
  held them together: a ninth field wired into one alone would have let the
  auto-fill cleanup delete a day carrying that manual signal. A barrier reads
  both functions and fails on the asymmetry, with Flow, CycleStart and
  IsUncertain carried as the documented exceptions they always were.
- **The stats page's sample-size thresholds say what each governs.** The pattern
  minimum and the trend-reliability threshold are both three and answer different
  questions about different quantities, so they stay two names with two doc
  comments, and each surface is now pinned at its own boundary. The
  historical-phase preference likewise has its scope written down: the drawn
  markers on the calendar and the cycle stack, not the prose that groups moods
  and symptoms by inferred phase.
- **Repeated mutation kills folded into the tables that own them.** Five
  round-*N* files restated assertions their coverage tables already made, with
  line-number comments pointing at code that has since moved. Every case they
  contributed uniquely moved into the surviving table first. One of those tables
  turned out to assert Russian plurals with a word that is identical in the "one"
  and "many" forms, so no teen case in it could fail; it now uses distinct
  sentinels. The four symptom-ranking tests named for four boundaries exercised
  one identical state, and are one table whose rows sit where their names say —
  three of the four are new coverage.

- **The manual cycle-start control and its confirmation policy are one
  component.** The form and the hidden `[data-cycle-start-policy]` node beside it
  were copied four times — three branches of the calendar day panel and the
  dashboard journal — and the eleven attributes on that node are the whole
  contract with the browser script that stands between a mis-tap and replacing a
  recorded cycle start. All four copies were complete, so nothing rendered wrong;
  what the duplication cost was that adding a twelfth attribute, renaming one or
  changing a message key meant four edits, and no reader could tell a deliberate
  branch difference from an omission. The four call sites now go through one
  `cycle_start_form` define, with the three differences that track the branch
  each serves — the htmx target, the wrapper class and whether the button paints
  as the primary action — passed as parameters. A new barrier derives the
  expected attribute set from the confirm script itself and fails on any policy
  node that renders without all of it, so a partial copy can no longer look like
  a deliberate one. The rendered markup is unchanged apart from indentation and,
  on the dashboard, the order in which the form's own attributes appear.

- **One URL-hostname redaction helper, not two.** The settings display carried its own byte-identical
  copy of the "hostname and nothing else" rule that the notify pass and the CLI print through; a
  hardening of what is safe to show now reaches every surface at once.
- **The notify pass decides once per owner.** Its idempotency counter was derived by re-running the
  whole reminder decision with the watermarks cleared, which rebuilt the owner's cycle statistics
  from their entire logged history a second time; one traversal now yields both the due set and the
  count.
- **A locale sweep for the day-field label functions.** Every value of every day-field enum is fed
  through its translation-key function and required to resolve in all six catalogues, closing the
  gap the existing map-based sweep could not cover: those keys come from `switch` statements, so a
  typo reached every language as a raw key on screen.

- The stats page's view model no longer carries predicted next-period, ovulation or fertile-window
  values for an owner whose predictions are suppressed. Suppression was a template obligation — one
  boolean beside the full statistics it was meant to hide — so a new partial or a serialization of
  the struct could publish an estimate the page itself refuses to show.

- **Owner isolation is now proved under concurrency, not only statically.** On an instance hosting
  two independent owners, twelve workers across the two sessions write and read the same dates at
  once; every response is checked for the other owner's rows, identity and cookies — in the body
  and in `Set-Cookie` — the persisted rows are re-read afterwards, and a positive anchor counts the
  own-data reads so a run that returned nothing cannot pass as "no leakage". Green on Windows and
  under the race detector on Linux.

- **A required CI check can no longer disappear behind a `t.Skipf`.** `bash`/`docker`/`git`
  absence and a missing tz database were skipped identically to an operational failure — bash
  answering garbage, docker's daemon not responding, git exiting non-zero, zoneinfo parsing wrong —
  so a misconfigured runner reported the same quiet green as a developer machine that never
  installed the tool. `internal/testenv` now gives every site one fail-closed switch per resource
  (`OVUMCY_REQUIRE_BASH`/`DOCKER`/`GIT`/`TZDATA`), armed on the lane that owns each check in
  `ci.yml`: absence skips only where the lane never declared the resource mandatory, and an
  operational error always fails, on every machine. `dashboard_today_request_timezone_regression_test.go`'s
  previously unconditional `t.Skip` is now the same conditional absence check as the rest.

- **Four documents restate the webhook egress contract, and nothing failed when a change moved
  three.** A classifier or delivery-envelope change touches `SECURITY.md`, its public mirror
  `docs/SECURITY_INVARIANTS.md`, `docs/notifications.md` and `docs/self-hosted.md`; one change
  moved the matrix and left the mirror stale for a day, and only a follow-up caught it. The new
  `scripts/webhookdocs` guard checks what each pair genuinely owes rather than demanding the four
  read alike: every row of the matrix's webhook section is either mirrored in the public document
  or explicitly declared matrix-only with a reason, every test a row cites is defined in a file
  that row links, and the two operator documents state the private-address gate together with the
  default read out of the code that parses it. The row set is swept out of the document, so a row
  added later cannot join without a verdict. The obvious shape — every address form named in every
  document — was measured first and refused: the four name 1, 1, 4 and 1 of the six IPv6
  transition prefixes, because they are written for different readers.

Cycle-phase colours gain a `--phase-*` token family that reuses the calendar's
own channels, replacing four one-off values the dashboard ring carried alone —
"menstrual" was a different colour on Today than on the calendar.

- **The Docker Hub mirror waits out Docker Hub's rate limit instead of failing the publish on it.**
  Two pushes to `main` on 2026-09-29 went red in `publish-image` because a `cosign copy` in the
  mirror step was answered with `429 Too Many Requests` during a merge burst, after the mirror
  login had succeeded and, in one of them, after the same step's pushes to Docker Hub had been
  accepted. Every copy in that step now asks again on that one verdict, up to four attempts with a
  growing pause, and on nothing else: any other refusal still fails the step on its first attempt,
  and a limit still standing after the last attempt fails it too, so the mirror is never reported
  published with a copy missing. The retry is matched on the registry's own wording rather than on a
  bare `429`, which a digest in an unrelated error can contain. `scripts/releasegate` runs the
  step's real script against a shimmed `cosign` and `sleep` to hold it to both halves and to the
  growing pause between attempts, and holds the shipped budget to more than one attempt with a
  non-zero pause.

- **The Docker Hub mirror's aliases are copied from GHCR, so they no longer spend Docker Hub's pull
  quota.** Each alias used to be copied from the mirror's own digest, and one `cosign copy` makes up
  to about fourteen manifest requests to its source: the index, each platform, and a signature,
  attestation and SBOM tag probe per entity, most of which answer 404. The aliases now read the
  same digest from GHCR, whose bytes the first copy has already landed on Docker Hub, so Docker Hub
  receives only the manifest under each tag and the HEADs before it, which its quota does not
  count. The signing call and the signature read-back, the step's remaining Docker Hub reads, now
  wait out a rate limit under the same bounded budget as the copies; a throttled read-back used to
  be reported as a signature that did not match this repository's signer. A new informational step
  after the mirror login reports whether the Docker config holds an entry for Docker Hub and what
  Docker Hub answers for the mirror's repository anonymously and with that login: the limit, what
  remains, and the source it charges. The step never fails the job, and the credential reaches
  `curl` on stdin only. `scripts/publishorder` and `scripts/releasegate` pin the new alias source,
  the retry on each call, and the step's place in the mirror's sequence.

- **A release tag now needs proof that a Go job ran, and CI runs the Go lanes for the documents its tests read.** The tag gate used to read `test`, `race` and `e2e` from the merge-queue run, which reports all three green when it skips every lane beneath them for a tree its pull request already tested, so a tag could be signed with no Go or race job executed in any run it inspected. It now also requires `test-go-shard (1)` and `race-db (1)` to have concluded `success` in a run of ci.yml that the merge queue or a push to `main` started, on the tagged commit or on an ancestor; a skipped or missing job is not proof. Only runs of ci.yml itself count — matched by the workflow file, never a display name — and only those two events, so a job of the same name in another workflow file, in a run started by hand or by a release, or in a pull request run (which executes the copy of the workflow on the pull request's own branch, even for the pull request the commit came from) is not read; the same holds for the ancestors' runs. A tag whose queue run skipped its lanes for a tree its pull request had already tested therefore has no run of its own to point at, and is accepted only through an ancestor. A job still unproven on the tagged commit is taken from the nearest first-parent ancestor, within twenty commits, that differs from the tagged commit in nothing ci.yml's own change detection would run that lane for. The gate cuts the detect step out of the checked-out ci.yml and runs it over the paths that differ between the ancestor and the tagged commit, compared directly, and reads its `run_race` or `run_unit` answer, so embedded SQL, templates and locale files, readmes and changelogs are treated exactly as the lane filter treats them and the gate keeps no list of paths of its own. The ancestor is judged as the tagged commit is, by a success on it in a run that counts; a failure on it refuses, and so does a change the filter runs the lane for, a path it cannot list, an answer it does not give, a read of the ancestor's evidence that fails, or a walk past the bound. A commit in between with no run on record costs nothing. The proof is read as a job of that run, from the run's own list of jobs, and never from the commit's check runs: a token holding `checks: write` can add a check run of any name and conclusion to a commit, so a forged `test-go-shard (1)` could have stood in for a job that never ran, and the gate no longer reads the Checks API at all (the three gates and the image lanes are read from the runs' jobs too). Where a job was run again, only its newest attempt speaks for it. The gate's job reads no pull request, and its read scopes are now `actions` and `contents`, which a test pins; the `checks` scope is gone from it and from the caller's ceiling. Separately, a pull request touching only a document a Go test reads (the OpenAPI file, the security invariants and their pages, the data-protection and notification operator pages, the example compose files, the readme, the changelog and its fragments, the security policy, the fuzz build script and nothing else in the fuzz sandbox directory) no longer clears the Go lanes as documentation-only, and a new guard derives those documents from the tests, directory and glob reads included, so one added later is covered too.

- **The release gates refuse the runner keys that would let them fail open.** A step's
  `continue-on-error:` or `if:`, or a job's `continue-on-error:`, lets the runner pass a gate whose
  script refused — which no test that runs the script can see. The tag-ancestry gate and the publish
  job's steps from the push through the anonymous read-back of the public tags are now held free
  of those keys by one shared reader. The
  ancestry fixture now mirrors the runner's checkout of a tag (detached, no local `main`), and an
  accepted tag is recognised by its own acceptance line rather than by text the refusal also
  contains.

- **The Docker Hub mirror steps are held free of the runner keys that let a failure through.** The
  mirror's login, its signed copy and its anonymous read-back may carry no `continue-on-error:`,
  and no `if:` other than the one that turns the mirror on; a mutant adding the former to the copy
  step fails the check.

- **The release tag's ancestry check is now exercised against a real repository.** `verify-release-tag`
  refuses a tag whose commit `main` does not contain, and until now no test ran that step. The
  release-gate suite now extracts the step's own script and runs it inside a real clone of a
  fixture origin: tags on or behind `main`'s tip pass, lightweight or annotated, and a tag on a
  branch `main` never merged is refused — also when `main`'s history mentions that commit in a
  message, and when the checkout's `origin/main` was left pointing at it. The job's checkout is
  pinned to a full history, which the check cannot answer without.

- Pinned the rolled-window "fertile" reading of the stats overview as a backstop: since the
  fertility half is withheld from the day the projected period is due, no mode reaches it, and a
  sweep over regular and irregular range-mode histories now proves that.

- **A refused session's cookie retraction is tested on every refusal arm, and the account-binding
  sweep resolves the tests it relies on.** The expired-token, sealed-decode and invalid-claims
  refusals now each prove the session-only handoffs leave with the session while the reset-password
  and TOTP-pending cookies stay. The binding sweep type-checks the package with its tests and
  refuses a route whose named cross-account test is not a declared `Test` function. The two unbound
  step-up starts now name the reason that holds: the identity link demands the current password, and
  the local-password setup writes only after a fresh provider sign-in as the account's own identity.

- **`internal/db` runs under the race detector in four cells of its own (`race-db`).** It was
  849.550 s of `race-rest`'s 866.827 s package sweep and the critical path of every full
  pull-request and merge-queue run; `scripts/racepartition` splits it by test name as it already
  splits `internal/services`, and each cell refuses a listing too short to give it a test. The
  required `race` gate now judges `race-db` too. `race-services` and the browser `e2e-shard` lanes
  each gain a fourth cell.
- **The weekly mutation run can finish.** Its `internal/services` cells were dying when a mutant
  turned a loop's `x++` into `x--` and the loop's appends ran the runner out of memory; each
  mutation cell now caps a process's address space, so that test binary fails instead. gremlins'
  coverage pass gets a 30-minute test timeout in place of Go's 10-minute default, which
  `internal/api` was reaching. Files are dealt to shards by estimated mutation weight
  (`scripts/mutationpartition`) rather than round-robin by count, across 14 `internal/api` and
  10 `internal/services` shards, with at most 11 cells running at once.

- **The document-reader CI guard no longer fails on a release commit.** Its check that `changelog.d` is still read as a directory looked for a fragment file under it, and release assembly leaves only `.gitkeep` there. It now checks the bare-name read itself and expands that one directory on its own, so it holds with or without fragments and still fails when either half of the derivation breaks.

- **A refused page no longer pays for the whole message catalogue.** Every page, a rate-limited
  refusal of the language switch included, copied both the default and the request's message
  catalogue into a fresh map before rendering. The merged catalogue for each language is now built
  once at start-up and shared, so a refusal costs what the page costs and no more.

- **`reset-password --full` is tested against an OIDC link racing it on Postgres, and clears the
  second factors in its one UPDATE.** A link queued on the account ahead of the reset commits while
  the reset waits, and the reset still deletes the identity it added. The TOTP and recovery-code
  clear now rides the reset's own UPDATE instead of a second statement in the same transaction.

### Dependencies

- Raised the `brace-expansion` override from `^5.0.8` to `^5.0.11` (lockfile
  now at 5.0.12), clearing CVE-2026-102276 and CVE-2026-102278 in the frontend
  build toolchain. Development dependency only; the shipped binary and runtime
  image are unaffected.

- **`github.com/go-jose/go-jose/v4` 4.1.4 → 4.1.6** (indirect, through `go-oidc`): fixes
  CVE-2026-116587, a denial of service when parsing a JWK set that contains RSA keys. The JWK set
  comes from the operator-configured OIDC provider.

- Bumped the npm-minor-patch group (5 dev-dependency updates, including htmx.org 2.0.11) and regenerated the committed `htmx.min.js`.

- Overrode `@parcel/watcher` (pulled in at 2.5.1 by `@tailwindcss/cli`) to `^2.6.0`, which
  matches globs with `picomatch` instead of `micromatch`. This drops `braces@3.0.3` and its
  unfixed CVE-2026-93687 (stack-exhaustion DoS, no patched release) from the frontend build
  toolchain. Development dependency only; the shipped binary and runtime image are unaffected.

- `@humanfs/node` 0.16.7 → 0.16.8, closing GHSA-p498-v437-472g (moderate): its recursive copy followed symlinked files and copied data from outside the source tree. The package is a dev-scope transitive dependency of `eslint` and is never built into the runtime image. `eslint`'s own range already admits the patched version, so the lockfile carries the fix and no manifest pin was added.

- **Everything that moved between v1.9.2 and v2.0.0, in one place.** The advisory-driven bumps
  have their own entries (the Go toolchain under *Security*; `go-jose`, `brace-expansion`,
  `@parcel/watcher`, `minimatch` and `@humanfs/node` in this section). The rest came in as routine
  Dependabot group updates; the number of those pull requests says nothing about how many
  advisories were fixed.
  - **Runtime — what ships in the image.** Go 1.26.5 → 1.27.3 (`go.mod` and the
    `golang:1.27.3-alpine3.24` builder stage). Direct modules: `gofiber/fiber/v3` 3.4.0 → 3.6.1
    (with `valyala/fasthttp` now a direct dependency at 1.77.0), `coreos/go-oidc/v3` 3.20.0 →
    3.21.0, `jackc/pgx/v5` 5.10.0 → 5.11.0, `gorm.io/driver/postgres` 1.6.0 → 1.6.3,
    `golang.org/x/crypto` 0.54.0 → 0.58.0, `x/net` 0.57.0 → 0.62.0, `x/oauth2` 0.36.0 → 0.37.0,
    `x/sys` 0.47.0 → 0.49.0; indirect `go-jose/v4` 4.1.4 → 4.1.6, `klauspost/compress` 1.19.0 →
    1.20.3, `x/text` 0.40.0 → 0.43.0. The `runtime-assets` stage moves from Alpine 3.24.1 to
    3.24.3 with `tzdata` and `ca-certificates` now version-pinned; the final stage is still
    `scratch`. `THIRD_PARTY_LICENSES.md` follows every module move.
  - **Example deployments.** The Caddy reverse-proxy examples move from `caddy:2.11.4-alpine` to
    `caddy:2.11.7-alpine`.
  - **Frontend build and test toolchain — development only, never in the image.**
    `htmx.org` 2.0.10 → 2.0.11 (the committed `htmx.min.js` is regenerated from it),
    `@playwright/test` 1.61.1 → 1.64.1, `eslint` 10.7.0 → 10.13.0, `jsdom` 29.1.1 → 30.1.3,
    `otplib` 13.4.1 → 13.5.0, `globals` 17.7.0 → 17.13.0; `typescript`, `typescript-eslint` and
    `@types/node` are new, for the type check over the e2e suite.
  - **CI tooling and actions.** `golang.org/x/tools` v0.52.0 is now a module dependency, and the
    analyzers (staticcheck v0.8.1, golangci-lint v2.13.2, gosec v2.29.0) are built against it
    because Go 1.27 export data needs it. Pinned actions moved by minor and patch releases only:
    `actions/checkout`, `setup-node`, `upload-artifact`, `download-artifact`,
    `attest-build-provenance` (4.1.1 → 4.2.2), the `docker/*` build, login, Buildx and QEMU
    actions, `github/codeql-action` (4.37.1 → 4.39.1), `codecov/codecov-action` and
    `ossf/scorecard-action` (2.4.3 → 2.5.0); `oras-project/setup-oras` 2.0.3 is new. Every
    action stays pinned by commit SHA.

- Added a `minimatch` override at `^11.0.1` (lockfile now at 11.0.3), clearing
  GHSA-g3hv-75wg-m249 (CVE-2026-118764) in the frontend lint toolchain. The
  patched release exists only on the 11 line. Development dependency only; the
  shipped binary and runtime image are unaffected.

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

## [1.3.0] - 2026-06-15

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

[Unreleased]: https://github.com/ovumcy/ovumcy-web/compare/v2.0.0...HEAD
[2.0.0]: https://github.com/ovumcy/ovumcy-web/compare/v1.9.2...v2.0.0
[1.9.2]: https://github.com/ovumcy/ovumcy-web/compare/v1.9.1...v1.9.2
[1.9.1]: https://github.com/ovumcy/ovumcy-web/compare/v1.9.0...v1.9.1
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
