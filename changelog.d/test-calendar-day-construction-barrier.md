none

Test-only barrier: the suite now fails when shipped Go code builds a calendar
day as an instant in a non-UTC location, or orders a location-midnight value
against a UTC-midnight one, outside the single sanctioned construction point.
No user-visible change — the three sites the sweep reports on this tree are
carried in the test's allowlist with the reason each is tolerated.
