### Security

- **Day, symptom and sign-in-identity rows are refused at write time when they name no owner.**
  `DailyLogRepository.Create`/`CreateBatch`, `SymptomRepository.Create`/`CreateBatch` and
  `OIDCIdentityRepository.Create` now return an error instead of writing a row that no
  owner-scoped read and no account erasure could ever reach again. The symptom rows seeded while
  a new account is created run the same check, so the registration path is covered along with the
  repository's own inserts. Onboarding's day-completion update is now also scoped by owner in its
  query, matching the rest of the day-write path, and `CompleteOnboarding` itself refuses a zero
  owner rather than reporting success for writes that touched nothing.
