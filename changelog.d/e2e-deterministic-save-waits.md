none

Test-only: two e2e flake classes made deterministic. The calendar save and
cycle-start helpers no longer wait on `networkidle`, which counted a request
aborted by an earlier navigation and ran out the test timeout after the grid
refresh had already answered; they now wait on the grid refresh (or the day
editor's lazy load) response and on htmx having no request in flight. The
dashboard specs pin the page clock, so a run that crosses local midnight between
two autosaves is no longer reloaded onto the new day under the spec. No product
code changes.
