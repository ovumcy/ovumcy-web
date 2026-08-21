none

Test- and documentation-only. A class-wide sweep now requires every sealed cookie
to be bounded on the server: it derives the roster from the declared cookie specs,
mints each one through its production path, and — for a payload carrying its own
expiry — rewrites that expiry into the past, re-seals it with the production codec
and requires the production reader to refuse it. The naive rule ("every payload
carries `expires_at`") is false for three cookies that are correct as they are, so
the two exemptions are stated as properties of the payload the mint produces —
an opaque signed token whose own `exp` the verifier enforces, and a payload naming
no account whose bytes the server contributed nothing to — never as a list of
cookie names. A sealed cookie the sweep cannot classify fails rather than being
skipped: an undecidable case is the defect it exists to catch. There is no
allowlist, so a cookie added later is judged by the same rule. The roster itself
is cross-checked against every cookie-name constant the package declares, so a
spec built by a helper call rather than a bare literal cannot slip past the source
walk and leave the sweep green over a cookie it never judged.

Alongside it, `SECURITY.md` now says what the calendar-feed tests actually prove:
the revoke row cites the owner-scoped route regression, the settings-endpoint row
claims owner scoping for revoke as well as mint, rotate and reveal (it always held,
and the repository-layer test proving it hid the fact in its name), and the
payload-expiry row carries the class-wide claim and its sweep. The service-layer
revoke test is renamed to state the owner id it verifies. No production change.
