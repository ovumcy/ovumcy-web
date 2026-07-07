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

# internal/api and internal/services are each sharded (see header comment):
# both outgrew the 3h CI timeout as a single unsharded job (internal/api first,
# issue #161; internal/services once its integration/property suites expanded).
# Shard counts were picked for comfortable margin — 5 shards gives each one a
# small (~17–22-file) slice with room to spare even under a pessimistic
# multi-hour full-package estimate. Keep this registry in sync with the matrix
# in .github/workflows/mutation.yml. Entries are "slug-base:package-dir:count".
SHARDED_PKGS=(
  "internal_api:./internal/api:5"
  "internal_services:./internal/services:5"
)

# shard_pkg_field <slug-base> <dir|count> looks up a sharded package's directory
# or shard count from SHARDED_PKGS; non-zero exit if the base is not registered.
shard_pkg_field() {
  local want="$1" field="$2" entry base dir count
  for entry in "${SHARDED_PKGS[@]}"; do
    IFS=: read -r base dir count <<<"$entry"
    if [[ "$base" == "$want" ]]; then
      case "$field" in
        dir) printf '%s\n' "$dir" ;;
        count) printf '%s\n' "$count" ;;
        *) return 2 ;;
      esac
      return 0
    fi
  done
  return 1
}

usage() {
  echo "usage: $0 {baseline [pkg-slug]|diff [ref]|verify-shards|merge-shards <base> [in-dir] [out-file]}" >&2
  exit 2
}

require_gremlins() {
  if ! command -v "$GREMLINS" >/dev/null 2>&1; then
    echo "error: '$GREMLINS' not found on PATH." >&2
    echo "install: go install github.com/go-gremlins/gremlins/cmd/gremlins@latest" >&2
    exit 127
  fi
}

# shard_files <pkg-dir> lists a package's non-test .go basenames, one per line,
# in a stable sort order. This is the single source of truth every shard
# computation (selection + exclusion + the completeness proof) reads from, so
# they can never disagree with each other.
shard_files() {
  local pkg_dir="$1"
  find "$pkg_dir" -maxdepth 1 -name '*.go' ! -name '*_test.go' -printf '%f\n' | sort
}

# shard_select <pkg-dir> <shard-num> <shard-count> prints the basenames assigned
# to shard <shard-num> (1-based), via round-robin: the file at sorted index i
# (0-based) goes to shard (i % shard-count) + 1.
shard_select() {
  local pkg_dir="$1" shard_num="$2" total="$3"
  shard_files "$pkg_dir" | awk -v shard="$shard_num" -v total="$total" \
    'NR % total == shard % total'
}

# shard_exclude_args <pkg-dir> <shard-num> <shard-count> prints one
# --exclude-files argument pair per line for every file NOT assigned to the
# shard — the complement gremlins needs to scope a single `unleash <pkg>`
# invocation down to just that shard's files. Each pattern is anchored (^...$)
# so a filename that is a substring of another (e.g. input_types.go vs
# handlers_onboarding_input_types.go) can never over-match. The only regex
# metacharacter a Go source filename can ever contain is the extension dot
# (filenames are restricted to [A-Za-z0-9_.]+\.go), so escaping just that dot is
# sufficient here — no general-purpose regex-escape helper needed.
shard_exclude_args() {
  local pkg_dir="$1" shard_num="$2" total="$3"
  local keep_list
  keep_list="$(shard_select "$pkg_dir" "$shard_num" "$total")"
  while IFS= read -r fname; do
    [[ -z "$fname" ]] && continue
    if ! grep -qxF "$fname" <<<"$keep_list"; then
      # sed 's/\./\\&/g': & re-inserts the matched dot, \\ prefixes it with a
      # literal backslash — i.e. "a.b" -> "a\.b". (A bare 's/\./\\./g' silently
      # no-ops: sed's replacement-side \. is just a literal dot, not "escaped
      # dot" — verified against GNU sed 4.9.)
      printf -- '--exclude-files\n^%s$\n' "$(printf '%s' "$fname" | sed 's/\./\\&/g')"
    fi
  done < <(shard_files "$pkg_dir")
}

# verify_shards proves every sharded package's partition is exact — each
# non-test .go file lands in exactly one shard, no gaps, no overlaps — for every
# entry in SHARDED_PKGS. Pure file-listing arithmetic, no gremlins/network
# dependency, so it is safe to run in any CI job or locally.
verify_shards() {
  local entry base dir count rc=0
  for entry in "${SHARDED_PKGS[@]}"; do
    IFS=: read -r base dir count <<<"$entry"
    verify_one_partition "$base" "$dir" "$count" || rc=1
  done
  if [[ "$rc" -ne 0 ]]; then
    exit 1
  fi
}

