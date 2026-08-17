### Fixed

- **A shaded range on the calendar no longer loses a day in time zones whose
  clock jump lands on midnight.** In zones west of UTC where daylight saving
  starts at 00:00 (America/Santiago, America/Havana), the builders that paint a
  predicted period, a fertile window or the pre-fertile band stepped through the
  range by adding a day to an instant, and the missing midnight resolved
  backward into the previous calendar day: the range painted that day twice and
  dropped one of its own — the transition day itself, or the range's last day.
  Ranges are now stepped and bounded by calendar day. The same change restores
  the closing day of the projected and historical pre-fertile bands, which every
  zone west of UTC lost in every month because the band's two ends were measured
  from different midnights. UTC and zones east of it are unaffected.
