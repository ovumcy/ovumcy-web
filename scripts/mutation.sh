#!/usr/bin/env bash
#
# Mutation testing wrapper around gremlins (https://github.com/go-gremlins/gremlins).
#
# Why this exists: high test coverage does not prove tests actually catch broken
# code. Mutation testing injects faults into the production code and checks that
# at least one test fails ("kills" the mutant). Surviving mutants mark assertions
# that are too weak. Efficacy = killed / (killed + lived).
#
# Modes:
#   baseline        Full run over the high-value packages. Slow (hours). Run
#                   locally or nightly; writes per-package JSON under .tmp/mutation/
#                   and a committed score summary under .mutation/.
#   diff [ref]      Mutate only code changed vs <ref> (default origin/main).
#                   Fast enough for CI. Advisory: never fails the build.
#
# The test-suite auditor consumes the JSON output
# to triage survivors into "real test gap" vs "equivalent mutant".
#
# Mutation testing is intentionally scoped to business-logic + security packages.
# internal/api is markup-heavy and far slower to mutate; services/security carry
# the behavioral signal worth mutating.

set -euo pipefail

GREMLINS="${GREMLINS:-gremlins}"
WORKERS="${MUTATION_WORKERS:-4}"
TMP_DIR=".tmp/mutation"
BASELINE_DIR=".mutation"

# Packages worth mutating: domain behavior + security, not markup plumbing.
TARGETS=(
  "./internal/services"
  "./internal/security"
)

usage() {
  echo "usage: $0 {baseline|diff [ref]}" >&2
  exit 2
}

require_gremlins() {
  if ! command -v "$GREMLINS" >/dev/null 2>&1; then
    echo "error: '$GREMLINS' not found on PATH." >&2
    echo "install: go install github.com/go-gremlins/gremlins/cmd/gremlins@latest" >&2
    exit 127
  fi
}

run_baseline() {
  mkdir -p "$TMP_DIR" "$BASELINE_DIR"
  for pkg in "${TARGETS[@]}"; do
    local slug
    slug="$(echo "$pkg" | sed 's#^\./##; s#/#_#g')"
    echo ">> baseline mutation: $pkg"
    "$GREMLINS" unleash "$pkg" \
      --workers "$WORKERS" \
      --output "$TMP_DIR/${slug}.json"
  done
  echo ">> baseline JSON written to $TMP_DIR/ (commit a summary into $BASELINE_DIR/ once reviewed)"
}

run_diff() {
  local ref="${1:-origin/main}"
  mkdir -p "$TMP_DIR"
  echo ">> diff mutation vs $ref (advisory)"
  # No --threshold-* flags: this is advisory and must not fail the build.
  "$GREMLINS" unleash \
    --diff "$ref" \
    --workers "$WORKERS" \
    --output "$TMP_DIR/diff.json"
}

main() {
  require_gremlins
  local mode="${1:-diff}"
  case "$mode" in
    baseline) run_baseline ;;
    diff)     run_diff "${2:-}" ;;
    *)        usage ;;
  esac
}

main "$@"
