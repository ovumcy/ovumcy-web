### Fixed

- **A pregnancy-test result refused on a future day now says so.** Saving a test result on a day
  more than two days ahead is refused, but the message spoke of marking a cycle start, which the
  save did not ask for. It now explains that a pregnancy-test result can be recorded only for past
  days and up to two days ahead — in the day editor with or without JavaScript, and as its own
  error key (`invalid pregnancy test day`) from `PUT` and `PATCH /api/v1/days/{date}`. A period
  saved that far ahead keeps the cycle-start message.
