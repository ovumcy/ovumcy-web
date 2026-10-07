### Fixed

- **A day form sends the browser back to one page, whether it was saved or refused.** Without
  JavaScript, a day form names the page it came from, and the save read that name more leniently
  than a refusal did: deleting a day from a link spelled `?source=Calendar` returned to the
  calendar when it worked and to the dashboard when it was refused. Both now read it the same way.
