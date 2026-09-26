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
#                   locally or by the weekly CI job; writes per-package JSON under
#                   .tmp/mutation/ and a committed score summary under .mutation/.
#   diff [ref]      Mutate only code changed vs <ref> (default origin/main).
#                   Fast enough for CI. Advisory: never fails the build.
#   verify-shards   Proves every registered shard partition is exact — for each
#                   entry in SHARDED_PKGS, every non-test .go file lands in
#                   exactly one shard, no gaps, no overlaps. No gremlins/network
#                   dependency — pure file-listing arithmetic, safe to run in any
#                   CI job or locally.
#   merge-shards <base> [in-dir] [out-file]
#                   Combines a registered package's <base>_1..N shard JSON
#                   reports (once downloaded from their CI artifacts) into one
#                   <base>.json, via scripts/mutationmerge (go run).
#
# The test-suite auditor consumes the JSON output
# to triage survivors into "real test gap" vs "equivalent mutant".
#
# Mutation testing is scoped to business-logic + security + transport packages.
# internal/services is the largest of them (11.5k source lines against
# internal/api's 8.1k), but internal/api is the slowest: it carries heavy
# integration tests against a real database, and each mutant re-runs them. Both
# are sharded for that reason — budget accordingly (see MUTATION_WORKERS below
# and the weekly CI job).
#
# Sharding (issue #161): a single unsharded internal_api run blew
# past the 3h CI timeout (manual run 28741574692: killed at 3h0m16s, having
# reached only ~85% of the package's files with a steady, non-decelerating
# mutant rate — internal/services, at least as large a target, finished its
# *whole* run in 1h53m, so internal/api's heavier DB-integration tests are the
# bottleneck, not raw file count). gremlins has no package-subdivision or
# --include-files flag, but `unleash` does support repeatable --exclude-files
# <regexp> (matched against each candidate file's basename within the target
# package). That is the only file-subset mechanism the installed gremlins
# v0.6.0 exposes, so sharding partitions a registered package's own non-test .go
# files into the shard count SHARDED_PKGS declares for it and, for shard N,
# excludes every file that is NOT in group N. The partition is computed at run
# time from a live directory listing (never a hardcoded file list) so it
# self-heals as files are added/removed — see shard_files below. Files are
# dealt by WEIGHT, not by count (scripts/mutationpartition): each is weighed by
# the operators gremlins' default mutators rewrite and dealt heaviest first to
# the lightest shard. Round-robin by file count, the earlier rule, left the
# five internal/services shards holding 462..868 candidates (tree of
# 2026-09-25), so the heaviest cells hit the 3h cap while others idled.

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
# Shard counts come from the weekly runs of 2026-08-31..09-21, where 5 shards
# each left every internal/api cell at the 180-minute cap with 52-96% of its
# files reached. Measured on those logs, one mutation candidate costs about
# 1.24 wall-minutes in internal/api and 0.41 in internal/services at 2 workers,
# plus gremlins' coverage run (up to ~9 and ~2 minutes). The 0.41 estimate
# undershot: run 36138463409 (2026-09-25, 10 internal/services shards
# near-equal at ~290 weight each) actually took 101-172 wall-minutes on the 8
# that finished (rate up to 0.594/weight, not 0.41); the other 2
# (internal_services_6, _7 — no single file heavier than 97 of either shard's
# ~289) hit the 180-minute cap still running, which only lower-bounds their
# rate — >= 180/289 ≈ 0.623/weight, with no upper bound, since the cap cut
# them off before one showed. 14 shards brings internal/services to ~207
# weight each: at that lower bound the slow cells land near ~129 minutes, and
# the 180-minute cap tolerates up to ~0.87/weight (180/207) — about 1.4x the
# lower bound — before a cell hits it again. The next dispatched run is the
# confirmation either way. Keep this registry in sync with the matrix in
# .github/workflows/mutation.yml. Entries are "slug-base:package-dir:count".
SHARDED_PKGS=(
  "internal_api:./internal/api:14"
  "internal_services:./internal/services:14"
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
# to shard <shard-num> (1-based). scripts/mutationpartition deals shard_files'
# own list by estimated mutation weight; it reads the names from that list, so
# the partition and verify-shards' proof of it can never see different
# populations. A failure (no Go toolchain, an unreadable file, a name it
# refuses) is a non-zero status here, which every caller must check: an empty
# selection read as success would exclude every file from the shard.
shard_select() {
  local pkg_dir="$1" shard_num="$2" total="$3"
  shard_files "$pkg_dir" | go run ./scripts/mutationpartition \
    -dir "$pkg_dir" -shard "$shard_num" -of "$total"
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
  keep_list="$(shard_select "$pkg_dir" "$shard_num" "$total")" || return 1
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
  # Checked before counting, because the count cannot tell the two apart and
  # `|| rc=1` at the call site disables set -e for this whole function: a
  # missing directory makes `find` fail, `wc -l` still prints 0, and the
  # refusal below would report a registered package as EMPTY when the real
  # fault is that its path is gone. Two outcomes, two messages.
  if [[ ! -d "$pkg_dir" ]]; then
    echo "::error::$base ($pkg_dir) is not a directory — the SHARDED_PKGS entry names a path that does not exist" >&2
    return 1
  fi
  total_count="$(shard_files "$pkg_dir" | wc -l | tr -d ' ')"
  # A registered sharded package with zero non-test .go files is a
  # misconfiguration (stale SHARDED_PKGS entry, wrong path, files moved away),
  # not a legitimate empty partition: the union-vs-total comparison below would
  # accept 0 == 0 as "no gaps, no overlaps" and print OK having verified
  # nothing. A single shard within a non-empty package coming up empty (more
  # shards than files) stays legitimate and is not rejected here — only the
  # whole-population case is.
  if [[ "$total_count" -eq 0 ]]; then
    echo "::error::$base ($pkg_dir) has zero non-test .go files — a registered sharded package must not be empty" >&2
    return 1
  fi
  union_file="$(mktemp)"

  local shard_num
  for ((shard_num = 1; shard_num <= total; shard_num++)); do
    local selected count
    # One partition call per shard, checked: `|| rc=1` at the call site has
    # turned set -e off in this function, so an unchecked failure would read
    # as an empty shard and surface only as a "gap" naming the wrong cause.
    if ! selected="$(shard_select "$pkg_dir" "$shard_num" "$total")"; then
      echo "::error::$base: could not compute shard $shard_num of $total (scripts/mutationpartition failed)" >&2
      rm -f "$union_file"
      return 1
    fi
    count="$(grep -c . <<<"$selected" || true)"
    echo ">> shard $shard_num: $count files"
    if [[ -n "$selected" ]]; then
      printf '%s\n' "$selected" >>"$union_file"
    fi
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
  # "internal_api_<n>" / "internal_services_<n>", n up to the SHARDED_PKGS count)
  # to run just one target — used by CI's per-target matrix jobs so each gets a
  # fresh runner instead of accumulating disk/cache across all targets.
  local only="${1:-}"
  mkdir -p "$TMP_DIR" "$BASELINE_DIR"

  # Shard slugs are handled separately from the plain TARGETS loop below: they
  # mutate a *subset* of a package's files rather than a distinct package path,
  # via repeated --exclude-files on the same target. The slug base selects the
  # package directory and shard count from the SHARDED_PKGS registry.
  # The shard number is matched WITHOUT a leading zero on purpose, so the set
  # this accepts is exactly the "base_1..N" the unmatched-target error below
  # advertises. Accepting "08" was worse than cosmetic: `(( 08 > total ))`
  # fails on the octal literal, that failure is inside an `if` condition where
  # set -e does not apply, the test therefore reads false, and the shard is
  # accepted — then `awk 'NR % total == shard % total'` reads "08" as 8 and
  # runs shard 3's files under shard 8's name, reporting success. A typoed CI
  # matrix entry passing green is the very thing this dispatch is meant to
  # refuse.
  if [[ "$only" =~ ^(internal_api|internal_services)_([1-9][0-9]*)$ ]]; then
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
    # The same zero-population refusal verify_one_partition applies, at the
    # site that actually runs gremlins. shard_exclude_args names the files to
    # EXCLUDE, so a package with no non-test .go files yields an empty array
    # and the invocation below degrades into a full, unsharded run of the
    # package under a shard's name — reporting success for work no shard was
    # supposed to do. Checked here rather than trusted from verify-shards:
    # that is a separate command nobody is required to have run.
    # Same two outcomes verify_one_partition separates, and separated here for
    # the same reason: without this, a registry entry whose directory has moved
    # kills the run with a bare `find: ... No such file or directory` (pipefail
    # carries it out of the assignment below), leaving the operator to work out
    # that the fault is a stale registry rather than a broken package.
    if [[ ! -d "$pkg_dir" ]]; then
      echo "error: $base ($pkg_dir) is not a directory — the SHARDED_PKGS entry names a path that does not exist" >&2
      exit 1
    fi
    local shard_population
    shard_population="$(shard_files "$pkg_dir" | wc -l | tr -d ' ')"
    if [[ "$shard_population" -eq 0 ]]; then
      echo "error: $base ($pkg_dir) has zero non-test .go files, so shard $shard_num would mutate the whole package rather than its own slice" >&2
      exit 1
    fi
    local slug="$only"
    echo ">> baseline mutation: $pkg_dir (shard $shard_num/$total)"
    echo ">> shard $shard_num files:"
    shard_select "$pkg_dir" "$shard_num" "$total" | sed 's/^/     /'
    # Taken through a checked command substitution, not `mapfile < <(...)`: a
    # process substitution's status is never read, so a partition that failed
    # there would arrive as an EMPTY exclusion list — and an empty list turns
    # this shard into a full, unsharded run of the package under its name.
    local exclusions
    exclusions="$(shard_exclude_args "$pkg_dir" "$shard_num" "$total")" || {
      echo "error: could not compute the exclusions for $slug (scripts/mutationpartition failed)" >&2
      exit 1
    }
    local -a exclude_args=()
    if [[ -n "$exclusions" ]]; then
      mapfile -t exclude_args <<<"$exclusions"
    fi
    "$GREMLINS" unleash "$pkg_dir" \
      --workers "$WORKERS" \
      --output "$TMP_DIR/${slug}.json" \
      "${exclude_args[@]}"
    echo ">> baseline JSON written to $TMP_DIR/${slug}.json"
    return
  fi

  local matched=0
  for pkg in "${TARGETS[@]}"; do
    local slug
    slug="$(echo "$pkg" | sed 's#^\./##; s#/#_#g')"
    if [[ -n "$only" && "$slug" != "$only" ]]; then
      continue
    fi
    matched=1
    echo ">> baseline mutation: $pkg"
    "$GREMLINS" unleash "$pkg" \
      --workers "$WORKERS" \
      --output "$TMP_DIR/${slug}.json"
  done
  # A pkg-slug argument that matches neither a plain TARGETS slug nor a
  # registered shard form (handled in the branch above) must not report
  # success: silently running zero mutation targets and still printing
  # "baseline JSON written" would let a typoed CI matrix entry pass green
  # having tested nothing.
  if [[ "$matched" -eq 0 ]]; then
    echo "error: '$only' does not match any baseline target" >&2
    echo "valid targets:" >&2
    for pkg in "${TARGETS[@]}"; do
      echo "  $(echo "$pkg" | sed 's#^\./##; s#/#_#g')" >&2
    done
    for entry in "${SHARDED_PKGS[@]}"; do
      local base dir count
      IFS=: read -r base dir count <<<"$entry"
      echo "  ${base}_1..${count}" >&2
    done
    exit 2
  fi
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
  # -expect reuses the SHARDED_PKGS registry above as the single source of
  # truth for how many shards this base owes — the same count verify-shards
  # already checks the partition against — so a shard whose guard failed and
  # whose upload was skipped makes the merge fail loudly instead of quietly
  # publishing a partial report under the complete report's name.
  local expect
  expect="$(shard_pkg_field "$base" count)" || {
    echo "error: '$base' is not a registered sharded package (see SHARDED_PKGS)" >&2
    exit 1
  }
  go run ./scripts/mutationmerge \
    -in "$in_dir" \
    -glob "${base}_*.json" \
    -out "$out_file" \
    -expect "$expect"
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
