### Fixed

- **A late period no longer sends the calendar and the `.ics` feed a month ahead.** Once a cycle had
  passed its usual length without a new period logged, the dashboard and the API kept naming the
  period that was due, while the calendar grid and the subscribed feed went on drawing the next
  cycles as if that period had started on time — two answers to "when is my next period" on one
  account. From the day after the usual length until predictions pause a week later, every surface
  now names only the period that is due, and nothing is projected from a start that has not been
  logged.
- **On the day a period is due, the dashboard no longer calls the cycle "1 day longer".** The
  excess over the usual range is now counted in whole days the cycle has run, so the notice starts
  the day after the predicted start.
