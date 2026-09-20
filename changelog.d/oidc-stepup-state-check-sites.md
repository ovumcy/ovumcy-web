none

Documentation-only correction to the entry that moved the OIDC step-up state
check to its dispatch seam. Every count it stated was one short of the code:
the match is made in three places, not two, and before that entry it was made
in five, not four. The missing site each time is the cross-site bounce, which
matches before it parks anything for the continue leg. None of the counts was
test-enforced — the guard scans the seam and the three completions, not the
bounce — so they could be wrong while the suite stayed green, and the same
miscount stood in the invariant, in the security matrix, in that entry, and in
the guard's own header.

The replacement says which of the three can still refuse anything reachable — one — rather than implying all of them can, which is the same defect one abstraction up.

Two comments are corrected with them: the continue route still claimed the
completion handler re-checks the state, which that entry is precisely what
removed, and the seam claimed to give a state check to "every other leg" when
the only other leg carries a comparison that cannot fail.

No code behaviour changes.
