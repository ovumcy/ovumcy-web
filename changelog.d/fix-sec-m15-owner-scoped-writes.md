### Security

- **A day entry, custom symptom, or OIDC last-used stamp saved with no owner now refuses instead of
  silently doing nothing.** `DailyLogRepository.Save`/`UpdateSymptomIDs` and `SymptomRepository.Update`
  scope their write by the row's own `user_id` in the query — the guard that stops one owner's write
  from overwriting another owner's row — but a `UserID` of zero was never checked: `WHERE user_id = 0`
  ordinarily matches no row at all, so the write returned success having touched nothing. All three now
  refuse a zero owner with an error. `OIDCIdentityRepository.TouchLastUsed` gained the same owner-scoped
  `WHERE`, previously matching by identity id alone, so a stale or foreign identity id could stamp
  `last_used_at` on a row it does not own; the method now takes the caller's `userID` and combines it
  with the identity id before writing, matching zero rows for a foreign id instead of touching the wrong
  owner's row.
