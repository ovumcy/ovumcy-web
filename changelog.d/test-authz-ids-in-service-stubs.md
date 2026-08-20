none

Test-only guard: four service-layer suites can now see the identity they were
handed. The OIDC login, OIDC logout-state, onboarding and password-reset CAS
doubles declared their identity argument unnamed — or named it and stored
nothing — and answered every key with the same record, so substituting a
constant owner `1` (or swapping issuer for subject) into the production call
left the focused service and API selections green. The doubles now record the
lookup key and the write's owner id, and the happy-path case of each service
asserts it: the identity resolved by its (issuer, subject) pair on both the
login and the erasure step-up re-auth path, the account by that identity's
owner, the logout row by its session id and carrying its owner id, every
onboarding write and the CAS reset by the calling owner. Onboarding's fixtures
moved off the literal `1`, which an assertion could not have distinguished from
the defect it guards against.

Three cases go further than observing an argument, because a recorded id proves
scoping only for the seam it was recorded at — and a store whose WHERE clause
lost a predicate is handed exactly the right arguments and still returns the
wrong row. Each seeds two independent owners on one database and drives the real
repositories as the later-created owner: step 1, step 2 and completion for
onboarding; the recovery reset for the CAS path, asserting the first owner's row
is byte-for-byte unchanged, its `auth_session_version` included; and, for OIDC,
an identity per owner under a different issuer with the same subject, so login
and re-auth are shown to resolve the acting owner's identity rather than the
same-subject account next to it. Each carries a positive anchor proving the
operation landed on the acting owner rather than nowhere. No production change:
the owner id was already forwarded, and the issuer already part of the lookup,
at every site.
