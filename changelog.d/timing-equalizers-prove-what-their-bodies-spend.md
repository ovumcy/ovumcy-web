none

Tests only: the three bcrypt/MAC timing equalizers that keep an unknown account
indistinguishable from a wrong credential — the login one, the registration one,
and the calendar feed's selector-miss one — were each declared as a swappable
`var`, and every test that named one replaced the whole var with a call counter.
Nothing ever drove the shipped body, and the work ledger that looked like it
measured the login branch read that branch's cost off the placeholder constant
instead of off the comparison. Emptying any of the three bodies left the whole
suite green while the enumeration oracle each exists to close was fully
restored. Each body now spends through a named compare seam, the ledger accounts
at that seam, and three new tests drive the shipped bodies and assert the
comparisons they actually make. A source sweep refuses any later
`equalize…Timing` var whose body calls the primitive directly. No product
behaviour changes: the same comparisons run against the same placeholders.
