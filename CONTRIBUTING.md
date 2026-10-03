# Contributing to Ovumcy

Thanks for contributing.

## Development Setup

1. Install Go and Node.js.
2. Install frontend deps:

```bash
npm ci
```

3. Run checks locally:

```bash
# scoped past node_modules/, where a vendored JS dep ships a .go file;
# -timeout 30m: internal/api outruns Go's 10-minute PER PACKAGE default on a dev
# host (CI shards it into 20m cells), so the default aborts the run with
# `panic: test timed out after 10m0s` (see TESTING.md)
go test ./cmd/... ./internal/... ./migrations/... ./scripts/... ./web/... -timeout 30m
go run ./scripts/archcheck
npm run lint:js
npm run lint:types
npm run test:unit
npm run build
```

`scripts/archcheck` reads the whole tree and answers three architecture
questions: nothing under `internal/api` or `internal/apideps` imports
persistence, the layers of [docs/architecture.md](docs/architecture.md) import
in one direction only (`internal/services` and `internal/db` never reach up into
transport, `internal/db` never reaches up into `internal/services`, and
`internal/models` depends on no other package in the module), and no schema is
migrated at runtime. It asks about the tree rather than about a diff, so it
answers the same way however the code got there — including for a file behind a
build tag your platform does not select, which a package listing would not see.
Test files are outside the two import rules on purpose: a fixture proves a
repository against the service that owns it, and the reverse. CI runs it too.

The same check refuses a commit, through `.githooks/pre-commit`. That file is
tracked but git hooks are not, so it has to be installed once per clone:

```bash
printf '#!/bin/sh\nexec sh "$(git rev-parse --show-toplevel)/.githooks/pre-commit" "$@"\n' > .git/hooks/pre-commit
chmod +x .git/hooks/pre-commit
```

A shim rather than a copy, so a later change to the tracked check is picked up
without reinstalling.

Skipping the install costs nothing but the early warning — CI is the backstop.

If your change touches Go code, it must also pass patch coverage: every
modified, coverable Go line needs to be exercised by a test. This isn't
enforced locally — CI's `patch-coverage` job is the gate. See "Checking patch
coverage locally" in [TESTING.md](TESTING.md) if you want to check before
pushing (a stale `coverage.out` gives a false pass, so don't run
`scripts/patchcov` by hand without a fresh profile).

4. Start app locally:

```bash
go run ./cmd/ovumcy
```

It listens on `PORT` (default `8080`) on every interface; `HOST_BIND_ADDRESS`
applies to compose only. On a machine others can reach, firewall the port.

## Reporting Bugs

Before opening a bug, check existing issues:
- https://github.com/ovumcy/ovumcy-web/issues

When opening a bug report, include:
- environment (OS, browser, Go/Node versions),
- exact steps to reproduce,
- expected vs actual behavior,
- relevant logs/screenshots,
- commit hash or branch if testing unreleased code.

Use the bug report template in `.github/ISSUE_TEMPLATE/bug_report.yml`.

Security issues should not be reported publicly. Use [SECURITY.md](SECURITY.md).

## Pull Request Rules

- Keep changes scoped and atomic.
- Add/adjust tests for behavioral changes.
- `internal/i18n/locales/en.json` is the canonical source for UI strings. When you add or rename strings, mirror the change in `ru.json`, `es.json`, `fr.json`, `de.json`, and `it.json` (the six locales advertised as supported in the README). If you cannot provide a native translation for a non-`en` locale, copy the English string verbatim and list the copied keys and their locales in the pull request description. The gap cannot be marked in the file itself: the locale files are strict JSON parsed at boot, so a comment breaks startup, an extra key fails `TestLocaleKeysParity`, and a marker inside the value is no longer the verbatim English string.
- Do not introduce legacy compatibility paths unless explicitly required.
- When cutting a release, bump every release-tag mention in README.md (intro blurb, Docker quick
  start image tag, cosign/attestation/SBOM verification examples, Releases section) to the new tag.
  `go test ./scripts/readmeversion/...` fails if any occurrence disagrees with the others.

## Migrations

Migration files are immutable once released: fix forward with a new migration, never edit an
applied one. The prose inside a migration is a snapshot of the contract at the time it shipped and
is not updated afterwards — the current contract always lives in the docs. Two known historical
spots, kept as shipped:

- `032_calendar_feed_verifier_mac.sql` (both dialects) describes how legacy feed rows behaved
  before the key-rotation sentinel existed; the current rotation contract is documented in
  [docs/security/cryptography.md](docs/security/cryptography.md) and
  [docs/self-hosted.md](docs/self-hosted.md).
- `003_daily_logs_schema_reconcile.sql` and `024_daily_logs_bbt_nullable.sql` end with
  `INSERT OR REPLACE INTO sqlite_sequence(…)`. `sqlite_sequence` has no unique index, so the
  statement appends a duplicate row instead of replacing one. The value it writes matches what
  SQLite already maintains through the preceding `INSERT … SELECT`, so the extra row is inert —
  do not copy the pattern into a new migration.

## API Stability Contract

`internal/api/routes.go` is the source of truth for HTTP endpoints; [docs/openapi.yaml](docs/openapi.yaml) is the authoritative description of the JSON surface.

`/api/v1/*` is the only HTTP API prefix, and external wrappers and integrations should
target it exclusively. There is no `/api/v2/*` and none is planned: a breaking change
ships in a new major release of Ovumcy on the same `/api/v1/*` prefix, never under a
parallel one. Endpoints content-negotiate, so the JSON shape is part of the contract:

