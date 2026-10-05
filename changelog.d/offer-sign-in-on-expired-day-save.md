### Fixed

- **A day entry refused for an ended session can be saved after signing in again.** When a
  dashboard autosave or a calendar day save is refused because the session has ended, the notice
  now carries a localized "Sign in in a new tab" link beside its retry. Signing in in that tab
  leaves the typed entry on the original page, and the retry then saves it; nothing from the entry
  is stored in the browser along the way.
