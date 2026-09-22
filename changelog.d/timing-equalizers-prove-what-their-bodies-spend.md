none

Tests plus a test seam: the three bcrypt/MAC timing equalizers that keep an
unknown account indistinguishable from a wrong credential — the login one, the
registration one, and the calendar feed's selector-miss one — were each declared
as a swappable `var`, and every test that named one replaced the whole var with
a call counter. Nothing ever drove the shipped body, and the work ledger that
looked like it measured the login branch read that branch's cost off the
placeholder constant instead of off the comparison. Emptying any of the three
bodies left the whole suite green while the enumeration oracle each exists to
close was fully restored. Each body now spends through a named compare seam, the
ledgers account at that seam, and new tests drive the shipped bodies and assert
the comparisons they actually make. The settings re-auth equalizer now spends
through the same bcrypt seam as login and registration instead of a duplicate of
it, and its ledger likewise accounts the comparison rather than the constant. A
source sweep over the services package refuses any later `equalize…Timing` var
whose body references `bcrypt.CompareHashAndPassword` (under any import name) or
`VerifyCalendarFeedToken` directly, calls no seam var bound to one of them, or
spends through a seam no test reassigns; a body that also calls some other
primitive directly alongside a seam passes it. No product behaviour changes: the
same comparisons run against the same placeholders.
