none

Dead-code removal in the auth-session area with no user-visible effect: the two
free-function auth-session token builders that only discarded what the real one
returns, the `AuthService` method wrapping one of them, and the handler helper
that sealed an auth token beside the shared sealing path. Every caller now uses
the live builder, and the security regressions on that path keep asserting the
same refusals — expired token, invalid signature, legacy unsealed JWT cookie,
and the unsupported-role matrix over every authenticated route.
