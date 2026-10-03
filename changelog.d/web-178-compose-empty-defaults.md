none

The example compose stacks no longer repeat the binary's in-code defaults (`${KEY:-20}`); they
forward `${KEY:-}`, which the binary reads as unset, so a default lives in one place. The compose
passthrough test now also judges the root compose file and fails on an environment entry written at
another indentation instead of ending the block silently. No runtime behavior change.
