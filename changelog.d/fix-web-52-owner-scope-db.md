### Security

- **Every day, symptom, and sign-in-identity row now carries an owner from the moment it is
  written.** `DailyLogRepository.Create`/`CreateBatch`, `SymptomRepository.Create`/`CreateBatch`,
  and `OIDCIdentityRepository.Create` refuse a zero owner instead of writing a row that no
  owner-scoped read and no account erasure could ever reach again. Onboarding's day-completion
  update is now also scoped by owner in its query, matching the rest of the day-write path, and
  `CompleteOnboarding` itself refuses a zero owner.
