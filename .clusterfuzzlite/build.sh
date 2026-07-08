#!/bin/bash -eu
# Compile each native Go fuzz target (go test -fuzz) into a libFuzzer binary for
# ClusterFuzzLite. Keep this list in sync with .github/workflows/fuzz.yml — same
# targets in internal/services/policy_fuzz_test.go.
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
