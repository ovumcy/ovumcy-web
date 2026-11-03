### Fixed

- **The day summary prints a temperature in the interface language's number format.** In Russian
  with Fahrenheit, the read-only summary read "98.60 °F" while the stats page read "98,6"; it now
  uses the same decimal separator and one decimal as stats. The temperature field in the day
  editor and on the dashboard shows the same separator too (98,6, not 98.60) and keeps it after you
  leave the field; a comma or a dot is accepted either way.
- **The language picked on the registration page is stored when the account is created.** It used
  to be stored only when onboarding completed, so signing in again before then showed English.
