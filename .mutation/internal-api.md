# Mutation baseline — `internal/api`

Authoritative gremlins run for the transport/HTTP package. Reproduce with
`scripts/mutation.sh baseline` (per-package JSON lands in `.tmp/mutation/`); this
file is the reviewed summary committed alongside the code.

## Score (pending first weekly CI run)

`internal/api` joined the baseline scope in `scripts/mutation.sh` alongside
`internal/services` and `internal/security`. It is the largest package
(~8.5k source lines) and carries heavy integration tests against a real
database, so no local number is authoritative for it: canonical efficacy comes
only from the weekly `mutation.yml` CI job (clean Linux), never from a local
Windows run. This file will be filled in — measuring commit, killed/lived/timed
out/not-covered counts, efficacy, and any documented equivalent mutants — once
that job has produced its first `internal/api` result.

| Metric | Value |
|--------|-------|
| Killed | pending first weekly CI run |
| Lived (survivors) | pending first weekly CI run |
| Timed out | pending first weekly CI run |
| Not covered | pending first weekly CI run |
| Not viable | pending first weekly CI run |
| **Test efficacy** | pending first weekly CI run |
| Mutator coverage | pending first weekly CI run |
