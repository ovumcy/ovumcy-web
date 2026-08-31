### Fixed

- **The calendar's confirmed ovulation day now falls on the day the temperatures point at.** When
  basal body temperature is tracked and the recorded readings show a sustained thermal shift, that
  shift confirms an ovulation that has already happened — it says which day it was, and it never
  predicts a future one. The stats chart has always marked that day. The month grid did not: on a
  detected shift it kept its solid ovulation marker on the day the cycle model had projected before
  any temperature arrived, and the detector's answer was read only as a yes-or-no. The two surfaces
  therefore named two different days for one shift whenever the observation and the projection
  disagreed, which is the ordinary case rather than an unusual one — a cycle that ovulates later
  than its own average is exactly the cycle a thermal shift is being read for.

  The grid now moves the marker to the day the detector named and takes it off the projection it
  supersedes; the fertile-window shading around the projected day is unchanged, since that window
  remains a forecast and never claimed to be confirmed. The behaviour when no shift is found is
  unchanged too: the projected day stays, drawn as tentative. One detector, one day, on the calendar
  and in the chart alike.
