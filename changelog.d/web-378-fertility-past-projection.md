### Fixed

- **No ovulation estimate is named from a period that is already due.** Next-period dates are
  projected from the median cycle length, while the out-of-date notice measures the average. On a
  history with one long cycle (27, 28, 28 and 36 days) the period was due on cycle day 29, but the
  dashboard, the API, the calendar, the `.ics` feed and webhook reminders still named an ovulation
  and a fertile window for the cycle that period would have opened — a date built on a start that
  has not happened. From the day the projected period is due, the fertility half is now withheld on
  every surface, with the late-cycle notice explaining why; the next-period estimate and the
  out-of-date notice are unchanged. Irregular range mode is not affected.

### Changed

- **API:** `GET /api/v1/stats/overview`: `suppression.reasons` can carry a new value,
  `cycle_past_projection_length`, on the days described above (`cycle_data_stale` stays false there).
  Clients that reject unknown enum values need to accept it.
