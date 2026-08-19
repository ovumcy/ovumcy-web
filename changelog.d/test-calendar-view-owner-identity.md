none

Test-only guard: the calendar-view doubles now capture the owner argument they
used to discard, and the render test asserts that the day read and the cycle-stats
build each receive the same nonzero authenticated owner id. No user-visible change
— the render path already passed the owner correctly; until now nothing could
observe it, so a regression that swapped the owner for a constant stayed green.
