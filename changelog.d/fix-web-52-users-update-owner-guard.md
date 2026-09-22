### Security

- **A zero owner id could no longer be told apart from a real one on the users table.**
  Every UPDATE against a user's row was scoped by `WHERE id = ?` with no guard on that id being
  zero: such a write matches no row and reports success, so a caller handed a zero id (an
  unauthenticated or mis-derived owner reference) would be told its write landed while nothing
  changed. All twenty-one of these writes — display name, timezone, interface language, reminder
  and webhook settings, calendar-feed token issue/clear, password/recovery/TOTP rotation and
  revocation, onboarding, clear-data, and the generic settings updater — now build their query
  through one helper that refuses a zero id up front, so the refusal cannot be dropped by a future
  call site without a test noticing. Three more writers that build a compound `id = ? AND ...`
  compare-and-set clause (webhook delivery mark, webhook watermark release, calendar-feed
  verifier-MAC backfill) shared the same hole — a zero id was indistinguishable from a normal
  zero-row CAS miss — and now call the same refusal directly before building their query.
  Calendar-feed token issue and clear now refuse a zero id before advancing the restore fence,
  not after: a fence advanced for a revocation that never happened made a restore from any
  earlier backup disarm every armed feed.
