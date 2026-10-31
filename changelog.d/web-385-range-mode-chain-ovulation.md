### Fixed

- **Irregular range mode no longer names an ovulation from a period that is already due.** Once
  the median-projected period was due (cycle day 29 on a 24–36-day history with a median of 28 and
  an average of 30), the dashboard named only the ovulation range placed from the recorded start,
  but the calendar still drew a fertile window and the `.ics` feed still sent an ovulation for the
  cycle that missed start would have opened. Neither is drawn or sent any more; the ovulation range
  and the next-period estimate are unchanged.
