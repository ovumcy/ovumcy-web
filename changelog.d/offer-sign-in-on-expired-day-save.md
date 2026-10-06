### Fixed

- **A day entry refused for an ended session can be saved after signing in again.** When a
  dashboard autosave or a calendar day save is refused because the session has ended, the notice
  now carries a localized "Sign in in a new tab" link beside its retry. Signing in in that tab
  leaves the typed entry on the original page, and the retry then saves it; nothing from the entry
  is stored in the browser along the way. If a different account signs in in that tab, the retry
  is refused with "You're now signed in to a different account. This entry was not saved." and
  writes nothing: every day write a page renders (save, delete, the dashboard undo, cycle start)
  carries an opaque binding to the account that rendered it, and a write whose binding names
  another account — or a write from a page that carries none — answers 409. A page left open
  while another account signs in elsewhere in the same browser can therefore no longer save,
  delete or mark a cycle start in that account. The calendar, whose day editor is fetched rather
  than rendered with the page, sends the account it was rendered for on every request from inside
  it: a calendar tab left open while another account signs in answers the day fetch and the grid
  refresh with the same refusal instead of showing that account's day. JSON API clients that do
  not send the binding are unaffected. If the page's security token went stale while the owner
  was signing in (it idled out, or the server restarted), the refused retry re-reads it from the
  page and the next retry saves, instead of being refused until the page is reloaded.