- Non-breaking, any minor or patch release: a new response field, a new optional
  request field, a new endpoint. A published response schema that lists its fields
  exactly (`additionalProperties: false`) fails a strict validator on a new field,
  so a client that validates responses strictly must validate against the spec of
  the release it runs.
- Breaking, major release only: renaming or removing a field, requiring a request
  member that was optional, refusing one that was accepted, changing what a field or
  an operation means, changing a status code, a route or an error key. Each ships as
  a **Breaking (API shape)** entry in CHANGELOG.md that names what a client has to
  change (the marker is described under Changelog Fragments below).
- A new per-account rate limit is not a break: it answers `429` only to a client that
  was already guessing a credential or a code, and is recorded as an ordinary
  Security or Added entry.
- A correction that makes docs/openapi.yaml describe what the server already does is
  not a breaking change, even when the corrected text is stricter than the old one;
  it is listed under **Fixed**.
- `info.version` in docs/openapi.yaml names the major release the contract belongs
  to — `2.0.0` for every v2.x release — and changes only with a major release.
- The export payload (`GET /api/v1/exports/{json,csv,summary}`) follows the separate
  stability contract in [docs/export.md](docs/export.md).

v2.0.0 is the first major release under this policy. It changes `/api/v1/*` in place;
the largest changes: `GET /api/v1/stats/overview` publishes an explicit payload that
withholds suppressed predictions, `PATCH /api/v1/users/current/cycle` updates only the
members a JSON request names, `POST /api/v1/password-resets` requires the account
password, and onboarding step 2 refuses `age_group`. The **Breaking (API shape)**
entries of the v2.0.0 section of CHANGELOG.md are the complete list of `/api/v1/*`
breaks.

If you script against `/api/v1/*` from outside the bundled UI, pin a specific image
tag and re-validate on every upgrade: minor and patch releases within a major are
safe; before a major upgrade, read its **Breaking (API shape)** entries.

## Changelog Fragments

Every pull request adds one changelog fragment, `changelog.d/<slug>.md` — the slug is the branch name
without its type prefix, so `fix/web123-cli-schema-check` adds `changelog.d/web123-cli-schema-check.md`
— instead of editing [CHANGELOG.md](CHANGELOG.md); Dependabot's pull requests are the one exception,
described below. The check refuses a pull request none of whose added fragments carries that name.
Several pull requests inserting an entry at the same anchor in the `[Unreleased]` section was this
repository's only recurring merge conflict, and rebasing replays the earlier commit straight back
into the contested spot; a file per branch has no shared anchor.

A fragment holds exactly the text that used to go under `[Unreleased]` — a Keep a Changelog section
header plus the entry:

```markdown
### Fixed

- **Short summary.** What changed, and what an operator or a user notices.
```

- Valid headers are the Keep a Changelog sections — `### Added`, `### Changed`, `### Deprecated`,
  `### Removed`, `### Fixed`, `### Security` — plus the two this changelog has always carried after
  them, `### Internal` and `### Dependencies`. Several sections may appear in one fragment; assembly
  puts them in that order.
- An entry for a breaking `/api/v1/*` change (as the API Stability Contract above defines it) stays
  in its ordinary section — usually `### Changed` or `### Fixed`; there is no `### Breaking` header —
  and opens its bold summary with `Breaking (API shape):`, for example
  `- **Breaking (API shape): onboarding step 2 no longer accepts age_group.**`. The entry names what
  a client has to change. A break is judged against the last release, so a route, or a behaviour
  introduced and changed again between two releases, that no release shipped carries no marker.
- A pull request with no user-visible change adds a fragment whose first line is exactly `none`;
  anything below that line is ignored, so the reason can be written underneath it.
- `CHANGELOG.md` itself is edited directly only by release assembly and by corrections to text that
  has already been released. At release time,
  `go run ./scripts/changelogd assemble -version X.Y.Z` merges the accumulated fragments (and
  anything still frozen under `[Unreleased]`) into a new released section in Keep a Changelog order
  and deletes the consumed fragments.
- The `changelog-fragment` CI check enforces this: it fails a pull request that adds neither a valid
  fragment nor the heading of a new release (`## [x.y.z]`) in `CHANGELOG.md`, and it names the file
  and the problem when a fragment (added or modified) has an unknown section header or no entry
  text. It also fails a pull request that edits `CHANGELOG.md` above the first released
  `## [x.y.z]` heading — the title or the `[Unreleased]` body — unless it adds a new release
  heading (assembly), whether or not a fragment is added beside it. Re-typing an existing heading,
  such as a date correction, is not assembly: that heading belongs to its released section, so
  correcting it is allowed, and it never licenses an edit above it.
- A pull request adds exactly one fragment: a second added fragment is refused, and so is deleting
  or renaming away a fragment another branch landed, unless that fragment's content is `none`
  (it holds no entry). A `none` fragment renamed into a real entry stays refused. To check the
  naming rule locally against the branch name a pull request will carry — on a detached worktree,
  or under another branch name — set `GITHUB_HEAD_REF` (the variable CI provides); it overrides
  the current branch.
- Dependency updates opened by Dependabot are exempt: the check stays required for them but returns
  success without a fragment, since the bot cannot write one. Nothing collects their entries
  automatically either: at release time the `### Dependencies` section is compiled from
  `git log <last-tag>..HEAD` over the Dependabot merges and entered as one fragment before
  `assemble` runs — a step of the release checklist, not of the gate.

## Commit Style

Use imperative commit messages, e.g.:

- `Fix calendar ovulation tag precedence`
- `Pin staticcheck version in CI`
