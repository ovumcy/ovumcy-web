security

A cross-site OIDC step-up parks a one-minute hand-off cookie that seals the
whole step-up — the owner it names, the purpose, and an authorization code the
provider has not yet redeemed. Signing out dropped the step-up cookie but left
that hand-off, so within its minute a restored tab or a back-navigation could
still finish the linking, the local-password setup, the data clearing or the
account deletion the ended session had started. A sign-in start left it behind
too, which is the path a session that lapsed rather than being signed out takes.

Both now retract it alongside the step-up cookie. Nothing else changes: the leg
that spends the hand-off still clears it, and a step-up carried through in one
sitting is unaffected.
