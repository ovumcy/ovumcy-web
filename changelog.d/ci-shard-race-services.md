none

CI only: the race lane splits `internal/services` across three shards, with the
test list taken from the Go toolchain rather than a hand-written pattern, and a
thin gate job keeping the required `race` status check. No product code.
