### Internal

- **The auth-policy and data-handling concern docs stop misdescribing the code.** Accuracy pass
  from the 2026-08-28 documentation audit. `auth-policy-and-rate-limits.md`: the recovery-code
  offline-guessing bound no longer names `SECRET_KEY` — the hash is plain bcrypt and verification
  compares it directly, so a database leak on its own already permits offline candidate testing
  (regenerate outstanding codes after any database compromise); the settings re-auth budget's
  enumeration gains the two password-gated actions it omitted (`PUT …/2fa` enrollment
  confirmation, `POST …/recovery-code` regeneration); the single-document-era pointers ("see
  *Cookies* above", *Session Invalidation*, *Field-Level Encryption*) become links to the sibling
  concern docs they moved to. `data-handling.md`: the canonical egress statement now covers the
  pull-based `.ics` feed alongside the push integrations; the `users`-table inventory gains the
  migration-036 reveal consumption marks (`recovery_code_revealed_at`,
  `calendar_feed_revealed_at`) and the clear-data contract records two facts the code already
  held — the wipe blanks the stored `timezone`, and the reveal marks deliberately survive it;
  the *Logging Policy* and *Field-Level Encryption* pointers become file links. No behaviour
  changes.
