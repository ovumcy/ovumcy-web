none

Test-only guard: the dashboard and day-editor view builders now have their
provider calls pinned to the acting session owner. The stub providers record
the user they are handed and the owner ids the two day-state reads carry, and
the test asserts each one is the session account — with a nontrivial id, so a
hard-coded or unscoped read cannot agree with the fixture by accident, and with
a positive anchor per provider, so a run that reached none of them fails rather
than passing as "no mismatch found". No behaviour changes.
