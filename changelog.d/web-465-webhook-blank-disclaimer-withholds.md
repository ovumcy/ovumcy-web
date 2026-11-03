### Fixed

- **A webhook reminder no longer leaves without its medical-safety disclaimer.** When the disclaimer
  copy resolved to an empty string, the reminder was still sent with a blank `disclaimer` field. The
  notify pass now withholds that owner's reminders, claims no watermark and logs the reason, the same
  rule the `.ics` feed applies; the reminder goes out on the first pass after the copy resolves. The
  payload shape is unchanged.
