### Fixed

- **Saving a day during a first tracked cycle no longer announces a fertile
  window.** With a single logged period start no cycle has closed yet, so there
  are no observed cycle lengths and the fertility window is the default cycle
  length projected forward with the default luteal phase — configuration, not
  anything the account recorded. The save confirmation stated it as fact ("You
  are in your fertile window right now"), which is the estimate-presented-as-fact
  the medical-safety rule forbids: where the only source is configuration
  defaults, the surface is withheld rather than qualified. The day save now falls
  back to its neutral confirmation until the first cycle closes; from the first
  completed cycle on, the fertile message is unchanged, as are the self-care,
  pregnancy-paused and unpredictable-cycle messages.

### Internal

- The day-service test doubles for the user repository now record the owner id of
  every settings read and write, and the day tests assert that every such call
  targets the acting owner. The id used to be discarded, so a settings write
  aimed at another account — the luteal phase, the period tip, the long-period
  warning — passed the whole day suite unnoticed.
