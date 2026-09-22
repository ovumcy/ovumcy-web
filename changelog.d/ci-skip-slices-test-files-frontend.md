none

CI-only: the `changes` job no longer forces the browser e2e shards for a diff
confined to Go test files and `testdata/` (they never reach the compiled
binary; the Go unit/race lanes still run for them), and `test-frontend` now
gates on its own `run_frontend` output — true when `web/`, `e2e/`,
`scripts/*.mjs`, `package(-lock).json`, `eslint.config.mjs`,
`tsconfig.json`, `playwright.config.ts`, or one of the three files
`web/src/css/input.css` names as a Tailwind `@source` outside `web/`
(`internal/templates/**/*.html`, `internal/httpx/markup.go`,
`internal/api/calendar_days_view_helpers.go`) changed — instead of riding
the Go lanes' `run_core`, so a backend-only pull request no longer pays it
while a template-only one still does. `run_frontend` clears on push exactly
like `run_core`, since the merge queue already ran it on the identical
commit. scripts/ciguards pins the allowlist against every `@source` line in
that file so a fourth one added later cannot drift past it silently.
