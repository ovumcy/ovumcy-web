### Security

- **A zero owner id could no longer be told apart from a real one on the users table.**
  Every UPDATE against a user's row was scoped by `WHERE id = ?` with no guard on that id being
  zero: such a write matches no row and reports success, so a caller handed a zero id (an
  unauthenticated or mis-derived owner reference) would be told its write landed while nothing
  changed. All twenty-one of these writes — display name, timezone, interface language, reminder
  and webhook settings, calendar-feed token issue/clear, password/recovery/TOTP rotation and
  revocation, onboarding, clear-data, and the generic settings updater — now build their query
  through one helper that refuses a zero id up front, so the refusal cannot be dropped by a future
  call site without a test noticing.
