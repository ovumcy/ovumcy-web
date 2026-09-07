### Fixed

- **`ovulation_exact`'s description no longer claims it is a personalisation signal.** The flag
  reports exactly what `CalcOvulationDay` decides: whether the luteal phase fit inside the cycle
  without the arithmetic clamp. It is `true` on the 14-day model default whenever that default
  happens to fit a cycle, and `false` on a per-owner inferred value the cycle was too short to hold —
  the opposite of what the wording said. It remains independent of `ovulation_confirmed` and always
  `false` when `ovulation_date` is null; only the misleading half of the description changes.

### Added

- **`GET /api/v1/stats/overview` gains `luteal_phase_personalised`.** It reports whether
  `luteal_phase` was inferred from the owner's own logs (`InferUserLutealPhase`'s refined return)
  rather than the 14-day model constant — the signal `ovulation_exact` was wrongly described as
  carrying. The two are independent: a personalised luteal phase can still be clamped
  (`ovulation_exact = false`), and the 14-day default can still fit a cycle exactly
  (`ovulation_exact = true`). `false` means the owner's logs did not yet support an inference, not
  that a personalised value was tried and rejected. Cleared to `false` under fertility suppression,
  alongside `ovulation_date` and `ovulation_exact`, so it never asserts personalisation about a
  projection this tier has already withheld.
