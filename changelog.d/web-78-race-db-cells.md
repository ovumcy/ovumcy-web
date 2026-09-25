### Internal

- **`internal/db` runs under the race detector in four cells of its own (`race-db`).** It was
  849.550 s of `race-rest`'s 866.827 s package sweep and the critical path of every full
  pull-request and merge-queue run; `scripts/racepartition` splits it by test name as it already
  splits `internal/services`, and each cell refuses a listing too short to give it a test. The
  required `race` gate now judges `race-db` too. `race-services` and the browser `e2e-shard` lanes
  each gain a fourth cell.
- **The weekly mutation run can finish.** Its `internal/services` cells were dying when a mutant
  turned a loop's `x++` into `x--` and the loop's appends ran the runner out of memory; each
  mutation cell now caps a process's address space, so that test binary fails instead. gremlins'
  coverage pass gets a 30-minute test timeout in place of Go's 10-minute default, which
  `internal/api` was reaching. Files are dealt to shards by estimated mutation weight
  (`scripts/mutationpartition`) rather than round-robin by count, across 14 `internal/api` and
  10 `internal/services` shards, with at most 11 cells running at once.
