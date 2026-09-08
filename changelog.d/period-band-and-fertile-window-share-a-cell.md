### Fixed

- **A day that is both an expected period day and a fertile day now says so.** On a short cycle with
  a long average period the projected period band and the fertile window cover the same calendar
  days — bleeding inside the fertile window is ordinary rather than an anomaly, and both statements
  about such a day are true at once. The month grid paints one fill per cell, and the band's fill
  outranked the window's, so those days were shown as expected bleeding only and the fertile window
  appeared shorter on the calendar than it actually was.

  Those cells now carry a fill of their own — the two hatches woven together, dashed like the
  projected band and drawn in the fertile ink — with its own entry in the calendar legend. The days
  the next period may start on get the same fill with their usual dotted stroke, so the window is
  drawn without a gap where the start window crosses it. The fill follows the calendar's goal
  colours, so it warns or encourages exactly as the rest of the window does. Nothing is hidden to
  make room for it: the day remains both an expected period day and a fertile day everywhere else it
  is read, and every cell that carries only one of the two keeps exactly the fill it had.
