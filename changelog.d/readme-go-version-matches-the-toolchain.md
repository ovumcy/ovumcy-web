### Fixed

- **The README now states the Go version the project actually builds with, and a guard keeps it
  that way.** The badge and the build prerequisites still said Go 1.26.6 after the toolchain moved
  to 1.27.0 and then 1.27.1, so anyone building from source was told a version that no longer
  matches `go.mod` or the builder image. Both lines are now compared against `go.mod`'s `go`
  directive by `scripts/readmeversion`, which is the only thing that can catch this: every workflow
  resolves its own toolchain with `go-version-file: go.mod`, so CI stays green whatever the README
  claims.
