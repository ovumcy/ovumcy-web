### Added

- **Owners can record an LH test result for a day.** A new "Show LH test field" switch in Settings
  (off by default) adds a none, negative or positive LH test to the day editor, the day summary and
  both exports (JSON `lh_test`, CSV column `LH test` after `Uncertain`). The result is stored and
  shown only: it does not change the predicted ovulation, the fertile window or any cycle estimate,
  and it never leaves through the calendar feed or a webhook. Turning the switch off keeps the stored
  results. An import of an older export reads the missing field as no result.
