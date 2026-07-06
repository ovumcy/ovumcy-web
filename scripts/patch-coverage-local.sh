#!/usr/bin/env bash
#
# Reproduces CI's coverage condition in one command, then runs the patch-coverage
# gate (scripts/patchcov) against a GUARANTEED-fresh profile.
#
# Why this exists: `go test -coverprofile` is subject to Go's test result cache.
# If you edit a file, run the full package-set test+coverage command, then edit
# that file again WITHOUT touching its test, `go test` can serve the coverage
# profile for the previous run from cache instead of re-executing — so
# coverage.out silently reflects your code from before the last edit. Running
# scripts/patchcov against that stale profile gives a FALSE PASS locally: your
# newest lines look "covered" because the cache replayed an earlier, genuinely
# covered version of the diff. CI never hits this because every job starts on a
# clean checkout with an empty test cache, so it fails on the real gap — which is
# confusing and costly to reproduce after the fact if you don't know why.
#
# This script defeats that cache two ways, both required:
#   1. `go clean -testcache`   — discards cached PASS/coverage results so every
#                                package actually re-executes its tests.
#   2. `go test -count=1 ...`  — belt-and-suspenders: forces re-execution even if
#                                something repopulates part of the cache between
#                                the clean and the test invocation.
# Skipping either one re-opens the false-pass window; keep both.
#
# Mirrors, line for line, the CI `test` job's coverage step
# (.github/workflows/ci.yml) and the `patch-coverage` job's gate invocation:
#   - same package set for both -run and -coverpkg (cmd, internal, migrations,
#     scripts, web — NOT ./..., since node_modules/ under npm-installed
#     dependencies can ship a stray .go file; see the comment in ci.yml),
#   - same -covermode=atomic,
#   - scripts/patchcov with COVERAGE_FILE=coverage.out and BASE_REF defaulting to
#     origin/main (override BASE_REF to check against a different base, e.g. a
#     stacked branch).
#
# Usage:
#   bash scripts/patch-coverage-local.sh              # gate vs origin/main
#   BASE_REF=origin/release-1.2 bash scripts/patch-coverage-local.sh
#
# Takes a few minutes: it is the full test suite, run for real, on purpose.

set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

PKGS="./cmd/... ./internal/... ./migrations/... ./scripts/... ./web/..."
COVERPKG="./cmd/...,./internal/...,./migrations/...,./scripts/...,./web/..."
COVERAGE_FILE="${COVERAGE_FILE:-coverage.out}"
BASE_REF="${BASE_REF:-origin/main}"

echo ">> removing any existing $COVERAGE_FILE"
rm -f "$COVERAGE_FILE"

echo ">> go clean -testcache (defeats the cached-run false pass)"
go clean -testcache

echo ">> go test -count=1 -coverprofile=$COVERAGE_FILE -covermode=atomic (fresh run, matches CI's test job)"
# shellcheck disable=SC2086 # PKGS is an intentional word-split package list, as in ci.yml
go test $PKGS -coverprofile="$COVERAGE_FILE" -covermode=atomic -coverpkg="$COVERPKG" -count=1

echo ">> go run ./scripts/patchcov (COVERAGE_FILE=$COVERAGE_FILE BASE_REF=$BASE_REF)"
COVERAGE_FILE="$COVERAGE_FILE" BASE_REF="$BASE_REF" go run ./scripts/patchcov
