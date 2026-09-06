### Fixed

- **A temperature shift landing after the expected next-period date is now confirmed on every
  screen, not only on the stats chart.** The calendar's solid marker, the dashboard's ovulation line
  and the JSON API stopped confirming a late shift because the shared detector's window was cut off
  at the model's own projected next period start; the stats chart used a different, correct window
  and kept showing it. Those three now name the day the chart names. The fertile window, the
  current-fertility status and the dashboard ring still follow the projection; moving them with a
  confirmed day is a separate change. The situations in which no ovulation date is shown at all —
  an overdue cycle, unpredictable mode, a pregnancy pause, the first cycle — are unchanged, and the
  webhook reminder and the calendar subscription send exactly what they sent before.
