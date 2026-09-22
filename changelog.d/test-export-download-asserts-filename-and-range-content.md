none

Test only (WEB-17 / TS-M17): e2e/settings-export-download.spec.ts presses both
export buttons (CSV, JSON) for each preset range (30/90/365/all) and checks the
downloaded filename plus parsed content — entries inside the range present,
entries outside it absent — against seeded fixture data. The API contract
itself stays covered at its own level (export_regressions_test.go).
