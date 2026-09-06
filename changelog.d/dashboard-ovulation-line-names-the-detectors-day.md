### Fixed

- **The dashboard's ovulation line now names the day your temperatures point at.** When basal body
  temperature is tracked and the readings show a sustained thermal shift, that shift confirms an
  ovulation that has already happened and says which day it was. The month grid and the stats chart
  both name that day. The dashboard line did not — it kept naming the day the cycle model had
  projected before any temperature arrived.

  The two disagreed most visibly on the projected day itself. The line rolls its estimate forward to
  the next cycle only once the projected day is behind you, so on that day it stayed put and
  announced an ovulation as happening now, while the calendar beside it marked the same ovulation
  several days earlier. For someone reading the two together to time anything, that is the
  difference between "today still counts" and "this window has closed".

  The line now names the confirmed day, which is usually already behind you, instead of announcing
  one as upcoming. It does not raise the "date is already in the past" notice while doing so: that
  notice is about an estimate the app is still pointing at after its day has gone by, and a confirmed
  thermal shift being behind you is simply what the rest of a cycle looks like. Both surfaces resolve
  the day through one shared reader, so a single shift can no longer produce two dates.

  A confirmed day also outranks the two ways the dashboard expresses how uncertain a *projection* is:
  it is named as a day rather than widened into the range shown for irregular cycles, and it is not
  withheld under "needs more cycles". Neither of those is about a day the temperatures have already
  pointed at, and both used to hide it while the calendar went on marking it — for irregular cycles,
  the very accounts the model serves worst.

  One thing does stop appearing, and that is the point of the change: the dashboard's "ovulation is
  coming" reminder no longer counts down to a day the temperatures have already placed behind you.
  It counted down to the projection, so on the projected day it announced an ovulation as arriving
  today while the calendar beside it marked the same one several days earlier. With no shift
  recorded it counts down exactly as before.

  Nothing here changes *whether* an estimate is shown. An account with predictions turned off, a
  paused pregnancy estimate, an overdue cycle, or an account that has not completed a single cycle
  yet withholds the ovulation estimate exactly as before, on the dashboard and the calendar alike —
  they now read that decision from one place instead of each applying it separately. With no shift
  recorded, the projected day is shown unchanged.
