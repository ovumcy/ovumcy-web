# ClusterFuzzLite build sandbox — runs on GitHub Actions (not Google infra).
# It compiles the native Go fuzz targets in internal/services into libFuzzer
# binaries. base-builder-go is the rolling image ClusterFuzzLite requires for
# the go-118-fuzz-build toolchain; it is an ephemeral build environment, never
# a shipped or scanned runtime image, so it is intentionally not digest-pinned
# (unlike the repo-root Dockerfile). See docs/SECURITY_INVARIANTS.md.
FROM gcr.io/oss-fuzz-base/base-builder-go
COPY . $SRC/ovumcy-web
WORKDIR $SRC/ovumcy-web
COPY .clusterfuzzlite/build.sh $SRC/
