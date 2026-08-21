none

CI only: the Dependabot config no longer carries an `ignore` entry for
pre-release `golang` builder tags. A docker ignore condition takes version
requirements, so the pattern was rejected by Dependabot outright and the whole
file was invalid — which stops dependency and security updates for every
ecosystem in the repository, not just that entry. The builder is still held to
go.mod by the `Builder toolchain matches go.mod` CI step, which fails any diff
where the two disagree. No product code.
