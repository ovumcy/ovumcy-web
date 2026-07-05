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
#   verify-shards   Proves the internal/api shard partition is exact: every
#                   non-test .go file lands in exactly one shard, no gaps, no
#                   overlaps. No gremlins/network dependency — pure file-listing
#                   arithmetic, safe to run in any CI job or locally.
#   merge-api-shards [in-dir] [out-file]
#                   Combines the internal_api_1..N shard JSON reports (once
#                   downloaded from their CI artifacts) into one
#                   internal_api.json, via scripts/mutationmerge (go run).
#
# The test-suite auditor consumes the JSON output
# to triage survivors into "real test gap" vs "equivalent mutant".
#
# Mutation testing is scoped to business-logic + security + transport packages.
# internal/api is the largest package (~8.5k source lines) and carries heavy
# integration tests against a real database, so it is by far the slowest target
# here — budget accordingly (see MUTATION_WORKERS below and the weekly CI job).
#
# internal/api sharding (issue #161): a single unsharded internal_api run blew
# past the 3h CI timeout (manual run 28741574692: killed at 3h0m16s, having
# reached only ~85% of the package's files with a steady, non-decelerating
# mutant rate — internal/services, a comparably-sized target, finished its
# *whole* run in 1h53m, so internal/api's heavier DB-integration tests are the
# bottleneck, not raw file count). gremlins has no package-subdivision or
# --include-files flag, but `unleash` does support repeatable --exclude-files
# <regexp> (matched against each candidate file's basename within the target
# package). That is the only file-subset mechanism the installed gremlins
# v0.6.0 exposes, so sharding partitions internal/api's own non-test .go files
# into API_SHARD_COUNT groups and, for shard N, excludes every file that is NOT
# in group N. The partition is computed at run time from a live directory
# listing (never a hardcoded file list) so it self-heals as files are
# added/removed — see api_shard_files below. Round-robin assignment (file
# index modulo shard count over the sorted file list) balances shard weight
# better than contiguous alphabetical blocks, since large handler_* files
# cluster together mid-alphabet.

set -euo pipefail

GREMLINS="${GREMLINS:-gremlins}"
WORKERS="${MUTATION_WORKERS:-4}"
TMP_DIR=".tmp/mutation"
BASELINE_DIR=".mutation"

# Packages worth mutating: domain behavior, security, and transport/HTTP handling.
TARGETS=(
  "./internal/services"
  "./internal/security"
  "./internal/api"
)

# internal/api is sharded (see header comment). Shard count was picked for
# comfortable margin under the 3h CI timeout: the unsharded run didn't finish
# in 3h, so 5 shards gives each one a small (~22-file) slice with room to
# spare even under a pessimistic multi-hour full-package estimate.
API_SHARD_PKG="./internal/api"
API_SHARD_COUNT=5

usage() {
  echo "usage: $0 {baseline [pkg-slug]|diff [ref]|verify-shards|merge-api-shards [in-dir] [out-file]}" >&2
  exit 2
}

require_gremlins() {
  if ! command -v "$GREMLINS" >/dev/null 2>&1; then
    echo "error: '$GREMLINS' not found on PATH." >&2
    echo "install: go install github.com/go-gremlins/gremlins/cmd/gremlins@latest" >&2
    exit 127
  fi
}

# api_shard_files lists internal/api's non-test .go basenames, one per line,
# in a stable sort order. This is the single source of truth every shard
# computation (selection + exclusion + the completeness proof) reads from, so
# they can never disagree with each other.
api_shard_files() {
  find "$API_SHARD_PKG" -maxdepth 1 -name '*.go' ! -name '*_test.go' -printf '%f\n' | sort
}

# api_shard_select prints the basenames assigned to shard $1 (1-based) out of
# API_SHARD_COUNT, via round-robin: the file at sorted index i (0-based) goes
# to shard (i % API_SHARD_COUNT) + 1.
api_shard_select() {
  local shard_num="$1"
  api_shard_files | awk -v shard="$shard_num" -v total="$API_SHARD_COUNT" \
    'NR % total == shard % total'
}

# api_shard_exclude_args prints one --exclude-files argument pair per line for
# every file NOT assigned to shard $1 — the complement gremlins needs to scope
# a single `unleash ./internal/api` invocation down to just that shard's
# files. Each pattern is anchored (^...$) so a filename that is a substring of
# another (e.g. input_types.go vs handlers_onboarding_input_types.go) can
# never over-match. The only regex metacharacter a Go source filename can ever
# contain is the extension dot (filenames are restricted to
# [A-Za-z0-9_.]+\.go), so escaping just that dot is sufficient here — no
# general-purpose regex-escape helper needed.
api_shard_exclude_args() {
  local shard_num="$1"
  local keep_list
  keep_list="$(api_shard_select "$shard_num")"
  while IFS= read -r fname; do
    [[ -z "$fname" ]] && continue
    if ! grep -qxF "$fname" <<<"$keep_list"; then
      # sed 's/\./\\&/g': & re-inserts the matched dot, \\ prefixes it with a
      # literal backslash — i.e. "a.b" -> "a\.b". (A bare 's/\./\\./g' silently
      # no-ops: sed's replacement-side \. is just a literal dot, not "escaped
      # dot" — verified against GNU sed 4.9.)
      printf -- '--exclude-files\n^%s$\n' "$(printf '%s' "$fname" | sed 's/\./\\&/g')"
    fi
  done < <(api_shard_files)
}

