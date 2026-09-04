### Fixed

- **The README now states the Go version the project actually builds with.** The badge and the
  build prerequisites still said Go 1.26.6 after the toolchain moved to 1.27.0 and then 1.27.1, so
  anyone building from source was told a version that no longer matches `go.mod` or the builder
  image.
