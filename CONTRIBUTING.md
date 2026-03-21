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
go test ./...
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
- Keep `internal/i18n/locales/en.json` and `internal/i18n/locales/ru.json` in sync.
- Do not introduce legacy compatibility paths unless explicitly required.

## Commit Style

Use imperative commit messages, e.g.:

- `Fix calendar ovulation tag precedence`
- `Pin staticcheck version in CI`
