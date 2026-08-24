#!/bin/bash -eu
# Compile each fuzz target into a libFuzzer binary for ClusterFuzzLite.
#
# The six targets are mirrored in internal/services/policy_fuzz_libfuzzer.go under
# the `gofuzz` build tag (go-118-fuzz-build cannot read native testing.F fuzzers
# that live in _test.go). GOFLAGS forces that tag on every `go build` so the shim
# file is included, and compile_native_go_fuzzer (go-118-fuzz-build, which speaks
# testing.F) builds the harnesses. The list below is the third copy of the six
# target names; internal/services/policy_fuzz_parity_barrier_test.go reads this
# loop and fails when it stops matching the two Go harnesses, so a target added
# or renamed there has to appear here in the same commit.
# go-118-fuzz-build has no tagged releases; pin the pseudo-version (encodes the
# commit hash) instead of floating on @latest, same as every other `go install`
# in this repo's CI pins a version. Bump deliberately, not automatically.
go install github.com/AdamKorcz/go-118-fuzz-build@v0.0.0-20250520111509-a70c2aa677fa
go get github.com/AdamKorcz/go-118-fuzz-build/testing@v0.0.0-20250520111509-a70c2aa677fa
export GOFLAGS="-tags=gofuzz"

for target in \
  FuzzParseDayDate \
  FuzzParseDayRange \
  FuzzValidatePasswordStrength \
  FuzzNormalizeAuthEmail \
  FuzzNormalizeRecoveryCode \
  FuzzSanitizeOnboardingCycleAndPeriod
do
  compile_native_go_fuzzer github.com/ovumcy/ovumcy-web/internal/services "$target" "$target"
done
