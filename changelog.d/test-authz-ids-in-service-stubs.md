none

Test-only guard: four service-layer suites can now see the identity they were
handed. The OIDC login, OIDC logout-state, onboarding and password-reset CAS
doubles declared their identity argument unnamed — or named it and stored
nothing — and answered every key with the same record, so substituting a
constant owner `1` (or swapping issuer for subject) into the production call
left the focused service and API selections green. The doubles now record the
lookup key and the write's owner id, and the happy-path case of each service
asserts it: the identity resolved by its (issuer, subject) pair, the account by
that identity's owner, the logout row by its session id and carrying its owner
id, every onboarding write and the CAS reset by the calling owner. Onboarding's
fixtures moved off the literal `1`, which an assertion could not have
distinguished from the defect it guards against.

Two cases go further than observing an argument, because a recorded id proves
scoping only for the seam it was recorded at: each seeds two independent owners
on one database and drives the real repository as the later-created owner —
step 1, step 2 and completion for onboarding, the recovery reset for the CAS
path — then asserts the first owner's row is byte-for-byte unchanged, its
`auth_session_version` included, with a positive anchor proving the write landed
on the acting owner rather than nowhere. No production change: the owner id was
already forwarded correctly at every site.
