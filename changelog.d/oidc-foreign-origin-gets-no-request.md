none

Test-only: the refusal tests for a foreign `jwks_uri`, a foreign `token_endpoint`
and a cross-origin discovery redirect proved an error was returned, not that no
request reached the foreign host. A second trusted TLS server on another origin
now counts every request it receives while the full server-side sign-in runs, and
the count must be zero; the same counter on the issuer origin and a direct probe
of the foreign server keep that zero from being vacuous.
