none

Test-only barrier: a locale string used as a printf pattern now has to agree
with the argument list the code passes it. The suite fails when any of the six
catalogues carries a different verb sequence than its call site requires — the
sequence, not the count, so a locale that swapped %d and %s while keeping six of
them is a finding — when the English fallback literal beside the call drifts out
of step with it, and when a shipped `fmt.Sprintf` whose pattern is computed
rather than written is not declared in the contract table at all. `go vet`'s
printf checker stops at the first non-constant format string, which is every one
of these nine sites by construction.

No user-visible change: all eleven keys already match all nine sites in all six
languages, which is why the barrier is enabled without a baseline. The failures
it exists to name are `%!s(int=12)` and `%!(EXTRA string=days)` rendered into the
Insights summary, the basal-temperature range hint, the day-save toast, the cycle
trend labels and the webhook reminder body.
