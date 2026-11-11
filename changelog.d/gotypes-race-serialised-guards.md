none

Tests only: under the race detector, every guard test that type-checks the tree with go/packages now runs in a child test process with GOMAXPROCS=1, which keeps the upstream go/types data race (golang/go#81122) off the race lanes without dropping any guard.
