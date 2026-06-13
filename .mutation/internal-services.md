# Mutation baseline — `internal/services`

Authoritative gremlins run for the core domain package. Reproduce with
`scripts/mutation.sh baseline` (per-package JSON lands in `.tmp/mutation/`); this
file is the reviewed summary committed alongside the code.

## Score (measured on `ac3a4af`)

| Metric | Value |
|--------|-------|
| Killed | 1462 |
| Lived (survivors) | 4 — all documented equivalents (below) |
| Timed out | 39 (property/fuzz runtime; not survivors, excluded from efficacy) |
| Not covered | 59 |
| Not viable | 0 |
| **Test efficacy** | **99.73%** — killed / (killed + lived) = 1462 / 1466 |
| Mutator coverage | 96.13% |

Efficacy over *killable* mutants is 100%: every survivor below is an equivalent
mutant — a fault that cannot change any observable output — verified individually.

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

### 1–2. `cycles.go:315` — luteal-phase default guard (BOUNDARY, NEGATION)

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
