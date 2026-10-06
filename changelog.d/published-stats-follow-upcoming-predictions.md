### Fixed

- **`GET /api/v1/stats/overview` no longer publishes an ovulation day that has already passed.** Once
  the running cycle's projected ovulation was behind today, the dashboard, the calendar and the
  `.ics` feed rolled it to the next cycle while the API kept naming the passed day, flagged exact,
  with the fertile window around it. `ovulation_date`, `ovulation_exact`, `ovulation_impossible`,
  `fertility_window_start`/`fertility_window_end` and `next_period_start` now come from the same
  projection the pages use, so the API names the same days they do. In irregular-cycle mode, where
  the dashboard shows the running cycle's ovulation range, the API keeps that cycle's day and
  widened window. A BBT-confirmed ovulation and every suppression rule are unchanged.
