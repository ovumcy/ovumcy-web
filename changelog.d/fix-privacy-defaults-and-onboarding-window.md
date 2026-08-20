### Changed

- **Automatic period fill starts off on a new account.** `SECURITY.md` has stated auto-period-fill
  as an off-by-default privacy control since the GDPR cross-reference was written, while every
  producer set it on: the column default, both account constructors, and the reset that runs inside
  clear-data. A fresh account now records only the days its owner entered, and the onboarding toggle
  is where inference gets turned on. **Existing accounts are not migrated** — no stored value is
  rewritten, so an owner who is using auto-fill keeps it; the change governs accounts created from
  here on, plus the two paths below.

### Fixed

- **Onboarding accepts the whole window its own message promises.** The lower bound of the
  last-period-start field was raised to 1 January of the current year whenever that was the later
  date, so on 1 January the accepted window was a single day, and stayed shorter than the promised
  60 through January and February — while the rejection read "Choose a date within the last 60
  days" and onboarding refused to finish without a date. The only way out was to enter a date the
  owner knew was wrong, on the field that anchors the first cycle and every estimate built on it.
  The window is now the last 60 days on every calendar date, and it crosses into the previous year
  when it has to. The date picker offers the same range, because both read the same bound.

### Security

- **Clearing your data no longer re-arms automatic period fill.** The reset written inside the
  erasure transaction set auto-period-fill back on, so an account wiped by an owner who had turned
  inference off resumed manufacturing period days the owner never logged. Erasure has to hold
  across the transitions that follow it; the wiped account now starts from the same default a new
  one does.

### Internal

- **The GDPR cross-reference claimed the opposite of what the code does, and its cited test could
  not tell.** The Art. 9(2)(a) row said deployments "rely on operator-captured consent" and that the
  codebase "exposes no third-party transmission" with "no external network calls" as the control.
  Registration refuses an account whose consent field is falsy, and two paths carry health-derived
  data off the instance: webhook reminders to the endpoint an owner configured, and the read-only
  `.ics` calendar feed, whose subscriber is commonly a third-party calendar service receiving that
  owner's predicted days. The row now states the consent gate and both egress paths with the
  containment each has, and cites tests that can observe them. The security-document pair guard
  grows past the webhook rows to hold it there: consent capture, the egress set and the
  privacy-by-default settings are read out of the code that decides them — the feed's from the
  route that mounts it — and the table is held to what they say, in both directions, so naming one
  egress path no longer passes for naming them all.
- **The one premise behind an accepted schema divergence is now asserted.** Migration 035 moves the
  `auto_period_fill` column default on PostgreSQL; SQLite cannot restate a default without
  rebuilding the account table, which is the parent of every foreign key in the schema, so the old
  literal stays and is harmless only because every insert path names the column. A test inserts a
  row that omits it on each engine and pins what each stores, so a change that starts relying on
  the column default fails on a test instead of on an owner's data.
