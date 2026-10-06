### Fixed

- **Onboarding no longer records period days that have not happened yet.** With automatic period
  fill on, finishing onboarding marked the whole configured period length from the chosen start
  date — a period started today with a length of five stored the next four days as recorded period
  days, and the calendar painted them as logged. The fill now stops at the owner's own today, in
  the owner's timezone, on the same bound the day editor's automatic fill already used: a period
  still in progress is recorded through today, and one that ended in the past is recorded whole.
  The chosen start date is stored exactly as before. Days written by an earlier onboarding are left
  as they are.
