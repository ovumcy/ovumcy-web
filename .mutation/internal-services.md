# Mutation baseline — `internal/services`

Authoritative gremlins run for the core domain package. Reproduce with
`scripts/mutation.sh baseline` (per-package JSON lands in `.tmp/mutation/`); this
file is the reviewed summary committed alongside the code.

## Score (measured on the `mutation/shard-services-and-kills` branch)

**Update — run [28837779621](https://github.com/ovumcy/ovumcy-web/actions/runs/28837779621)
(first sharded services measurement).** The single-job run below timed out at the
3h cap (run 28827683010, killed at 3h0m17s, no report); `internal/services` is
now split into 5 file-subset shards that finished in 1h9m–2h27m and uploaded
green. Summed across the 5 shard JSONs: **killed 1508, lived 124, timed out 5,
not covered 65 → test efficacy 92.40% (1508/1632), mutator coverage 96.17%
(1632/1697)**. Survivors rose from 62 to 124 as the package grew (webhooks,
`.ics` feed, reminders, expanded stats). This is NOT a 7.6%-untested reading:
`CONDITIONALS_BOUNDARY` dominates and spot-triage shows these are overwhelmingly
equivalent — e.g. **all 13 `cycles.go` survivors are equivalent** (the
`CalcOvulationDay` L97 short-cycle threshold is redundant with the L104
`maxSupportedLutealPhase` guard rejecting the same `cycleLen < 15`; the
`LutealPhase <= 0` guard's sole caller sets the default the line above;
`tailInts`/`tailCycles` `len <= n` slices identically; min/max scan boundaries
keep the same extremum; capacity hints; `CycleLengthSpread` falls through to 0).
Real-gap kills landed on this branch from the `CONDITIONALS_NEGATION`/folding
survivors: the `.ics` feed lost its medical disclaimer
(`calendar_feed_service.go:117`), mis-folded long lines
(`calendar_feed_ics.go:216,226`), and resolved the owner's "today" in UTC instead
of the request timezone (`calendar_feed_service.go:106`); the post-restore
luteal-phase refresh no-op'd on success (`import_service.go:388`). The two
remaining tz-guards (`day_feedback_policy.go:62`, `day_service.go:366`) are
equivalent — `CalendarDay` uses `value.Date()` (no `In(location)` shift), so
`location` cancels through the subsequent UTC-midnight canonicalization. On the
webhook side the scheme-guard negation (`webhook_delivery.go:152`, an SSRF /
scheme-injection defence that refuses `file://`/`ftp://`/…) is killed by pinning
the `ErrWebhookDeliveryURLScheme` sentinel; `webhook_notify_service.go:247` is
equivalent (a best-effort watermark write whose mutant only flips which branch
logs). The remaining survivors are the same `CONDITIONALS_BOUNDARY` clamp/
threshold class. **Everything below this line is the prior `a6d7e41`
measurement, pending re-triage against this run.**

## Score (prior — `a6d7e41`)

**Source: workflow_dispatch run
[28758365493](https://github.com/ovumcy/ovumcy-web/actions/runs/28758365493)**
on `mutation.yml` (`mutation-baseline (internal_services)` job, single
unsharded run, green) — the same run that refreshed `internal-api.md` (PR
#174) and `internal-security.md`. Numbers confirmed two independent ways:
(a) gremlins' own end-of-run summary line in the job log, (b) counting
`"status"` values directly in the downloaded `internal_services.json`. Both
agree exactly.

| Metric | Value |
|--------|-------|
| Killed | 1478 |
| Lived (survivors) | 62 — 4 reconciled equivalents (below) + 58 new/not yet triaged |
| Timed out | 5 (property/fuzz runtime; not survivors, excluded from efficacy) |
| Not covered | 62 |
| Not viable | 0 |
| **Test efficacy** | **95.97%** — killed / (killed + lived) = 1478 / 1540 |
| Mutator coverage | 96.13% — (killed + lived) / (killed + lived + not covered) = 1540 / 1602 |

The efficacy figure dropped from the previously-measured 99.73% (`ac3a4af`) to
95.97% here — not a regression in test quality, but 43 feature commits
(webhooks, `.ics` calendar feed, reminder banner/scheduler, per-user timezone
persistence, JSON import/restore, prediction-struct refactor) landing between
`ac3a4af` and `a6d7e41` that added real code whose mutants have not yet been
individually triaged. See "Newly surfaced survivors" below — do not read the
raw 95.97% as "4% of behavior is untested"; some fraction of the 58 new
survivors are almost certainly equivalent mutants like the ones already
documented for this package and for `internal/security`.

## Round 3 hardening (2026-06-13)

The 10 commits between `ac3a4af` and `main` (audit phases 2–3, robustness/auth
hardening) introduced new code whose mutants were not yet covered, drifting the
killable-survivor count up from 4. Round 3 adds six focused test files
(`mutation_round3_{cycles,dashboard,day,i18n,stats,auth}_test.go`, ~52 tests)
that close the genuine gaps. **Every kill was verified per-mutant**: the exact
gremlins mutation was applied to the source and the targeted test confirmed to
fail, then reverted. Verified-killed survivor classes include:

- cycle math: `BuildCycleStats` observed-vs-detected starts, nil-location guards
  (`cycle_anchor`, `cycle_baseline`), `CalcOvulationDay`/`PredictCycleWindow`/
  `CycleLengthSpread`/`ResolveLutealPhase` boundaries;
- dashboard: irregular range `Max==Min`, `dashboardCycleHeroCurrentPhase`
  menstrual/ovulation/luteal day boundaries;
- day/calendar: `DayHasData`/`IsAutoFilledPeriodCandidate` mood boundaries,
  upsert/cycle-start transaction-error propagation;
- i18n: `LocalizedMonthYear`/`LocalizedDateLabel`/`LocalizedDashboardDate` January
  boundary, `russianPluralForm` lastDigit 1/2/4 boundaries;
- stats: reliability thresholds (3/6 cycles), `phaseForCompletedCycleDay` phase
  boundaries, `classifyStatsCycleFactorComparison` ±delta boundaries,
  `predictionExplanationPrimaryKey` branch selection, `compareISODate`,
  builtin-symptom sort;
- auth: `AuthAttemptPolicy.Configure` window lower bound, `attempt_limiter` sweep
  counter/size-guard, CAS session-version bump assertion.

The headline efficacy figure is re-measured by the weekly CI `mutation.yml` job
(clean Linux, stable Docker for the Postgres integration tests) and refreshes
from there — local Windows re-measurement was not used for the number because
(a) the Docker-backed Postgres integration tests (`testdb.StartPostgresDSN`)
perturb gremlins' single coverage pass when Docker is slow/cold, and
(b) gremlins' parallel-worker runs were not reproducible on this host. The
per-mutant verification above is the authoritative evidence that these mutants
are killed.

## Documented equivalent mutants

Reconciled against the `28758365493` survivor set: all 4 mutants below are
still present and still equivalent under the same argument — only their line
numbers shifted (intervening commits added code earlier in each file). None
of the previously-documented equivalents were killed or disappeared.

### 1–2. `cycles.go:337` — luteal-phase default guard (BOUNDARY, NEGATION)

```go
if stats.LutealPhase <= 0 {            // mutated
    stats.LutealPhase = defaultLutealPhaseDays
}
```

`applyPredictedCycleStats` has a single caller, `BuildCycleStats`, which sets
`stats.LutealPhase = defaultLutealPhaseDays` (= 14) on the line directly above the
call. The guard never sees `LutealPhase <= 0`:

- `<= 0` → `< 0` (BOUNDARY): `14 < 0` is still false — identical behavior.
- `<= 0` → `> 0` (NEGATION): `14 > 0` is true, but the body re-assigns
  `defaultLutealPhaseDays` over a value that already equals it — a no-op.

A defensive guard for a state no production path produces. Killing it would mean
calling the unexported helper with a hand-built `LutealPhase <= 0`, i.e. testing
dead code — declined per the no-brittle-tests policy.

### 3. `cycle_start_policy.go:80` — implantation pre-filter (ARITHMETIC_BASE)

```go
filtered := filterLogsNotAfter(logs, targetDay.AddDate(0, 0, -1))   // mutated
stats := BuildCycleStats(filtered, targetDay.Add(-time.Second))
```

`BuildCycleStats` re-applies `filterLogsNotAfter(logs, dateOnly(now))` internally,
clamping to `targetDay - 1d` regardless of the caller's pre-filter. Mutating the
`-1` to a later cutoff lets more logs past the caller's filter, but the internal
clamp drops them again — effective input unchanged, so the Median/Average cycle
length and luteal phase this function reads are identical.

### 4. `cycle_start_policy.go:81` — implantation "as-of" instant (INVERT_NEGATIVES)

```go
stats := BuildCycleStats(filtered, targetDay.Add(-time.Second))   // mutated
```

`-time.Second` → `+time.Second` shifts `BuildCycleStats`'s internal
`today = dateOnly(now)` from `targetDay - 1d` to `targetDay`. Since `filtered`
already excludes everything after `targetDay - 1d`, the internal filter selects the
same set either way. The only `today`-sensitive outputs — `CurrentCycleDay` and
`CurrentPhase` — are never read by `potentialImplantationGapDays`. No observable
change.

## Newly surfaced survivors — not yet triaged

The 43 commits between `ac3a4af` and `a6d7e41` (webhooks, `.ics` calendar feed
token storage/lifecycle, reminder banner + scheduler, per-user timezone
persistence, JSON import/restore, the prediction multi-bool→struct refactor,
bcrypt cost raise) added real domain code whose mutants were never run against
before. Unlike the 4 mutants above, these 58 are **not** individually triaged
into "real gap" vs. "equivalent mutant" yet — that requires the same
per-mutant source review (apply the mutation, confirm no test fails, judge
reachability) applied to the legacy 4, which is a separate follow-up, not
implied by getting real numbers out of CI. Do not read the delta from the
prior 99.73% as "behavior regressed" — some fraction of these are almost
certainly equivalent mutants like the ones already documented above and in
`internal-security.md`.

One adjacent guard is explicitly **not** folded into entries #3–4 above despite
sitting one line below them: `cycle_start_policy.go:83` and `:86`
(`if cycleLength <= 0 { cycleLength = DashboardCycleReferenceLength(...) }`,
twice) are new fallback branches introduced by the same refactor that added
`ResolveLutealPhase`/`DashboardCycleReferenceLength` — each has a `BOUNDARY`
survivor and a killed `NEGATION` mutant. They were not part of the codebase at
`ac3a4af` and need their own equivalence review, not inherited from #3–4's
argument.

Survivor count by file (from `internal_services.json`, all files with at least
one survivor):

| File | Survivors |
|------|-----------|
| `cycles.go` | 13 (2 reconciled equivalents above; 11 new) |
| `dashboard_cycle.go` | 7 |
| `cycle_start_policy.go` | 7 (2 reconciled equivalents above; 5 new) |
| `import_service.go` | 7 |
| `day_service.go` | 5 |
| `date_i18n_policy.go` | 5 |
| `cycle_signals.go` | 3 |
| `dashboard_view_service.go` | 3 |
| `dashboard_cycle_hero.go` | 3 |
| `calendar_days.go` | 2 |
| `i18n_policy.go` | 2 |
| `attempt_limiter.go` | 1 |
| `cycle_baseline.go` | 1 |
| `day_input.go` | 1 |
| `day_feedback_policy.go` | 1 |
| `calendar_view_policy.go` | 1 |

The 62 not-covered mutants concentrate in `dashboard_cycle_hero.go` (11) and
`stats_cycle_factor_context.go` (11), followed by `stats_page_view_service.go`
(6), `i18n_policy.go` (5), and `stats_phase_insights.go` (5) — worth checking
whether these are dead code, tested only at the integration/e2e layer, or a
genuine gap before the next triage pass. The 5 timed-out mutants are in
`cycles.go` (4) and `day_service.go` (1) — property/fuzz runtime, not
survivors, excluded from efficacy per the existing convention.