verify_shards() {
  local total_count union_file
  total_count="$(api_shard_files | wc -l | tr -d ' ')"
  union_file="$(mktemp)"

  local shard_num
  for ((shard_num = 1; shard_num <= API_SHARD_COUNT; shard_num++)); do
    local count
    count="$(api_shard_select "$shard_num" | grep -c . || true)"
    echo ">> shard $shard_num: $count files"
    api_shard_select "$shard_num" >>"$union_file"
  done

  local union_count dup_count overlap_found=0
  union_count="$(sort -u "$union_file" | wc -l | tr -d ' ')"
  dup_count="$(sort "$union_file" | uniq -d | wc -l | tr -d ' ')"

  echo ">> total internal/api non-test files: $total_count"
  echo ">> union across shards (unique):      $union_count"
  echo ">> duplicate assignments:              $dup_count"

  if [[ "$dup_count" -ne 0 ]]; then
    echo "::error::shard partition has $dup_count file(s) assigned to more than one shard" >&2
    sort "$union_file" | uniq -d >&2
    overlap_found=1
  fi
  if [[ "$union_count" -ne "$total_count" ]]; then
    echo "::error::shard union ($union_count) does not cover all internal/api files ($total_count) — gap detected" >&2
    comm -23 <(api_shard_files) <(sort -u "$union_file") >&2
    overlap_found=1
  fi

  rm -f "$union_file"

  if [[ "$overlap_found" -ne 0 ]]; then
    exit 1
  fi
  echo ">> OK: every internal/api file is covered by exactly one shard, no gaps, no overlaps."
}

run_baseline() {
  # Optional single package slug (e.g. "internal_api", or a shard slug
  # "internal_api_1".."internal_api_${API_SHARD_COUNT}") to run just one
  # target — used by CI's per-target matrix jobs so each gets a fresh
  # runner instead of accumulating disk/cache across all three targets.
  local only="${1:-}"
  mkdir -p "$TMP_DIR" "$BASELINE_DIR"

  # Shard slugs are handled separately from the plain TARGETS loop below:
  # they mutate a *subset* of internal/api's files rather than a distinct
  # package path, via repeated --exclude-files on the same target.
  if [[ "$only" =~ ^internal_api_([0-9]+)$ ]]; then
    local shard_num="${BASH_REMATCH[1]}"
    if (( shard_num < 1 || shard_num > API_SHARD_COUNT )); then
      echo "error: shard '$only' out of range (1..$API_SHARD_COUNT)" >&2
      exit 2
    fi
    local slug="$only"
    echo ">> baseline mutation: $API_SHARD_PKG (shard $shard_num/$API_SHARD_COUNT)"
    echo ">> shard $shard_num files:"
    api_shard_select "$shard_num" | sed 's/^/     /'
    mapfile -t exclude_args < <(api_shard_exclude_args "$shard_num")
    "$GREMLINS" unleash "$API_SHARD_PKG" \
      --workers "$WORKERS" \
      --output "$TMP_DIR/${slug}.json" \
      "${exclude_args[@]}"
    echo ">> baseline JSON written to $TMP_DIR/${slug}.json"
    return
  fi

  for pkg in "${TARGETS[@]}"; do
    local slug
    slug="$(echo "$pkg" | sed 's#^\./##; s#/#_#g')"
    if [[ -n "$only" && "$slug" != "$only" ]]; then
      continue
    fi
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

merge_api_shards() {
  # in-dir defaults to where CI's download-artifact step lands the shard
  # artifacts (one subdirectory per mutation-baseline-results-internal_api_N
  # artifact); out-file defaults to the same $TMP_DIR/<slug>.json convention
  # every other target already uses, so downstream tooling only ever needs to
  # know about "internal_api.json", never the shard count.
  local in_dir="${1:-.tmp/mutation-shards}"
  local out_file="${2:-$TMP_DIR/internal_api.json}"
  go run ./scripts/mutationmerge \
    -in "$in_dir" \
    -glob 'internal_api_*.json' \
    -out "$out_file"
}

main() {
  local mode="${1:-diff}"
  case "$mode" in
    baseline)         require_gremlins; run_baseline "${2:-}" ;;
    diff)             require_gremlins; run_diff "${2:-}" ;;
    verify-shards)    verify_shards ;;
    merge-api-shards) merge_api_shards "${2:-}" "${3:-}" ;;
    *)                usage ;;
  esac
}

main "$@"
