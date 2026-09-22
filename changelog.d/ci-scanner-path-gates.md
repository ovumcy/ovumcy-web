none

CI-only: security.yml and codeql.yml now skip a scanner only when the diff
proves it irrelevant (push/schedule/dispatch and an unresolved diff still run
everything), with no change to what ships or how it behaves for an operator.
