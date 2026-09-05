### Fixed

- **A temperature shift landing after the expected next-period date is now confirmed everywhere,
  not only on the stats chart.** The calendar's solid marker, the dashboard's ovulation line and the
  JSON API stopped confirming a late shift because the shared detector's window was cut off at the
  model's own projected next period start; the chart used a different, correct window and kept
  showing it. All four surfaces now name the same confirmed day. Suppression (overdue cycles,
  unpredictable mode, pregnancy pause, the first-cycle floor) is unchanged.