# verify_one_partition <slug-base> <pkg-dir> <shard-count> checks a single
# package's shard partition and returns (without exiting) non-zero on a gap or
# overlap, so verify_shards can report every package before failing.
verify_one_partition() {
  local base="$1" pkg_dir="$2" total="$3"
  local total_count union_file
  echo ">> verifying $base partition ($pkg_dir, $total shards)"
  total_count="$(shard_files "$pkg_dir" | wc -l | tr -d ' ')"
  union_file="$(mktemp)"

  local shard_num
  for ((shard_num = 1; shard_num <= total; shard_num++)); do
    local count
    count="$(shard_select "$pkg_dir" "$shard_num" "$total" | grep -c . || true)"
    echo ">> shard $shard_num: $count files"
    shard_select "$pkg_dir" "$shard_num" "$total" >>"$union_file"
  done

  local union_count dup_count overlap_found=0
  union_count="$(sort -u "$union_file" | wc -l | tr -d ' ')"
  dup_count="$(sort "$union_file" | uniq -d | wc -l | tr -d ' ')"

  echo ">> total $base non-test files: $total_count"
  echo ">> union across shards (unique):      $union_count"
  echo ">> duplicate assignments:              $dup_count"

  if [[ "$dup_count" -ne 0 ]]; then
    echo "::error::$base shard partition has $dup_count file(s) assigned to more than one shard" >&2
    sort "$union_file" | uniq -d >&2
    overlap_found=1
  fi
  if [[ "$union_count" -ne "$total_count" ]]; then
    echo "::error::$base shard union ($union_count) does not cover all $base files ($total_count) — gap detected" >&2
    comm -23 <(shard_files "$pkg_dir") <(sort -u "$union_file") >&2
    overlap_found=1
  fi

  rm -f "$union_file"

  if [[ "$overlap_found" -ne 0 ]]; then
    return 1
  fi
  echo ">> OK: every $base file is covered by exactly one shard, no gaps, no overlaps."
  return 0
}

run_baseline() {
  # Optional single package slug (e.g. "internal_security", or a shard slug
  # "internal_api_1".."internal_api_5" / "internal_services_1".."internal_services_5")
  # to run just one target — used by CI's per-target matrix jobs so each gets a
  # fresh runner instead of accumulating disk/cache across all targets.
  local only="${1:-}"
  mkdir -p "$TMP_DIR" "$BASELINE_DIR"

  # Shard slugs are handled separately from the plain TARGETS loop below: they
  # mutate a *subset* of a package's files rather than a distinct package path,
  # via repeated --exclude-files on the same target. The slug base selects the
  # package directory and shard count from the SHARDED_PKGS registry.
  if [[ "$only" =~ ^(internal_api|internal_services)_([0-9]+)$ ]]; then
    local base="${BASH_REMATCH[1]}"
    local shard_num="${BASH_REMATCH[2]}"
    local pkg_dir total
    pkg_dir="$(shard_pkg_field "$base" dir)" || {
      echo "error: '$base' is not a sharded package" >&2
      exit 2
    }
    total="$(shard_pkg_field "$base" count)"
    if (( shard_num < 1 || shard_num > total )); then
      echo "error: shard '$only' out of range (1..$total)" >&2
      exit 2
    fi
    local slug="$only"
    echo ">> baseline mutation: $pkg_dir (shard $shard_num/$total)"
    echo ">> shard $shard_num files:"
    shard_select "$pkg_dir" "$shard_num" "$total" | sed 's/^/     /'
    mapfile -t exclude_args < <(shard_exclude_args "$pkg_dir" "$shard_num" "$total")
    "$GREMLINS" unleash "$pkg_dir" \
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

merge_shards() {
  # <base> is the sharded package slug (internal_api | internal_services).
  # in-dir defaults to where CI's download-artifact step lands the shard
  # artifacts (one subdirectory per mutation-baseline-results-<base>_N
  # artifact); out-file defaults to the same $TMP_DIR/<slug>.json convention
  # every other target already uses, so downstream tooling only ever needs to
  # know about "<base>.json", never the shard count.
  local base="${1:?merge-shards requires a slug base (internal_api | internal_services)}"
  local in_dir="${2:-.tmp/mutation-shards}"
  local out_file="${3:-$TMP_DIR/${base}.json}"
  go run ./scripts/mutationmerge \
    -in "$in_dir" \
    -glob "${base}_*.json" \
    -out "$out_file"
}

main() {
  local mode="${1:-diff}"
  case "$mode" in
    baseline)      require_gremlins; run_baseline "${2:-}" ;;
    diff)          require_gremlins; run_diff "${2:-}" ;;
    verify-shards) verify_shards ;;
    merge-shards)  merge_shards "${2:-}" "${3:-}" "${4:-}" ;;
    *)             usage ;;
  esac
}

main "$@"
