### Changed

- **Marking a period day now asks about the cycle start in place.** On a day where the existing
  cycle-start policy would suggest a new cycle, turning the period toggle on reveals one calm
  question next to it — "Start a new cycle from this day?" with yes and no as small controls —
  instead of leaving the answer to the separate "Mark new cycle start" action further down the
  page. The answer rides the same save as the entry: nothing is recalculated or written before
  that save, an untouched question writes nothing, and declining leaves a plain period day. The
  separate control stays where it is for corrections, and the hint that used to point at it steps
  aside while the question is on screen.
