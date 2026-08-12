none

Tests only: the register-and-onboard-with-an-explicit-cycle-start seeding used by
the dashboard and stats specs moves into a single shared e2e helper instead of
five hand-rolled copies, keeping each spec's seeding semantics — including the
explicit `Origin` header the HTTPS posture requires on a direct API call — and
its assertions unchanged. No product code.
