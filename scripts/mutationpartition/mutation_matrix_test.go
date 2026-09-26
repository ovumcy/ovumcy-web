package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// shardedPkgsEntry mirrors one SHARDED_PKGS line in scripts/mutation.sh:
// "slug-base:package-dir:count".
type shardedPkgsEntry struct {
	base  string
	count int
}

var shardedPkgsLine = regexp.MustCompile(`^\s*"([A-Za-z0-9_]+):[^:"]+:(\d+)"\s*$`)

// parseShardedPkgs reads scripts/mutation.sh's SHARDED_PKGS array. It scans
// only the lines between the array's opening "SHARDED_PKGS=(" and its closing
// ")" — the population scripts/mutation.sh itself iterates at run time —
// never the whole file, so an unrelated quoted colon-separated string
// elsewhere can't be mistaken for a registry entry.
func parseShardedPkgs(t *testing.T, path string) []shardedPkgsEntry {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var entries []shardedPkgsEntry
	inArray := false
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "SHARDED_PKGS=(":
			inArray = true
			continue
		case !inArray:
			continue
		case trimmed == ")":
			inArray = false
			continue
		}
		m := shardedPkgsLine.FindStringSubmatch(line)
		if m == nil {
			t.Fatalf("SHARDED_PKGS line does not match the expected \"base:dir:count\" format: %q", line)
		}
		count, err := strconv.Atoi(m[2])
		if err != nil {
			t.Fatalf("parsing shard count in %q: %v", line, err)
		}
		entries = append(entries, shardedPkgsEntry{base: m[1], count: count})
	}
	if len(entries) == 0 {
		t.Fatalf("no SHARDED_PKGS entries found in %s", path)
	}
	return entries
}

var matrixSlugLine = regexp.MustCompile(`^\s*-\s*slug:\s*(\S+)\s*$`)

// parseMatrixSlugs reads every "- slug: <name>" line in mutation.yml. There is
// no YAML library in go.mod, so this is a narrow line scan of the
// mutation-baseline job's `matrix: include:` list, not a structural parse.
// The other matrix in this file (mutation-merge) is keyed "base: [...]", not
// "- slug:", so the scan cannot wander into it.
func parseMatrixSlugs(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var slugs []string
	for _, line := range strings.Split(string(data), "\n") {
		if m := matrixSlugLine.FindStringSubmatch(line); m != nil {
			slugs = append(slugs, m[1])
		}
	}
	if len(slugs) == 0 {
		t.Fatalf("no matrix slugs found in %s", path)
	}
	return slugs
}

// TestMutationMatrixMatchesShardedPackages proves mutation.yml's matrix
// carries exactly the <base>_1..N slugs scripts/mutation.sh's SHARDED_PKGS
// declares for each registered package. Neither file constrains the other at
// build time, and scripts/mutation.sh verify-shards only proves a package's
// files partition cleanly across its own declared count — it does not read
// mutation.yml, so a slug dropped from the matrix leaves that shard silently
// unrun while every other check stays green.
func TestMutationMatrixMatchesShardedPackages(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolving module root: %v", err)
	}
	entries := parseShardedPkgs(t, filepath.Join(root, "scripts", "mutation.sh"))
	slugs := parseMatrixSlugs(t, filepath.Join(root, ".github", "workflows", "mutation.yml"))
	slugSet := make(map[string]bool, len(slugs))
	for _, s := range slugs {
		slugSet[s] = true
	}

	checkedInSharded := map[string]bool{}
	checkedInMatrix := map[string]bool{}
	for _, entry := range entries {
		checkedInSharded[entry.base] = true
		prefix := entry.base + "_"

		var missing []string
		for i := 1; i <= entry.count; i++ {
			want := fmt.Sprintf("%s%d", prefix, i)
			if slugSet[want] {
				checkedInMatrix[entry.base] = true
			} else {
				missing = append(missing, want)
			}
		}

		var extra []string
		for _, s := range slugs {
			if !strings.HasPrefix(s, prefix) {
				continue
			}
			n, err := strconv.Atoi(strings.TrimPrefix(s, prefix))
			if err != nil || n < 1 || n > entry.count {
				extra = append(extra, s)
			}
		}

		if len(missing) > 0 || len(extra) > 0 {
			t.Errorf("%s: matrix slugs don't match SHARDED_PKGS count %d; missing %v, extra %v", entry.base, entry.count, missing, extra)
		}
	}

	// Anti-vacuity: name the two packages the registry is known to carry
	// today and assert each was actually located on both sides of the
	// comparison above — never a population count. A comparison loop gutted
	// to a no-op, or a parse that drops internal_services before the loop
	// sees it, leaves these unset while reporting no mismatches.
	for _, base := range []string{"internal_services", "internal_api"} {
		if !checkedInSharded[base] {
			t.Fatalf("%s not found in SHARDED_PKGS — the comparison above never ran on it", base)
		}
		if !checkedInMatrix[base] {
			t.Fatalf("%s not found among the matrix slugs — the comparison above never ran on it", base)
		}
	}
}
