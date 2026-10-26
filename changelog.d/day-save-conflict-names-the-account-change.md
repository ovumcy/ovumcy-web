### Fixed

When a day save met a conflict and another account had signed in in the same
browser since the page was rendered, choosing "keep mine" or "take theirs" on
the dashboard or in the calendar editor showed "couldn't save, try again",
which no retry could make true. It now shows that the account changed, as the
other day reads already do, and the entry stays in the form. The
account-changed notice on either day form also stays until the next save
starts: an earlier save's own confirmation or failure, or the answer of the
cycle-start control beside the form, no longer replaces it when it arrives
second, and a confirmation's timed dismissal no longer takes it down.
