none

Dead-code removal with no user-visible effect: an unused settings-update struct,
an unused TOTP sentinel error, the single-branch `httpx.JSONMode` enum (JSON
negotiation already ran in accept-or-content-type mode everywhere, and now says
so without a parameter), and an exported phase-symptom-insight wrapper no
production caller reached — its test now drives the same path the stats page
does.
