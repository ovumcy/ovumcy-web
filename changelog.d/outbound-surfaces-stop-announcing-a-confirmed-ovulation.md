### Fixed

- **A subscribed calendar and a webhook reminder no longer announce an ovulation your temperatures
  have already placed.** When a sustained thermal shift is recorded, the dashboard, the month grid
  and the stats chart all move onto the day the temperatures name. The two surfaces that leave the
  instance did not: the `.ics` feed kept publishing an ovulation event on the projected day, and the
  reminder sent to your webhook endpoint kept counting down to it.

  Both exist to announce a day that is still ahead, so neither could simply be moved onto a day
  already behind you. They now stay quiet for that cycle instead. The gap was widest exactly where
  it mattered: on the projected day itself, the app on screen said the ovulation had happened three
  days earlier while a calendar you had subscribed to marked it as today.

  Only the cycle the shift belongs to is affected. The next projected cycle's ovulation event and
  its reminder are unchanged, as are every period event and period reminder, and a cycle with no
  thermal shift recorded behaves exactly as before. The suppression rules already in force —
  predictions turned off, a pregnancy pause, an overdue cycle, or no completed cycle yet — are
  untouched, and both surfaces still read them from the same place every other surface does.
