### Fixed

- **The 40-45 and 45+ age-group labels no longer both claim age 45.** All six locales showed
  `40-45` next to `45+`, so a woman exactly 45 literally fit either radio button even though they
  are mutually exclusive (`components/settings_cycle.html`). The only reader of the bracket,
  `shouldShowStatsPerimenopauseHint` (`internal/services/stats_page_view_service.go`), treats them
  as disjoint — it shows the STRAW+10 perimenopause note only for `age_45_plus` — so which button a
  45-year-old picked silently changed the medical guidance she saw. The AWHS cohort the bracket
  comment cites uses non-overlapping 40-44 / 45-49. Copy only: `40-45` → `40-44` (French:
  `40–45 ans` → `40–44 ans`) in `de/en/es/fr/it/ru`; the stored value `age_40_45` and the `45+`
  label are unchanged, so no migration is needed.
