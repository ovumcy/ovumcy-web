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
| `lutealPhase` | Days from ovulation to the next period | **14-day** default, refined toward the owner's own value from logged BBT / cervical-mucus signals when enough cycles carry them |

## The model

The model rests on one physiological assumption: **the luteal phase (ovulation →
next period) is relatively stable per person** — modelled at ~14 days by default,
and refined toward the owner's own value when logged signals allow — while the
follicular phase (period → ovulation) absorbs the variation in cycle length. So
ovulation is counted *backwards* from the next expected period.

### Constants

| Constant | Value | Role |
|----------|-------|------|
| `defaultLutealPhaseDays` | 14 | Default luteal phase, used when it is not refined from logged signals |
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

- **Cycle length** is the median of the owner's recent observed cycles (a cycle
  being the gap between two detected period starts). The median is used rather
  than the mean, so a single missed-log gap that merges two cycles cannot skew
  the estimate. When there is not enough history, the owner's configured value
  is used.
- **Luteal phase** defaults to the fixed 14-day model value, but is refined for
  the owner when their logs carry enough signal: when basal body temperature or
  cervical-mucus entries let the app infer the ovulation-to-next-period length
  across several cycles, that observed luteal length (clamped to a physiological
  10–20 day range) replaces the default. With little or no such data the fixed
  14-day default stands. Individual luteal phases vary (commonly 11–17 days),
  which is one reason predictions remain estimates.
- For irregular cycles the app widens the prediction into a range rather than a
  single date. The range and variability statistics (shortest/longest cycle and
  the sample standard deviation) are computed over the same recent-cycle window
  as the median, so an old outlier cycle stops affecting them once it ages out
  of the window.

## Assumptions and limitations

- Luteal phase defaults to a constant 14 days and is only refined when enough
  logged BBT / cervical-mucus signal exists; in reality it varies between people
  and cycles.
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

Every numeric claim above is enforced by `TestCyclePrediction_GoldenVectors`
and related tests in `internal/services/cycles_reference_test.go`. Property-based
tests (`cycles_property_test.go`) additionally assert the invariants for
thousands of generated inputs.

### Shared golden-vector fixture (lockstep with ovumcy-app)

The worked examples above are pinned to the code by a **shared golden-vector
fixture**, [`internal/services/testdata/cycle-prediction-golden-vectors.json`](../internal/services/testdata/cycle-prediction-golden-vectors.json),
consumed by `TestCyclePrediction_GoldenVectors`. That file is kept
**byte-identical** with ovumcy-app's
`src/services/__fixtures__/cycle-prediction-golden-vectors.json` (ovumcy-app
PR #75), where it is consumed by `src/services/cycle-prediction-reference.test.ts`.

The Go source of truth here (`internal/services/cycles.go`) and ovumcy-app's
TypeScript port (`src/services/cycle-prediction-policy.ts`) are hand-parallel
implementations; consuming one shared fixture makes any divergence between them
fail CI on **both** sides instead of drifting silently. If you change the
prediction math, update the fixture, **both** docs (this file and ovumcy-app's
`docs/cycle-prediction.md`), and **both** reference tests in the same change.

#### Projection / anchor section

The `vectors` section above pins the pure window math (`PredictCycleWindow`:
period start + cycle length + luteal phase → ovulation and fertile window). It
does **not** cover the layer production wraps around that math to project a live
prediction for a given day: choosing the cycle length from history, anchoring the
current cycle, and rolling ovulation forward. That layer is pinned by a second,
**additive** top-level key in the same fixture file, `projection`, consumed by
`TestCycleProjection_GoldenVectors`. Each projection vector takes a cycle-length
history plus `lastPeriodStart`, `today`, and an IANA `timezone`, and locks four
stages:

1. **Prediction length** — median-first (`predictedCycleLength`): the median of
   the recent-cycle window, falling back to the rounded mean only when no median
   exists. The `[28, 28, 28, 28, 60]` vector nails this down: it must select the
   median (28), never the mean (~34) that a single merged-log outlier inflates.
2. **Projected cycle start + cycle day** — `ProjectCycleStart` advances the last
   period start by whole cycles up to `today` and reports the 1-based day within
   the current cycle.
3. **Displayed next-period date** — the projected (un-shifted) start plus the
   selected length, re-anchored in the request timezone.
4. **Ovulation date** — the window for the projected cycle, rolled forward once
   by `ShiftCycleStartToFutureOvulation` when its ovulation already fell before
   `today`, so a displayed ovulation is never in the past.

All of this is calendar-day arithmetic and therefore **DST-immune**: the
`Europe/Berlin` 2026-03-29 spring-forward vector must project identically to a
UTC run — a truncating hour-based day count would mis-roll the cycle across the
23-hour transition day. The key is additive so an older twin that reads only
`vectors` keeps passing against a byte-identical file until it grows a
`projection` consumer of its own.
