### Changed

The dashboard's cycle ring becomes a cycle ribbon: one cell per day of the
cycle, carrying the phase along the band, the fertile window as a strip under
it with the ovulation day as its peak, a mark on every day that holds an entry,
and the days the next period may start on as a graded tail the ring had no way
to draw. Every suppression the status header obeys the ribbon obeys with it, so
unpredictable mode, a pregnancy pause, an overdue cycle or stale data leave the
band undrawn rather than guessed at.

### Internal

Cycle-phase colours gain a `--phase-*` token family that reuses the calendar's
own channels, replacing four one-off values the dashboard ring carried alone —
"menstrual" was a different colour on Today than on the calendar.
