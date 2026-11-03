### Changed

**Breaking (API shape): a future-day cycle start no longer replaces the saved start.**
Marking a day after today as the cycle start, inside the period whose saved
start it would replace, is now refused with `cycle start mark on the day`
(400) whatever `replace_existing` says, and the calendar's day editor shows a
"mark it on the day" notice instead of the cycle-start button. Marking that
day once it arrives works as before, and a future mark next to any other
logged start keeps the ordinary replace confirmation.
