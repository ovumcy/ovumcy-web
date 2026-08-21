none

Test-only guard: revoking the calendar feed is now observed to carry the owner id
it was asked to revoke. The service double recorded only that a clear had
happened, and every route regression armed a single owner, so replacing the
handler's owner id with a constant on the way into the service left both suites
green — the repository layer is scoped and proven on its own, and the untested
link was the argument between them. The double now records the id it was called
with on its own field, and a two-owner route case revokes as one owner and reads
the verdict from both rows and both subscribe URLs: the other owner's feed must
still serve, and the revoking owner's must 404. No production change — the
forwarding was already correct; what was missing was anything that would notice
if it stopped being.
