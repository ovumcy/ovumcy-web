none

Test-only: the Postgres containers internal/testdb and scripts/run-e2e.mjs
start now carry the label `ovumcy.test.postgres=<unix-nanos>`. The next run of
either harness removes labelled containers older than three hours, so a `go test`
or e2e run killed by its timeout or by Ctrl-C no longer leaves a container
running until Docker restarts. Unlabelled containers are never touched. No
production code changed.
