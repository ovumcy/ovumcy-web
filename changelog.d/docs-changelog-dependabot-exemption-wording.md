none

Docs-only: corrected `changelog.d/ci-changelog-fragment-dependabot-exemption.md`
in place, which claimed the `### Dependencies` section "is assembled from the
bumps that merged" — no such automatic mechanism exists. Both unreleased
fragments would have landed in the same release contradicting each other
(this one against `docs-root-truth-and-local-checks.md`, which already states
the real mechanism: the operator compiles the section by hand from
`git log <last-tag>..HEAD` and enters it as a fragment before `changelogd
assemble` runs). No code change; assembler not run.
