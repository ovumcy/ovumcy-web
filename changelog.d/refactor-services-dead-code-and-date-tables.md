### Internal

- **Localized date names moved to one table per language.** The five parallel month/weekday maps in
  the services layer became a single fixed-size entry per language, and a new sweep requires a
  complete entry for every locale the i18n manager reports — a seventh locale can no longer be added
  and silently render English dates on the calendar header, the day label and the dashboard. Every
  rendered date is byte-identical to before.
- **Dead code retired in the services layer.** The unused singular `AttemptLimiter` methods are gone
  and the lockout tests now drive the plural methods production actually calls; the Russian plural
  rule has one home again (`i18n.PluralCategory`); the unreachable nil-user guards in the day-feedback
  policy and a French count-word branch with identical arms were removed. `User.LongPeriodWarnedAt`
  is now `User.LongPeriodWarningCycleStart`, the name its column and its date-only type always had.
  No stored data, API payload or rendered string changes.
