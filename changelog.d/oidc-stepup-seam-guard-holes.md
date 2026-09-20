none

Tests only; no behaviour change. The guard for the OIDC step-up state check —
the one that keeps the match at the dispatch seam and out of the three
per-purpose completions — was blind in four ways, each proved by a mutation it
stayed green on.

Nothing pinned that the seam is the ONLY way into a completion, so a handler
calling one directly passed; the copy-detector matched the identifier
`matchesState`, so a copy respelled `state.State != exchange.State` (which also
drops the constant-time comparison) passed; the only mismatching state
exercised was an extension of the sealed one, so a check skipped for an empty
state and a comparison weakened to a prefix test both passed; and the control
that stops the mismatch case greening vacuously asserted a refusal the seam
itself raises, so a seam refusing everything unconditionally passed.

Now the package's non-test sources are scanned and every mention of a
completion must sit inside the seam, the copy-detector keys on the two state
values meeting in one expression however it is spelled, the mismatch cases add
an empty and a truncated callback state, and the control asserts a per-purpose
signal only each completion can produce — so it also names which arm ran.
