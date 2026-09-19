### Fixed

- **The stats BBT chart now shows temperatures in the account's own unit.** The chart, its axis,
  the hover readout, the coverline, the readings table and the text summary on the stats page
  always printed the stored Celsius value under a fixed `°C` label, so an owner tracking in
  Fahrenheit saw 36.5 °C on the stats page for the 97.7 °F they had logged. Every one of those
  surfaces now converts for display and names the same unit; stored readings are unchanged and
  ovulation detection still compares them in Celsius, so the detected shift does not move.
