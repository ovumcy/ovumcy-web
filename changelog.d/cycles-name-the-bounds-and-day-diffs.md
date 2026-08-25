none

Internal hygiene in the cycle services, with no user-visible change: the three
day-difference sites in `cycles.go` now measure calendar days through
`CalendarDaysBetween` instead of an hour difference, and a new barrier refuses
that shape anywhere in shipped Go outside `CalendarDaysBetween` itself. The
calendar's projected-cycle loop now refuses a non-positive cycle-length step
instead of trusting its callee for one, which is what the baseline projection
already did; production stats cannot produce such a step, so nothing that
renders today changes. The plausible-luteal ceiling used by the
observed-luteal inference is a named constant beside its floor; a
production-unreachable location fallback in the calendar month clamp is gone
with the white-box test that was its only caller; the recent-cycles builder
sorts a copy rather than its caller's slice; and the two arithmetically
unreachable returns in `CalcOvulationDay` carry the `codecov:ignore` marker and
the invariant that makes them unreachable, matching the sibling return below
them. Every prediction, projected date and rendered cycle length is unchanged.
