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
# scoped past node_modules/, where a vendored JS dep ships a .go file
go test ./cmd/... ./internal/... ./migrations/... ./scripts/... ./web/...
npm run lint:js
npm run build
```

If your change touches Go code, also gate it on patch coverage before opening a
PR: `scripts/patch-coverage-local.sh` (see "Checking patch coverage locally" in
[TESTING.md](TESTING.md) — a stale `coverage.out` gives a false pass, so don't
run `scripts/patchcov` by hand without a fresh profile).

**Recommended: enable the pre-push patch-coverage hook** so this runs
automatically instead of relying on remembering to run it by hand:

```bash
bash scripts/setup-hooks.sh
# equivalent one-liner, if you'd rather not run the script:
#   git config core.hooksPath scripts/hooks
```

This wires `scripts/hooks/pre-push` in via git's `core.hooksPath` (git does not
run a hook committed under a repo path unless something points it there first
— this is a one-time, per-clone setup step). Once enabled, `git push` first
checks whether the push includes any `*.go` changes:

- **No Go files changed** (e.g. a docs-only push): the hook exits immediately,
  no coverage run.
- **Go files changed**: the hook runs the same fresh patch-coverage gate as
  `scripts/patch-coverage-local.sh` (a few minutes — it reruns the real test
  suite on purpose, to avoid the stale-`coverage.out` false pass described
  above) and blocks the push if any modified line isn't covered, printing
  which lines and how to fix it.

Emergency bypass: `git push --no-verify` skips the hook entirely.

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

## API Stability Contract

`internal/api/routes.go` is the source of truth for HTTP endpoints; [docs/openapi.yaml](docs/openapi.yaml) is the authoritative description of the JSON surface.

`/api/v1/*` is the canonical, stable HTTP surface. External wrappers and integrations should target this prefix exclusively. Endpoints content-negotiate and emit JSON when the client sends `Accept: application/json` (or HTML/HTMX otherwise), so the JSON shape is part of the v1 contract:

- Field additions are non-breaking and may ship in any minor release.
- Field renames, removals, status code changes, route moves, and error key changes are breaking; they require a new major version (`/api/v2/*`) shipped alongside `/api/v1/*` long enough for callers to migrate.
- The export payload (`GET /api/v1/exports/{json,csv,summary}`) follows the separate stability contract documented in [docs/export.md](docs/export.md).

If you are scripting against `/api/v1/*` from outside the bundled UI, pin to a specific image tag and re-validate on every upgrade — `v1.x.y` minor bumps are safe; major bumps surface in [CHANGELOG.md](CHANGELOG.md) with the breaking entries called out.

## Commit Style

Use imperative commit messages, e.g.:

- `Fix calendar ovulation tag precedence`
- `Pin staticcheck version in CI`
