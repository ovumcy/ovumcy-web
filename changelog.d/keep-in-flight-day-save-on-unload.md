### Fixed

- **A day edit already being saved is no longer lost when the page is reloaded or closed.** The
  dashboard journal's autosave now rides a keepalive request, so a save the page has started
  outlives the page instead of being cancelled with it. A calendar Save still waiting on the server
  is re-sent on a keepalive request as the page leaves, carrying the same entry. While a save is
  open, leaving the page asks for confirmation first.
