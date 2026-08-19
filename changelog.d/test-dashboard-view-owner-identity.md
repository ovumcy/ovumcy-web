none

Test-only guard: the dashboard and day-editor view builders now have their
provider calls pinned to the acting session owner — all three collaborators,
including the cycle-stats provider whose output decides what the dashboard
predicts. The stubs record the user they are handed and the owner ids the two
day-state reads carry, and the test asserts each one is the session account —
with a nontrivial id, so a hard-coded or unscoped read cannot agree with the
fixture by accident, and with a positive anchor per provider, so a run that
reached none of them fails rather than passing as "no mismatch found". The two
cycle-stats entry points sit on mutually exclusive paths — an owner view derives
its stats from the entry-context logs, any other session takes the ranged read —
so each is driven by its own session rather than left unobserved. No behaviour
changes.
