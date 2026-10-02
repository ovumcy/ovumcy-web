### Fixed

- **The irregular-cycle ovulation range on the dashboard is no longer a day late.** The range
  was the next-period range shifted back by the luteal phase, which puts ovulation one day after
  where the cycle model — and therefore the calendar, the `.ics` feed and the reminders — places it.
  With a 24-day shortest and a 45-day longest cycle from 1 March the dashboard showed 11 March to
  1 April while the model gives 10 to 31 March, so the earliest possible ovulation was named a
  day late. Both ends now come from the same predictor as every other surface.
