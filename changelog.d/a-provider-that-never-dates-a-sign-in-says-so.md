### Fixed

An OIDC step-up (erasure, local-password enrollment, identity linking) now
tells apart a provider that returned no `auth_time` at all from a sign-in that
was merely too old. The first case used to render "too old … try again", which
can never succeed on a provider that omits the claim, and both cases reached
the audit stream under one reason, so an operator could not separate a
non-conforming identity provider from a slow owner. The missing-claim refusal
now has its own copy in all six languages and its own audit reason
(`reason="oidc reauth auth_time missing"`); the too-old refusal is unchanged.
