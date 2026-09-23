none

CI-only: security.yml and codeql.yml now skip a scanner only when the diff
proves it irrelevant; push, schedule, dispatch and an unresolved diff still run
everything. The trivy-image gate follows what the Dockerfile copies (all of
web/ but web/src/) plus the Dockerfile and .dockerignore, the trivy-fs gate
follows the manifests and lockfiles of every ecosystem it names, and the
CodeQL JavaScript gate follows npm manifests at any depth. ci.yml, security.yml
and codeql.yml share one base-resolution action, which lists the diff with
`git diff -z` (a path with a newline runs everything), hands the list over as a
file under RUNNER_TEMP, and forces every scanner and CodeQL language when it is
edited itself; they check out full history only on pull_request and
merge_group, and match paths as bytes, so a name that is not valid UTF-8
cannot skip a lane. The browser shards and the Postgres smoke now run when the
`changes` job fails instead of being skipped. No change to what ships or how it
behaves for an operator.
