### Fixed

- **"Mark anyway" no longer quietly does nothing.** A cycle start confirmed after the short-gap
  warning is kept as an uncertain start, and an uncertain start used to withhold the whole bleeding
  episode it sat in — so the marked day counted for less than an unmarked one. It now counts as no
  mark: the episode opens a cycle by the ordinary rules (two consecutive period days, or a single
  period day dated today or yesterday).

- **A recorded period day is never called fertile at the same time.** A period day that opens no
  new cycle — a spotting day, or a lone day once it is older than yesterday — was shown as
  "Menstrual" from the log while the running projection still called the same day fertile and
  counted down to an ovulation a few days off, on the dashboard, the statistics page, the API
  overview, the calendar and the `.ics` feed alike. On the day such bleeding is recorded and the
  day after, the fertile window and the ovulation day are now withheld on every surface, and
  `GET /api/v1/stats/overview` names the new reason `bleeding_outside_cycle_start`. The phase still
  reads "Menstrual" and the next-period estimate stays.
