### Fixed

- **The stats page counts completed cycles the way the dashboard does.** The insights gate and the
  "n / 3" progress line on `/stats` used to count the gaps between cycle starts before today, so on
  the day a new period start was logged the dashboard already counted the cycle it closed while the
  stats page still waited one cycle longer. Both pages now read the same count. The trend chart keeps
  its own series.
- **No sentence names the three-cycle threshold as a literal any more.** The empty-state body and
  hint on `/stats`, the "waiting for more cycles" and irregular-mode explainers on the dashboard,
  calendar and stats page, and the dashboard's next-period, ovulation and fertile-window captions
  take the number from the one threshold constant, in all six languages.
- **A late cycle on one or two completed cycles no longer repeats itself on `/stats`.** The
  late-cycle card ("a personal range needs more completed cycles") now stands alone; the
  "fertile details wait for 3 completed cycles" explainer beneath it is shown only while the cycle is
  not late.
