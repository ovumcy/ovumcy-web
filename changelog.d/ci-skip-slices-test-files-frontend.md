none

CI-only: the `changes` job no longer forces the browser e2e shards for a diff
confined to Go test files and `testdata/` inside the Go trees (they never
reach the compiled binary; the Go unit/race lanes still run for them), and
`test-frontend` now gates on its own `run_frontend` output — true when
`web/`, `e2e/`, `scripts/*.mjs`, `package(-lock).json`, `eslint.config.mjs`,
`tsconfig.json`, `playwright.config.ts`, `.github/workflows/ci.yml`, or any
file `web/src/css/input.css` names as a Tailwind `@source` outside `web/`
changed — instead of riding the Go lanes' `run_core`, so a backend-only pull
request no longer pays it while a template-only one still does.
`run_frontend` clears on push exactly like `run_core`, since the merge queue
already ran it on the identical commit. The diff is now listed with
`core.quotePath=false` and `--no-renames`, so a non-ASCII path or the source
side of a move is judged like any other. scripts/ciguards executes the
detect step against fixture diffs and pins the allowlist against every
`@source` in that file.
