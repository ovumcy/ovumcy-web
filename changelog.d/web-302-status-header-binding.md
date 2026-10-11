### Fixed

- **A stale dashboard tab no longer shows another account's status header.** After a pregnancy test
  result is saved, the dashboard fetches itself again to refresh its status header. If a different
  account had signed in from another tab in the meantime, that fetch returned the other account's
  page and its header was painted into the stale tab. The refreshed header is now used only when the
  fetched page was rendered for the same account as the open one; otherwise the header stays as it
  was.
