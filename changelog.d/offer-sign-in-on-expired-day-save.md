### Fixed

- **A day entry refused for an ended session can be saved after signing in again.** When a
  dashboard autosave or a calendar day save is refused because the session has ended, the notice
  now carries a localized "Sign in in a new tab" link beside its retry. Signing in in that tab
  leaves the typed entry on the original page, and the retry then saves it; nothing from the entry
  is stored in the browser along the way. If a different account signs in in that tab, the retry
  is refused with "You're now signed in to a different account. This entry was not saved." and
  writes nothing: each day form carries an opaque binding to the account that rendered it, and a
  save whose binding names another account answers 409. API clients that do not send the field
  are unaffected.
