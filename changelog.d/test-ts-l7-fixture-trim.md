none

Test-only cleanup: `internal/db`'s reminder and TOTP-step suites each carried
their own byte-identical copy of the shared owner-user fixture (and its
matching temp-SQLite opener) under a locally scoped name. Both now call the
already-widely-used `createUserForTimezoneTest`/`openTimezoneRepoForTest`
pair instead. No test assertions changed; the unsupported-role and onboarding
fixtures elsewhere in the suite are deliberately distinct and were left
alone.
