### Fixed

- **An impossible temperature is no longer saved as an empty one.** For an account tracking in
  Fahrenheit, any entry from just above 0 °F up to 32 °F was quietly filed as "not measured" —
  the day saved without a word, the reading gone — because the "no measurement" test ran on the
  converted Celsius value instead of on what was typed. The test now runs in the account's own
  unit, so 0 or less still means "not measured" and anything above it is judged against the
  physiological range and refused if it falls outside.
