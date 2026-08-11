### Fixed

- **The phase label no longer alternates between two vocabularies.** On the same day the dashboard
  could say "Follicular" while Insights said "Current phase: Fertile" — cycle phase (menstrual /
  follicular / ovulation / luteal) and fertility status (inside the fertile window or not) are two
  different facts, and each screen answered with whichever one it computed. The phase slot now
  always carries one of the four phases, and the fertile window shows as its own "Fertile window"
  note beside the phase on the dashboard and on Insights, read from the same window bounds the
  calendar already shades — so the surfaces cannot disagree again.
