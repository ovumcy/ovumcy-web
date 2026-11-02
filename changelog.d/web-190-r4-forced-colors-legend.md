### Fixed

- **Primary buttons keep a frame in forced-colors mode, the success toast is opaque, and pre-fertile
  calendar days have their own legend row.** In a high-contrast theme a primary button had no border
  and read as bold text; it now draws a `ButtonText` frame. The success toast background was about
  one third opaque, so page text showed through it; it is now a solid surface in both themes with
  the same readable text colour. A pre-fertile calendar day used to share its dashed outline with a
  predicted period day and had no legend entry; it is now a solid line with an inner ring, listed in
  the legend as "Days before the fertile window (estimate)" (all six languages) and named in the
  day's accessible name, and in forced-colors mode it no longer looks like a recorded period. A day
  save that fails or is refused (a conflict, an expired session, a stale token) is scrolled into
  view on a desktop screen instead of appearing below the fold.
