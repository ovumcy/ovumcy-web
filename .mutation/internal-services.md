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
