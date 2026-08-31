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

  The line now names the confirmed day, and — because that day is usually already behind you — it is
  shown as past rather than announced as upcoming. Both surfaces resolve the day through one shared
  reader, so a single shift can no longer produce two dates. Nothing here changes *whether* an
  estimate is shown: an account with predictions turned off, a paused pregnancy estimate, or an
  overdue cycle withholds the ovulation estimate exactly as before, and a recorded shift is not a way
  around that. With no shift recorded, the projected day is shown unchanged.
