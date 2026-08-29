none

CI/tooling-only fix: `changelogd assemble` never maintained the trailing
Keep a Changelog reference-link block, so the first real release it cuts
would ship with no `[vX.Y.Z]: …compare…` line for that release and a stale
`[Unreleased]` compare link. `assemble` now inserts the new release's link
and moves `[Unreleased]` to compare against it; a rerun for a version that
already has a link is a no-op. No release was cut and the assembler was not
run against the real `CHANGELOG.md`.
