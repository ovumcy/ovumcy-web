### Fixed

- **A temperature shift landing exactly on the required margin above the coverline is no longer
  dropped.** The BBT ovulation detector confirms a shift once the third elevated day reaches at
  least 0.2 °C above the coverline, but comparing that margin as a floating-point number could
  reject a reading sitting exactly on it — a coverline of 36.2 °C plus 0.2 rounds to a value a
  hair above 36.4 in float64, so a third day recorded at precisely 36.4 read as short of the
  margin. Readings entered in Fahrenheit hit the same boundary after conversion. The comparison
  now runs in the same integer units a saved reading is rounded to, so an exact 0.2 °C shift is
  confirmed regardless of unit or magnitude.
