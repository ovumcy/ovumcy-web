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
- `internal/i18n/locales/en.json` is the canonical source for UI strings. When you add or rename strings, mirror the change in `ru.json`, `es.json`, `fr.json`, and `de.json` (the five locales advertised as supported in the README). If you cannot provide a native translation for a non-`en` locale, copy the English string verbatim and leave a `TODO(<locale>)` next to it so the gap is visible in review and search.
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
