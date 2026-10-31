### Fixed

The calendar no longer shows a fertile window or an ovulation day on cycles
projected far ahead. A regular account with enough history saw full fertile
shading and ovulation markers on every chained cycle, as far as three years out,
and the `.ics` feed sent an ovulation event for two cycles past the running one.
Each projected cycle adds its own spread to the one before, so a few cycles out
the estimate is wider than the window it names. Fertility is now shown on the
running cycle and the next one only — on the running cycle only in irregular
range mode — on the calendar and in the feed alike. Predicted periods still
cover the whole calendar.
