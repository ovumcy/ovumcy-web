none

Internal only: three `internal/services` declarations the application could not
reach are resolved so the production-scoped `deadcode` run stops reporting them.
The two calendar-navigation wrappers are gone and their tests now drive the
bounded forms the calendar page and the calendar view service actually call; the
runner-less `DayService` constructor moved into a test file, since production
always builds the service with a transaction runner. No behaviour change.
