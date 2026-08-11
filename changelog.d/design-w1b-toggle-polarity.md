### Changed

- **Every tracking toggle in settings now reads "Show ...".** The intimacy, cycle-factor and notes
  rows used to be phrased as "Hide ...", so an enabled switch meant *visible* on some rows and
  *hidden* on others. All three are now positive and start checked, matching the fields that are
  visible by default; their saved state is unchanged by the upgrade, and the copy is updated in all
  six interface languages. The settings form posts the new `show_sex_chip`, `show_cycle_factors` and
  `show_notes_field` fields; the `/api/v1/users/current/tracking` JSON body keeps its published
  `hide_*` fields, so scripted callers need no change.
