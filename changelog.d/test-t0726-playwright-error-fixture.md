none

Browser-suite strengthening, with no production behaviour change. The e2e suite
had exactly one error handler — a `page.on('console')` collector inside a single
register test — so a page could throw an uncaught exception, or log a console
error, while every locator the spec asserted still resolved, and the run reported
green. Every spec now imports `test` from `e2e/support/fixtures.ts`, which binds
to the BrowserContext (so a page a spec opens itself is covered too) and fails
the test on any console error or page error that no allowlist entry covers. Each
allowlist entry carries its reason inline; the one entry there today is the
non-2xx resource line every engine logs, which the suite provokes on purpose.

The guard was observed failing before it was believed: a throwaway spec that logs
a console error, and one that throws asynchronously in the page, both fail on the
shared `test` and both pass on Playwright's stock `test` with every assertion
green. An ESLint `no-restricted-imports` rule keeps a new spec from importing
`@playwright/test` directly, so the converted set cannot grow a silent
unconverted half; that rule was observed failing on a spec reverted to the stock
import.

`playwright.config.ts` gains a comment naming which of its three projects the
gating pipeline actually installs and runs. The matrix itself is unchanged.
