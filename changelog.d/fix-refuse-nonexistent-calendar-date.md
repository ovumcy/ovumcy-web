### Fixed

- **A date your time zone never had is now refused instead of quietly saving the
  day before.** A zone that crosses the date line skips a whole calendar day
  (Pacific/Apia had no 2011-12-30, Pacific/Kiritimati no 1994-12-31). Entering
  one used to parse as the previous day and report success, so the save, the
  onboarding anchor or the imported row landed on a day nobody named. Date input
  now reports it as invalid, on the same validation path an empty or malformed
  date already takes; an import counts such a row as rejected and imports the
  rest of the file. Dates that do exist are unaffected — a day whose midnight a
  daylight-saving jump skips still resolves to that day's first real instant.
