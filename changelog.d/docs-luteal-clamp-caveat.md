### Internal

- **The cycle-prediction doc stated the personalized round trip as an equality it does not always
  hold.** "An ovulation observed on cycle day N predicts cycle day N again" is true only while the
  luteal phase that observation implies still fits the cycle; past that the reserve clamp applies and
  the prediction is marked non-exact, which the step immediately above it already documented for its
  own clamp. The invariant now carries the exception, and the worked-example table carries a row for
  it — an ovulation observed on day 4 of a 15-day cycle, where the implied 11-day luteal phase is
  clamped to 10 and ovulation is estimated on cycle day 5. That row is asserted by the same reference
  test as the others, so the exception is demonstrated rather than only described. No behaviour
  changes: the clamp is what the code already did.
