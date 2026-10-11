### Fixed

- **With automatic period fill on, ticking a later day of a period you are still logging now fills the
  days you skipped.** The fill stopped at today when you marked day 1, so marking day 4 later left days
  2 and 3 empty and each had to be ticked by hand. Now, when the day you tick lies within your period
  length of the period's first day, the days between your last period day and the one you ticked are
  filled as period days, and the fill that follows the ticked day stops at the period's own last day.
  A day that already has an entry of any kind stays exactly as you saved it, an un-ticked day or a day
  with only a mood or note included, and nothing is filled past today. Un-ticking the day later does
  not remove the days that were filled; you un-tick those yourself. With automatic period fill off,
  nothing changes.
