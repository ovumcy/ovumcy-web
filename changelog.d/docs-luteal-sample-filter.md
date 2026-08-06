### Internal

- **`docs/cycle-prediction.md` now describes what the inferred luteal phase actually does with an
  out-of-range cycle.** It said the observed length is "clamped to a physiological 10–20 day
  range"; the code discards such a cycle instead, and falls back to the fixed 14-day default
  unless at least two cycles survive the filter. The difference is visible to an owner — a clamp
  would let one implausible reading pull the estimate to 10 or 20, and none does.
