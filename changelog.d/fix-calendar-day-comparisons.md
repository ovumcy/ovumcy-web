### Fixed

- **Predictions no longer jump a whole cycle, or drop a day's own entry, outside
  UTC.** On the estimated ovulation day itself, accounts west of UTC saw the
  dashboard, the calendar, the reminder and the `.ics` feed skip that estimate and
  advertise the next cycle's instead, and the feed lost its ovulation event for
  the day; accounts east of UTC had today's entry ignored when deciding whether a
  temperature shift had been observed. The Insights cycle stack also left the
  first or last fertile day unshaded, depending on the time zone, and never marked
  the peak day at all.
