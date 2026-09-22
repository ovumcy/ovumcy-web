### Fixed

- **The dashboard reminder banner no longer marks a confirmed ovulation day approximate.** The
  in-app "ovulation in ~N days" banner derived its approximate marker from
  `DisplayOvulationExact` alone, while the dashboard's own ovulation line also checks
  `DisplayOvulationConfirmed` — so a BBT-confirmed shift landing on a clamped (non-exact) luteal
  projection read as approximate in the banner while the line beside it did not. The banner now
  reads both fields, matching the dashboard.
