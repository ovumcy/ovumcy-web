### Security

- **The migration refusal over duplicate rows no longer prints the conflicting keys.** When a
  migration adds a unique index to a table that already holds colliding rows, the refusal ends the
  boot and is logged on every restart. It used to spell out the keys — an owner id with symptom
  names, or an email address. It now reports only the table, the index and how many conflicting
  groups there are (capped at "more than 5"); the rows themselves are read offline with
  `ovumcy repair`, as the runbook describes. No migration SQL or numbering changed.
