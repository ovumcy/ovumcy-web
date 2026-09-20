none

Documentation-only correction to the entry that moved the OIDC step-up state
check to its dispatch seam. The invariant said the match is made "in exactly
two places"; there are three, the third being the cross-site bounce, which
matches before it parks anything for the continue leg. The count was never
test-enforced — the guard scans the seam and the three completions, not the
bounce — so it could be wrong while the suite stayed green. A comment on the
continue route also still claimed the completion handler re-checks the state,
which that entry is precisely what removed.

No code behaviour changes.
