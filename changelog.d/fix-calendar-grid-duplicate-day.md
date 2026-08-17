### Fixed

- **The calendar grid no longer prints one day twice and drops the last one in
  time zones whose clock jump lands on midnight.** In zones west of UTC where
  daylight saving starts at 00:00 (America/Santiago, America/Havana), local
  midnight does not exist on the transition date. The grid stepped from day to
  day as an instant, so that missing midnight resolved one calendar day
  backward: a month whose displayed range crossed the transition rendered the
  preceding day in two cells and never rendered the final day of the range at
  all — the cell for it was the duplicate. The grid now counts and steps its
  cells as calendar days, so every day in the range appears exactly once, in
  order. Zones without such a transition, including UTC, render identically to
  before.
