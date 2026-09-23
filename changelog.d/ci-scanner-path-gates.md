none

CI-only: security.yml and codeql.yml now skip a scanner only when the diff
proves it irrelevant; push, schedule, dispatch and an unresolved diff still run
everything. The trivy-image gate follows what the Dockerfile copies (all of
web/ but web/src/), and the CodeQL JavaScript gate follows npm manifests at any
depth. ci.yml, security.yml and codeql.yml share one base-resolution action and
check out full history only on pull_request and merge_group. No change to what
ships or how it behaves for an operator.
