### Fixed

- **Dates counted forward or back from today no longer slip a day on a clock-change date.** In the
  timezones west of UTC whose daylight-saving jump happens exactly at midnight — Santiago on
  6 September 2026, Havana on 8 March 2026 — that local midnight does not exist. Counting a number
  of days inside such a timezone and landing on one of those dates quietly returned the day before
  it, at 23:00. Ovumcy counted days that way in nine places, and each of them named the wrong day
  once a year for owners in those zones:

  - the projected ovulation and fertile window behind every predicted date on every surface;
  - the chain of future cycles painted on the month grid, which shifted a whole projected period
    one day earlier;
  - the events published to a subscribed calendar app through the `.ics` feed;
  - the rule allowing a cycle start to be recorded up to two days ahead, which refused a date it
    was meant to accept;
  - the earliest date the onboarding and settings date pickers offered;
  - the ninety-day window behind the cycle-factor context on the stats page, and the dashboard's
    look at yesterday.

  All nine now count through one shared step that does its arithmetic on dates rather than on
  clocks, and then resolves the answer back into the owner's own timezone. Nothing changes on any
  other date or in any other zone.

### Internal

- **The barrier that guards this class now reads the third shape it comes in.** A sweep over the
  shipped source already failed the suite for building a calendar day as an instant in a timezone,
  and for ordering a location-anchored day against a UTC-anchored one. It did not read the shape
  that produced the fixes above — a day STEPPED from a location anchor — and its own classifier
  asserted that such a step "keeps the anchor", which is untrue on exactly the dates that matter.
  The sweep now flags the stepping shape, carries a floor so it cannot pass by reading nothing, and
  states in its blind-spot list that it only sees a step whose anchor is built in the same function.
  One site is allowlisted with its reason: a step of whole years producing the lower bound of a log
  fetch window, where the one-sided slip widens the window by a day and never narrows it.
