none

The user-visible half of this work shipped in #806 and carries its own fragment
(`mirror-carries-its-own-signature.md`). What is left here is the invariants document catching up
with it: `docs/SECURITY_INVARIANTS.md` described the publish order for GHCR only, while the mirror
now follows that order on its own account. No behaviour changes.
