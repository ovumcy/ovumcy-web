### Fixed

- **The dashboard, the stats page and the day-save message now read today's fertility status and
  phase against the same window as `GET /api/v1/stats/overview`.** Once the running cycle's
  ovulation was behind today and the window rolled to the next cycle, the API and the calendar
  called today fertile while the dashboard header and the stats page still showed "luteal" and
  "outside the estimated window", and the save message stayed neutral. They now follow the rolled
  window: `fertile` with no phase named where it covers today, and `unknown` for both where the
  rolled day falls past 9999-12-31. Pregnancy pause, unpredictable-cycle mode, suppressed
  predictions and out-of-date cycle data are unchanged.
