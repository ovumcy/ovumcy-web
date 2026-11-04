### Fixed

- **A bleeding day logged in the owner's own morning withholds the fertile window on every surface.**
  For an owner whose timezone runs ahead of the server's, the cycle derivation took "today" from
  the server's calendar day and dropped the owner's log for their own today. Bleeding logged
  outside a recorded cycle start that morning then failed to withhold the projected fertile window
  and ovulation, so the `.ics` feed and the webhook reminder pass could still announce them for
  a day recorded as bleeding. The derivation now takes the owner's day from the owner's timezone.
