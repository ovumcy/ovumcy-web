none

Test-only: pins REL-6 through REL-10 (patch-coverage in the merge queue, the three
--ignore-unfixed removals, mutation-merge's -expect flag, the changelog gate's untrusted-ref
splice, and the checkout persist-credentials sweep) with regression tests, so a future edit
reverting any of them fails CI instead of shipping silently. No workflow behavior changes.
