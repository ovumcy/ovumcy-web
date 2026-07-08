#!/bin/bash -eu
# Compile each fuzz target into a libFuzzer binary for ClusterFuzzLite.
#
# go-118-fuzz-build cannot pick up native testing.F fuzzers that live in _test.go
# files, so the six targets are mirrored in internal/services/policy_fuzz_libfuzzer.go
# under the `gofuzz` build tag (see that file). compile_go_fuzzer builds those shim
# harnesses with -tags gofuzz. Keep this list in sync with .github/workflows/fuzz.yml
# and policy_fuzz_libfuzzer.go.
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
  compile_go_fuzzer github.com/ovumcy/ovumcy-web/internal/services "$target" "$target" gofuzz
done
