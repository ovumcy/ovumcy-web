### Fixed

- **A temperature shift landing after the expected next-period date is now confirmed on every
  screen, not only on the stats chart.** The calendar's solid marker, the dashboard's ovulation line
  and the JSON API stopped confirming a late shift because the shared detector's window was cut off
  at the model's own projected next period start; the chart used a different, correct window and
  kept showing it. Whenever a confirmed day may be shown at all, the four now name the same one.
  Suppression (overdue cycles, unpredictable mode, pregnancy pause, the first-cycle floor) is
  unchanged, and so are the webhook reminder and the calendar subscription.
