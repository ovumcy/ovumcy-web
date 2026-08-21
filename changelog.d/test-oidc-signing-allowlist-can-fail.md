none

Tests only: the OIDC signing-algorithm allowlist now has coverage that can fail.
The HS256 and alg=none proofs stayed green with the allowlist unwired, because
go-oidc refuses both on its own; new tests read the shipped list, prove it
reaches provider.Verifier, and pin the library fallback that proof rests on.
No product code.
