none

CI security-scanner configuration only, with no effect on the shipped application. `.gitleaks.toml`
allowlisted every `_test.go` and every `e2e/*.spec.ts` file by path, which switched off every
gitleaks rule on those files entirely — not just the handful of known fixture values the blanket was
written for. A real secret pasted into a test or an e2e spec would have gone undetected.

The path blanket is gone. Every known fixture (the previously-documented ones, plus several more a
full-history sweep with no allowlist at all turned up) is now exempted by its exact value against the
one rule that flags it (`generic-api-key`), under `[[rules]] id = "generic-api-key"` extending
gitleaks' default rule set. `.env.example` and the history-only e2e TLS PEM fixtures keep their
existing path exemptions; they were never part of the blanket this removes. A synthetic secret
injected into a `_test.go` file for local verification confirmed the old config missed it and the new
one catches it; a full-history scan with the new config still reports zero findings.
