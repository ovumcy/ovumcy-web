### Fixed

- **The stats page and the dashboard now ask for the same number of completed cycles, and the calendar
  legend lists only what the grid draws.** Stats unlocked its insights after two completed cycles
  while the dashboard held its ranges back until three, so one account was told both "2" and "3".
  Stats now waits for three, with the progress bar and its "n / 3" line reading that count; the
  sentence explaining why predictions are held back now shows above that waiting state too, so an
  irregular-mode account with one or two cycles still reads it. The late-cycle notice compares
  against a personal range, and the dashboard's cycle-factor hint appears, from three completed
  cycles as well. The calendar legend used to list every projected state to every account, including
  accounts whose grid drew none of them; it now shows an entry only when the month on screen carries
  that state.
