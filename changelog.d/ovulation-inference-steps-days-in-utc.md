### Fixed

- **Ovulation inferred from your own observations no longer lands a day early on a clock-change
  date.** Both signals Ovumcy reads an ovulation from count a calendar day forward from something
  already known: the thermal shift names the day before the first elevated temperature, and the
  cervical-mucus fallback names the day after the last fertile-quality day. Both counted that day
  inside the timezone the page was being viewed in. In the zones west of UTC whose daylight-saving
  jump happens exactly at midnight — Santiago on 6 September 2026, Havana on 8 March 2026 — that
  local midnight does not exist, and a count landing on it resolves backward into the day before.
  The inference then named 5 September for an ovulation it had found on the 6th.

  Two things read that answer, and on those dates both were a day out: the personalised luteal
  phase learned from past cycles, and — now that the month grid marks a confirmed ovulation on the
  day the temperatures point at rather than on the earlier projection — the solid ovulation marker
  itself. The count now runs over calendar days anchored at UTC, which no timezone shifts, exactly
  as the month grid's own bounds already do. Nothing moves on any other date, in any zone: the
  value has only ever been read as a calendar date, never as a clock time.
