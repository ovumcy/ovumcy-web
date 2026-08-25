### Internal

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
