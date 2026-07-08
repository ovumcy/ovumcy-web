#!/bin/bash -eu
# Compile each native Go fuzz target (go test -fuzz) into a libFuzzer binary for
# ClusterFuzzLite. Keep this list in sync with .github/workflows/fuzz.yml — same
# targets in internal/services/policy_fuzz_test.go.
#
# The base image's bundled go-118-fuzz-build can predate native testing.F fuzz
# support, so it fails to locate targets that live in _test.go files. Install the
# current tool and its testing shim first (the documented ClusterFuzzLite Go
# recipe) so the native targets are found and the rewrite compiles.
go install github.com/AdamKorcz/go-118-fuzz-build@latest
go get github.com/AdamKorcz/go-118-fuzz-build/testing

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
