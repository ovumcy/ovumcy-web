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
# -timeout 20m is the budget CI declares too — internal/api runs close enough to
# Go's 10-minute PER PACKAGE default that the default aborts the run with
# `panic: test timed out after 10m0s` (see TESTING.md)
go test ./cmd/... ./internal/... ./migrations/... ./scripts/... ./web/... -timeout 20m
go run ./scripts/archcheck
npm run lint:js
npm run build
```

`scripts/archcheck` reads the whole tree and answers four architecture
questions: nothing under `internal/api` or `internal/apideps` imports
persistence, the layers of [docs/architecture.md](docs/architecture.md) import
in one direction only (`internal/services` and `internal/db` never reach up into
transport, `internal/db` never reaches up into `internal/services`, and
`internal/models` depends on no other package in the module), no schema is
migrated at runtime, and no identifier names a role the product does not have
(the product is owner-role-only, so a `ViewerService` or a `partnerMode`
describes something that does not exist). It asks about the tree rather than
about a diff, so it answers the same way however the code got there — including
for a file behind a build tag your platform does not select, which a package
listing would not see. Test files are outside all of these rules on purpose: a
fixture proves a repository against the service that owns it and the reverse,
and it names an absent role precisely to prove that role is refused. CI runs it
too.

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
- `internal/i18n/locales/en.json` is the canonical source for UI strings. When you add or rename strings, mirror the change in `ru.json`, `es.json`, `fr.json`, `de.json`, and `it.json` (the six locales advertised as supported in the README). If you cannot provide a native translation for a non-`en` locale, copy the English string verbatim and leave a `TODO(<locale>)` next to it so the gap is visible in review and search.
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

`/api/v1/*` is the canonical, stable HTTP surface. External wrappers and integrations should target this prefix exclusively. Endpoints content-negotiate and emit JSON when the client sends `Accept: application/json` (or HTML/HTMX otherwise), so the JSON shape is part of the v1 contract:

- Field additions are non-breaking and may ship in any minor release.
- Field renames, removals, status code changes, route moves, and error key changes are breaking; they require a new major version (`/api/v2/*`) shipped alongside `/api/v1/*` long enough for callers to migrate.
- The export payload (`GET /api/v1/exports/{json,csv,summary}`) follows the separate stability contract documented in [docs/export.md](docs/export.md).

If you are scripting against `/api/v1/*` from outside the bundled UI, pin to a specific image tag and re-validate on every upgrade — `v1.x.y` minor bumps are safe; major bumps surface in [CHANGELOG.md](CHANGELOG.md) with the breaking entries called out.

## Changelog Fragments

Every pull request adds one changelog fragment, `changelog.d/<branch-name>.md`, instead of editing
[CHANGELOG.md](CHANGELOG.md); Dependabot's pull requests are the one exception, described below.
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
- A pull request with no user-visible change adds a fragment whose first line is exactly `none`;
  anything below that line is ignored, so the reason can be written underneath it.
- `CHANGELOG.md` itself is edited directly only by release assembly and by corrections to text that
  has already been released. At release time,
  `go run ./scripts/changelogd assemble -version X.Y.Z` merges the accumulated fragments (and
  anything still frozen under `[Unreleased]`) into a new released section in Keep a Changelog order
  and deletes the consumed fragments.
- The `changelog-fragment` CI check enforces this: it fails a pull request that adds neither a valid
  fragment nor a new `## [` heading in `CHANGELOG.md`, and it names the file and the problem when a
  fragment has an unknown section header or no entry text.
- Dependency updates opened by Dependabot are exempt: the check stays required for them but returns
  success without a fragment, since the bot cannot write one. Their entries reach the changelog at
  release time instead — the `### Dependencies` section is assembled from the dependency bumps that
  merged, not from fragments.

## Commit Style

Use imperative commit messages, e.g.:

- `Fix calendar ovulation tag precedence`
- `Pin staticcheck version in CI`
