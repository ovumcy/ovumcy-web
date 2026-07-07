# Mutation baseline — `internal/api`

Authoritative gremlins run for the transport/HTTP package. Reproduce with
`scripts/mutation.sh baseline` (per-package JSON lands in `.tmp/mutation/`); this
file is the reviewed summary committed alongside the code.

## Score (measured on `a6d7e41`)

A single unsharded CI run exceeds the job's 3h timeout before finishing (issue
#161), so `mutation.yml` runs `internal/api` as 5 file-subset shards
(`internal_api_1`..`internal_api_5` matrix entries, each excluding every file
not assigned to it — see `scripts/mutation.sh`'s `shard_*` functions) and a
follow-up job (`mutation-merge`) merges the 5 shard JSON reports into one
`internal_api.json` via `scripts/mutationmerge`, matching the single-file-per-target
convention `internal_services.json`/`internal_security.json` already use (PR
#163). The numbers below are the first complete run since sharding landed —
**workflow_dispatch run [28758365493](https://github.com/ovumcy/ovumcy-web/actions/runs/28758365493)**,
all 5 shards plus the merge job green — and are the combined total across all 5
shards, the same figure a hypothetical single unsharded run would have produced.

| Metric | Value |
|--------|-------|
| Killed | 488 |
| Lived (survivors) | 128 |
| Timed out | 0 |
| Not covered | 98 |
| Not viable | 0 |
| **Test efficacy** | **79.22%** — killed / (killed + lived) = 488 / 616 |
| Mutator coverage | 86.27% — (killed + lived) / (killed + lived + not covered) = 616 / 714 |

Per-shard breakdown (gremlins' own summary line for each matrix job; sums to the
combined row above):

| Shard | Killed | Lived | Not covered | Test efficacy | Mutator coverage |
|-------|--------|-------|-------------|----------------|-------------------|
| internal_api_1 | 77 | 29 | 19 | 72.64% | 84.80% |
| internal_api_2 | 111 | 21 | 6 | 84.09% | 95.65% |
| internal_api_3 | 93 | 8 | 9 | 92.08% | 91.82% |
| internal_api_4 | 115 | 52 | 46 | 68.86% | 78.40% |
| internal_api_5 | 92 | 18 | 18 | 83.64% | 85.94% |

## Survivors — not yet triaged

Unlike `internal-services.md`/`internal-security.md`, the 128 survivors here are
**not** individually triaged into "real gap" vs. "documented equivalent mutant"
yet — that requires a source-level equivalence review per mutant (apply the
mutation, confirm no test fails, judge whether it's reachable), which is a
separate follow-up, not implied by getting real numbers out of CI for the first
time. Do not read the raw 79.22% efficacy as "21% of behavior is untested" —
some fraction of these are almost certainly equivalent mutants like the ones
already documented for `internal/services`/`internal/security`.

Survivor count by file, top entries (from the merged `internal_api.json`; full
list has 40 files with at least one survivor):

| File | Survivors |
|------|-----------|
| `stats_page_helpers.go` | 19 |
| `handlers_auth_token_helpers.go` | 10 |
| `request_logging.go` | 9 |
| `handlers_days_status_helpers.go` | 8 |
| `security_event_logging.go` | 7 |
| `handlers_template_ui_helpers.go` | 6 |
| `handlers_template_format_helpers.go` | 6 |
| `settings_symptom_view_models.go` | 5 |
| `i18n_helpers.go` | 5 |

The 98 not-covered mutants concentrate similarly: `request_logging.go` (35),
`request_timezone_policy.go` (16), and `asset_version.go` (15) account for two
thirds of them — worth checking whether these are dead code, tested only at the
integration/e2e layer (the `internal/security` OIDC precedent), or a genuine gap
before the next triage pass.
