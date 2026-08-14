none

Dead-code removal with no user-visible effect: forty locale keys nothing
translates, removed from all six catalogues (240 entries). Each English value
was also searched as a quoted literal across the browser specs and Go tests
before removal, since a spec that re-types a sentence is invisible to a search
for the key name. The catalogues stay parity-complete.
