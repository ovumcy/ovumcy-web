### Fixed

- **A day page left open no longer overwrites what another device saved since.** The dashboard
  journal and the calendar day editor saved the whole form, so a stale tab erased a period mark,
  flow or note written elsewhere and still answered "Saved". They now send `PATCH
  /api/v1/days/{date}` with only the fields the owner changed, each beside the value the page was
  rendered with (`base_<field>`); fields the save does not name keep their stored value, and
  turning the period off is sent as an explicit `is_period=false`. When another device changed
  the same field, the save is refused with `409` (`day changed elsewhere`) and the page offers
  "Keep mine" or "Take theirs". A stored value already equal to the value being written is not a
  conflict, so a re-sent save that already landed still succeeds. `PUT` is unchanged.
- **A partial save on the onboarding start day keeps its period tick.** A `PATCH` to a date with no
  saved entry that holds the stored onboarding start reads that day as the period day the pages
  show: a save that leaves `is_period` out records the period beside the changed field instead of
  a non-period entry, and an explicit `is_period=false` withdraws the start.

### Added

- `PATCH /api/v1/days/{date}` accepts an optional `base_<field>` for each day field and answers
  `409` when a based field was changed concurrently; a field sent without a base is written as
  before.
