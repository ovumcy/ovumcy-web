none

CI-only: caches the gosec and govulncheck tool binaries (keyed on their pinned
versions, verified on restore) and adds a `concurrency` group to codeql.yml,
gitleaks.yml and changelog.yml matching the shape already used by ci.yml and
security.yml.
