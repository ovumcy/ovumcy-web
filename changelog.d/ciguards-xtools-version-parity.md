none

Test-only: a guard in scripts/ciguards now requires `ANALYSIS_XTOOLS_VERSION` to be
one literal module version across every workflow that reads it. ci.yml (staticcheck,
golangci-lint) and security.yml (gosec) each pin it in their own `env:` block, so a
bump that lifted one copy left the other workflow's analyzers on an older x/tools,
failing on every package at the next Go release in that workflow only. The guard
fails closed on a missing, duplicated (shadowing), non-`env:`, empty, expression or
block-scalar entry, and requires both ci.yml and security.yml to be found and parsed
by name.
