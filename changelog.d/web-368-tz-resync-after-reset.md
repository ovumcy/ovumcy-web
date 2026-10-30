### Fixed

- **Clearing all data no longer leaves the time zone unsynced for the rest of the tab session.**
  Clear-data resets the stored time zone, but the tab's once-per-session sync mark outlived the
  wipe, so reminders and the calendar feed fell back to the instance zone until a new tab opened.
  An empty stored zone now always triggers the sync. (WEB-368)
