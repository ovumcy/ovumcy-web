### Added

- **Calendar feed: two more choices per link, both off by default.** When you generate or rotate a
  calendar feed link you can now ask it to show each estimated period as the days it is expected to
  last, instead of the start day alone, and to add the estimated fertile window. A period appears
  as several days only once at least one cycle is complete and a period length is your own (a day
  past the first that you marked or saved yourself, not one the automatic fill wrote); before that the feed keeps sending
  the start day. The fertile window follows the same rules as the calendar page: it needs three
  completed cycles, covers the running cycle and the next one only, and is left out whenever the
  app withholds fertility estimates. Each fertile-window event says, under the medical-safety
  sentence, that pregnancy is possible on any day, including outside those dates. Like the
  detailed-titles choice, both belong to the link: turning the feed off, rotating it under other
  choices, a password reset or clearing your data resets them. Database migration 045 adds the
  two columns; a link created before it sends single days as before.
