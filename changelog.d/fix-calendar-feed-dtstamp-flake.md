none

Test only: the calendar-feed timezone-signal test compares feed bodies with DTSTAMP
lines removed, so two requests straddling a second boundary no longer fail it.
