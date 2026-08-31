### Fixed

- **The startup pass that repairs the learned luteal phase no longer counts a cycle start you
  recorded ahead of time.** Ovumcy lets a cycle start be logged up to two days in the future, and
  the personalised luteal phase is learned from completed cycles. The one-shot repair that runs at
  startup read the whole stored history with no upper bound, so a start recorded for tomorrow was
  taken as the end of the last completed cycle and the stored value was derived from a day that had
  not happened yet.

  Every surface that shows a prediction re-derives this value over a window that stops at your own
  today, so the stored one and the live one disagreed by construction — which is the drift this
  repair pass exists to remove, not to introduce. The pass now bounds its read at the same today, in
  the account's own timezone, exactly as every other surface does. A start already in the past
  counts as it always did.

  The stored value is a fallback rather than a dead cache: predictions fall back to it whenever the
  live inference has too little history to answer (fewer than three recorded cycle starts, or fewer
  than two usable ovulation signals), including on the dashboard and in the outbound webhook
  reminder. On those accounts the future-dated start reached what was actually shown.
