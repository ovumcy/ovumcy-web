none

Test-only guard: the erasure step-up's foreign-session case now seeds the second
owner with a day entry of its own and asserts that entry survives alongside the
first owner's, plus that the refusal is the identity-mismatch one rather than any
other check on the way. No user-visible change — the callback already required
the session to be the account the sealed step-up payload names; every count the
test read belonged to that one account, so a callback that erased the session's
own account instead moved no number and stayed green.
