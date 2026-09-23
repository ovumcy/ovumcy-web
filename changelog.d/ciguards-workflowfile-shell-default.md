none

Test-only: the shared workflow reader now answers a workflow step with no
`shell:` with the runner's own `bash -e`, and reads a composite action step's
`shell:` at its six-space depth, refusing one that has none. The `changes`
harness in `ciguards` takes its flags from it instead of a private copy,
reading the step's keys on both sides of its script. The scan that holds a
`changes`-gated job's reads of `needs` to the fail-safe shape now counts the
word only inside `${{ }}` or in an `if:` value, including one continued over
several lines, so a step name that says "needs" no longer fails it.
