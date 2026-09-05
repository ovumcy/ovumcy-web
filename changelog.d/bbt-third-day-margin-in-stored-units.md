### Fixed

- **A temperature shift landing exactly on the required margin above the coverline is no longer
  dropped.** The basal body temperature detector confirms a shift once three consecutive days sit
  above the coverline and the third of them reaches at least 0.2 °C above it, but comparing that
  margin as a floating-point number could reject a reading sitting exactly on it — a coverline of
  36.2 °C plus 0.2 came out a hair above 36.4, so a third day recorded at precisely 36.4 read as
  short of the margin. Readings entered in Fahrenheit hit the same boundary after conversion. The
  comparison now runs in the same fixed-precision units a saved reading is rounded to, so an
  exact 0.2 °C shift is confirmed whichever unit it was entered in. A restored backup now rounds
  its temperature readings onto that same precision, as a day saved through the app is.
