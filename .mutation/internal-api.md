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

A single unsharded CI run exceeds the job's 3h timeout before finishing (issue
#161), so `mutation.yml` runs `internal/api` as 5 file-subset shards
(`internal_api_1`..`internal_api_5` matrix entries, each excluding every file
not assigned to it — see `scripts/mutation.sh`'s `api_shard_*` functions) and a
follow-up job merges the 5 shard JSON reports into one `internal_api.json` via
`scripts/mutationmerge`, matching the single-file-per-target convention
`internal_services.json`/`internal_security.json` already use. The numbers
below, once filled in, are the combined total across all 5 shards — the same
number a hypothetical single unsharded run would have produced.

| Metric | Value |
|--------|-------|
| Killed | pending first weekly CI run |
| Lived (survivors) | pending first weekly CI run |
| Timed out | pending first weekly CI run |
| Not covered | pending first weekly CI run |
| Not viable | pending first weekly CI run |
| **Test efficacy** | pending first weekly CI run |
| Mutator coverage | pending first weekly CI run |
