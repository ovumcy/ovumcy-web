none

Two preconditions in `internal/services` now refuse the input they were written
for, and two tests now assert what their names promise. Neither production
change is reachable from a request: the manual cycle-start policy's zero-day
refusal ran on the day already projected into the request's zone, where a zero
`time.Time` is an ordinary year-1 calendar day, so it fired in UTC only — every
other zone computed the policy against a year-1 calendar instead — and the
viewer reads dereferenced the acting owner with no branch in front, so a read
that arrived without a resolved session would have panicked rather than
refused. The transport layer parses the date and resolves the session before
either is reached, so no operator or owner sees a difference today.

On the test side, the auto-fill previous-day fixture agreed with every offset
mutant it named (the recent-period lookback already covers those days and
decides the answer alone); it now says what it pins and points at the test that
pins the offset. Two error-propagation tests asserted only that some error came
back, which a pre-transaction refusal or a cause-dropping wrap also satisfies;
they now assert the injected error itself.
