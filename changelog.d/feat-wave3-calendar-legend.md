### Changed

- **The calendar legend leads the grid and names concepts instead of CSS states.** It sat under
  the month in the faintest text on the page, listing nine entries — one per shading variant — so
  it was off screen exactly while the grid was being read. It now renders above the weekday row,
  inside the same panel, as seven grouped entries: recorded period, predicted period days,
  predicted start window, fertile window, ovulation, today, logged entry. Each swatch is painted
  from the same definition as the day cell it explains, so the two can no longer drift apart, and
  the redundant non-colour encoding on the day cells (markers, dashes, borders) is unchanged.
- **A predicted period start is drawn differently from a predicted period day.** The dashboard's
  "Next period: 25 Aug — 27 Aug" is a range of possible START dates, while the grid shades the
  projected bleeding days; both used one shading, so a reader saw two different quantities in one
  visual language. Texture now separates fact from estimate: a recorded period day is solid, a
  projected period day is hatched, and the days the next period may start on carry a graded fill
  that fades across the window. The window is the range the dashboard already computes, so the two
  surfaces cannot disagree, and it appears only where a prediction is shown at all — an
  unpredictable cycle, a pregnancy pause or a cycle past its reference length still paints nothing.
  Day-number contrast stays above WCAG AA on every state in both themes (worst measured stop
  5.04:1).
