# Cycle prediction — how the math works

This document describes, in full, how ovumcy estimates ovulation, the fertile
window, and the next period. It exists so that anyone — users, contributors,
auditors — can read exactly what the app computes and verify it against the
code. The worked examples below are mirrored 1:1 by automated reference tests
(`internal/services/cycles_reference_test.go`), so the documentation and the
implementation cannot silently drift apart.

> [!IMPORTANT]
> **This is a calendar-based estimate, not medical advice and not a method of
> contraception.** Predictions are statistical guesses derived from cycle
> dates. They cannot detect ovulation, do not account for illness, stress,
> medication, or hormonal conditions, and are unreliable for irregular cycles.
> Do not rely on them to avoid or achieve pregnancy. Consult a qualified
> healthcare professional for medical decisions.

## Inputs

| Input | Meaning | Source |
|-------|---------|--------|
| `periodStart` | First day of the current menstrual period (cycle day 1) | Detected from logged period days |
| `cycleLength` | Length of the cycle in days | Median of observed cycles, or the user's configured value |
| `lutealPhase` | Days from ovulation to the next period | Fixed model default of **14 days** |

## The model

The model rests on one physiological assumption: **the luteal phase (ovulation →
next period) is roughly constant at ~14 days**, while the follicular phase (period
→ ovulation) absorbs the variation in cycle length. So ovulation is counted
*backwards* from the next expected period.

### Constants

| Constant | Value | Role |
|----------|-------|------|
| `defaultLutealPhaseDays` | 14 | Luteal phase used by the app |
| `minLutealPhaseDays` | 10 | Lower clamp for the luteal phase |
| `minOvulationCycleDay` | 5 | Ovulation may not fall before cycle day 5 |
| (min cycle for a prediction) | 15 | `minLutealPhaseDays + minOvulationCycleDay` |

### Step 1 — resolve the luteal phase

```
luteal ≤ 0          → 14   (default)
0 < luteal < 10     → 10   (minimum)
luteal ≥ 10         → luteal
```

### Step 2 — ovulation day (1-based, within the cycle)

```
if cycleLength < 15:                      no prediction
maxSupportedLuteal = cycleLength − 5
if resolvedLuteal > maxSupportedLuteal:   resolvedLuteal = maxSupportedLuteal   (prediction marked non-exact)
ovulationDay = cycleLength − resolvedLuteal
if ovulationDay < 5:                      no prediction
```

`periodStart` is cycle day 1, so the ovulation **date** is
`periodStart + (ovulationDay − 1)` days.

### Step 3 — fertile window

The fertile window is the **6-day range ending on ovulation day**, reflecting
that sperm can survive several days and the egg is viable for about a day:

```
fertilityEnd   = ovulationDate
fertilityStart = ovulationDate − 5 days
if fertilityStart < periodStart:  fertilityStart = periodStart   (short-cycle clamp)
```

On short cycles the window may overlap menstruation; it is never allowed to
start before the period.

### Step 4 — next period

```
nextPeriodStart = periodStart + cycleLength days
```

A prediction is only returned when the ovulation date falls strictly before the
next period start.

## Worked examples

These are the exact cases asserted by the reference tests.

| periodStart | cycleLength | lutealPhase | → ovulation | fertile window | next period | exact? |
|-------------|-------------|-------------|-------------|----------------|-------------|--------|
| 2026-03-10 | 28 | 14 | 2026-03-23 | 2026-03-18 … 2026-03-23 | 2026-04-07 | yes |
| 2026-06-01 | 30 | 0 (→14) | 2026-06-16 | 2026-06-11 … 2026-06-16 | 2026-07-01 | yes |
| 2026-01-01 | 21 | 14 | 2026-01-07 | 2026-01-02 … 2026-01-07 | 2026-01-22 | yes |
| 2026-02-01 | 15 | 14 (→10) | 2026-02-05 | 2026-02-01 … 2026-02-05 | 2026-02-16 | no (luteal clamped, window clamped to period start) |
| any | 14 | any | — | — | — | no prediction (cycle too short) |

## How cycle length and luteal phase are chosen

- **Cycle length** is the median of the user's observed completed cycles (a
  cycle being the gap between two detected period starts). When there is not
  enough history, the user's configured value is used.
- **Luteal phase** is not estimated per user; the app uses the fixed 14-day
  model default. This is a deliberate simplification — individual luteal phases
  vary, which is one reason predictions are estimates.
- For irregular cycles the app widens the prediction into a range rather than a
  single date.

## Assumptions and limitations

- Luteal phase is modeled as constant; in reality it varies between people and
  cycles.
- Predictions are **calendar-based** and cannot observe the body. They do not
  use temperature, LH tests, or symptoms to confirm ovulation.
- Accuracy degrades sharply for irregular or very short/long cycles.
- The model is **not** a fertility-awareness contraceptive method (which require
  trained tracking of multiple biomarkers).

## Physiological basis

The ~14-day luteal phase and the "6-day fertile window ending at ovulation" are
standard reproductive-physiology concepts (e.g. the fertile-window work of
Wilcox et al., *NEJM* 1995). ovumcy applies them as a transparent calendar
estimate, nothing more.

## Verifying this document

Every numeric claim above is enforced by `TestCyclePrediction_ReferenceVectors`
and related tests in `internal/services/cycles_reference_test.go`. Property-based
tests (`cycles_property_test.go`) additionally assert the invariants for
thousands of generated inputs. If you change the math, these tests must be
updated in lockstep with this document.
