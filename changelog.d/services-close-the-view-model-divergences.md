### Fixed

- **The stats reliability card no longer describes two states of one account.** An owner in
  irregular-cycle mode with exactly two completed cycles read the heading "Early estimate" beside the
  hint written for a variable pattern. The heading and the hint now come from one reliability tier,
  so a "variable pattern" hint appears only where the "Variable pattern" heading does — at three
  completed cycles, the same minimum every other pattern statement on the page uses.
- **`ovumcy webhook show` reports the reminder lead window that is actually in force.** For a stored
  lead-day value outside 0–14 — a hand-edited row, a restored backup, or a row written before the
  bound existed — `show` printed the raw column, so it named a window no reminder would ever fire on
  and a no-op `set` looked like a change. Both the show and the set views now print the clamped value
  the reminder decision uses.

### Internal

- The stats page's view model no longer carries predicted next-period, ovulation or fertile-window
  values for an owner whose predictions are suppressed. Suppression was a template obligation — one
  boolean beside the full statistics it was meant to hide — so a new partial or a serialization of
  the struct could publish an estimate the page itself refuses to show.
