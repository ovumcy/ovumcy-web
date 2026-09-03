none

Go toolchain bump to 1.27.0 (`go.mod`, the Dockerfile builder stage, and the pinned CI scanner
versions that needed to catch up: `gosec` v2.29.0, `golangci-lint` v2.13.2, `staticcheck` v0.8.1).
Supersedes dependabot#635, which moved only the Dockerfile and could not go green on its own
(`Builder toolchain matches go.mod` compares the image tag against `go.mod`'s `go` directive). No
product behavior changes; nothing here is user-visible.
