### Fixed

- **The spring-forward date can now be entered in time zones whose clock jump
  lands on midnight.** In zones west of UTC where daylight saving starts at
  00:00 (America/Santiago, America/Havana), local midnight does not exist on
  that date, and every date input resolved it one calendar day backward: the
  day picked was stored, shown and exported as the previous day, and an
  onboarding cycle start recorded on it anchored every later prediction to the
  wrong day. Calendar days now resolve to the first instant that really exists
  on the requested day. Zones without such a transition, including UTC, are
  unaffected.
