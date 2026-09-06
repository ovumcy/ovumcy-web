### Changed

- **The 10–20 day luteal window is now documented as the outlier filter it is.**
  `docs/cycle-prediction.md` no longer calls the window physiological or treats a sample outside it
  as a bad reading: it filters *inferred* lengths before they are averaged, a luteal phase at or
  below its floor is ordinary rather than a fault, and a dropped sample says nothing about the
  owner's body. The doc also states that the 14-day value left standing when too few samples
  survive is the model's constant and must not be shown as a personalised luteal phase. The filter
  itself is unchanged.
