### Fixed

- **A day save no longer answers "changed on another device" after two of its own saves cross.** When
  an autosave and a Save of the same day were in flight together and the older one answered last, the
  page took that older answer as what the server held, and the next edit was refused as a conflict.
  The page now keeps the newest accepted save as its base, whichever answer arrives last.
- **A confirmed cycle start is sent with the save that confirmed it, not with every later save.**
  Ticking "this starts a new cycle" used to ride along on each later edit of that day, so an unrelated
  change could mark the day as a cycle start again after it had been un-marked.
- **A day save reads the onboarding period start inside its own write.** A mood or note saved on the
  onboarding start date, with no entry for that day yet, could turn it into a period day from a start
  Settings had just withdrawn; the start is now read in the same transaction as the day it shapes.
