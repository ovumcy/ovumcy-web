### Changed

- **Settings, the journal and Insights are shorter to read.** Related controls now share one
  surface instead of each getting its own block: the cycle length, period duration and last-period
  date sit together, the three cycle switches carry their explanation inside the control the way the
  tracking switches already did, age group and usage goal sit side by side, and the reminder lead
  time is one row with its own save. Block headings inside a section are a real step larger than the
  labels beneath them, so a section can be scanned instead of read. Helper lines that only said their
  label back are no longer rendered — the "currently visible/hidden" line under every tracking
  switch, the first-day-of-the-week hint, three webhook reminder hints, and the wrapper headings
  around the account's password and recovery-code blocks. The account email, the new password and its
  confirmation, and the re-authentication field beside its destructive button each share a row with
  what they belong to. On the dashboard the journal's title carries its date on the same line and the
  quick actions sit beside the period switch. Measured at 1280 px on a fresh account: settings
  8049 → 6407 px, the journal region 1000 → 872 px, Insights 825 → 793 px.

- **The danger zone has a surface of its own.** It has never painted one: the rule read an
  undefined token, so the section rendered as bare page. It is now a quiet warm neutral in both
  themes, with the danger signal carried by the border and the heading rather than by an alarm-red
  field, and it stays where the section index points at it.

- **Insights no longer repeats itself where a section is empty.** The cycle-trend card printed
  "Not enough cycle data yet." twice — once as its caption and once as its body — and the per-section
  empty hints were panel-sized boxes. The caption now names the window only when there is a chart to
  name it for, and an empty section shows a single-line hint; the whole-page empty state, with its
  illustration, is still the one card an account with nothing logged sees.
