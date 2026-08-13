### Added

- **The BBT chart reads back.** Pointing at the chart on Insights draws a hairline at the cycle day
  under the pointer and names it: the day, its date, and the reading with its unit — or that the day
  has no reading, rather than borrowing a neighbour's. It works by tap as well as by hover, and
  closes on Escape, on a tap elsewhere, or when the pointer leaves.
- **Every reading is also text.** A disclosure under the chart opens a table of the same series —
  cycle day, date, temperature — so no value is reachable by hovering only. The drawing itself stays
  hidden from assistive technology; the table is its equivalent.

### Fixed

- **The coverline is painted for the theme it is on.** `--chart-baseline` had no definition in either
  theme: both the stylesheet and the chart script fell back to the same light-theme colour, so the
  dark card drew its reference line and probable-ovulation marker in a light-theme neutral. The token
  now carries a value per theme, each measured against the surface it is drawn on.
