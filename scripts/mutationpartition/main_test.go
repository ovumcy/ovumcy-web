package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// synthesisedFiles builds a population of its own with uneven weights, the
// shape that made round-robin by file count uneven, rather than reading this
// repository: a partitioner is judged on a property, and a fixture that reads
// the real tree would pass for as long as that tree happened to be convenient.
func synthesisedFiles(count int) []File {
	files := make([]File, 0, count)
	for index := range count {
		files = append(files, File{Name: fmt.Sprintf("file_%03d.go", index), Weight: (index * 37) % 101})
	}
	return files
}

// The property mutation.sh's exclusions rest on: a file in no shard is never
// mutated while every shard reports success, and a file in two is mutated
// twice and double-counted by the merge.
func TestPartitionIsExhaustiveAndDisjoint(t *testing.T) {
	for _, of := range []int{1, 2, 3, 5, 10, 14} {
		for _, count := range []int{0, 1, 2, 7, 13, 104, 123} {
			files := synthesisedFiles(count)
			seen := map[string]int{}
			for _, names := range Partition(files, of) {
				for _, name := range names {
					seen[name]++
				}
			}
			if len(seen) != count {
				t.Fatalf("of=%d count=%d: %d distinct files placed, want %d", of, count, len(seen), count)
			}
			for _, file := range files {
				if seen[file.Name] != 1 {
					t.Fatalf("of=%d count=%d: %q placed %d times, want exactly 1", of, count, file.Name, seen[file.Name])
				}
			}
		}
	}
}

// What the weighting buys, as a bound rather than a snapshot: dealing
// heaviest-first to the lightest shard leaves no two shards further apart
// than the heaviest single file.
func TestPartitionEvensTheWeightToWithinOneFile(t *testing.T) {
	for _, of := range []int{2, 5, 10, 14} {
		files := synthesisedFiles(123)
		weight := map[string]int{}
		heaviest := 0
		for _, file := range files {
			weight[file.Name] = file.Weight
			heaviest = max(heaviest, file.Weight)
		}
		loads := []int{}
		for _, names := range Partition(files, of) {
			load := 0
			for _, name := range names {
				load += weight[name]
			}
			loads = append(loads, load)
		}
		if spread := slices.Max(loads) - slices.Min(loads); spread > heaviest {
			t.Fatalf("of=%d: shard loads %v spread %d, more than the heaviest file's %d", of, loads, spread, heaviest)
		}
	}
}

// Zero-weight files must spread too: on a weight-only tie-break they would
// all land in shard 1, and with as many files as shards some shard would come
// up empty and exclude the whole package.
func TestPartitionLeavesNoShardEmptyWhileFilesRemain(t *testing.T) {
	files := []File{{"a.go", 0}, {"b.go", 0}, {"c.go", 0}, {"d.go", 9}}
	for of := 1; of <= len(files); of++ {
		for index, names := range Partition(files, of) {
			if len(names) == 0 {
				t.Fatalf("of=%d: shard %d is empty with %d files to deal", of, index+1, len(files))
			}
		}
	}
}

// The split may not depend on the order the listing arrived in: every shard
// computes the partition on its own, and two orders disagreeing about who
// owns a file leave that file owned by nobody.
func TestPartitionIgnoresInputOrder(t *testing.T) {
	files := synthesisedFiles(40)
	reversed := slices.Clone(files)
	slices.Reverse(reversed)
	for _, of := range []int{3, 7} {
		if got, want := Partition(reversed, of), Partition(files, of); !slices.EqualFunc(got, want, slices.Equal) {
			t.Fatalf("of=%d: reversed input gave %v, want %v", of, got, want)
		}
	}
}

// An operator is counted only where gremlins can mutate it: in code, not in a
// comment or a string literal.
func TestWeighCountsOnlyMutableOperatorsInCode(t *testing.T) {
	src := []byte(`package p

// a < b + c is prose, not code
func f(a, b int) int {
	s := "x + y <= z"
	for i := 0; i < a; i++ { // <, ++
		b = b - a*2
	}
	if a == b || a >= -b {
		return len(s) % 3
	}
	return b / 2
}
`)
	// <  2, ++ 1, - 1, * 1, == 1, >= 2, unary - 1, % 1, / 1
	if got, want := Weigh(src), 11; got != want {
		t.Fatalf("Weigh = %d, want %d", got, want)
	}
}

func TestReadNamesRefusesWhatShardFilesCannotProduce(t *testing.T) {
	for _, listing := range []string{
		"a.go\nb c.go\n",   // a space would split the exclusion argument
		"a.go\nsub/b.go\n", // a path, not a base name
		"a.go\na_test.go\n",
		"a.go\na.go\n",
		"a.go\nb.g[o]\n",
	} {
		if _, err := readNames(strings.NewReader(listing)); err == nil {
			t.Errorf("readNames accepted %q", listing)
		}
	}
	names, err := readNames(strings.NewReader("b.go\n\na.go\n"))
	if err != nil || !slices.Equal(names, []string{"b.go", "a.go"}) {
		t.Fatalf("readNames = %v, %v; want [b.go a.go], nil", names, err)
	}
}

// The command end to end: names from stdin, sources from -dir, one shard out.
func TestRunPrintsTheRequestedShardFromTheListedFiles(t *testing.T) {
	dir := t.TempDir()
	sources := map[string]string{
		"heavy.go": "package p\nvar x = 1 + 2 + 3 + 4 + 5 + 6\n",
		"light.go": "package p\nvar y = 1 + 2\n",
		"none.go":  "package p\n",
	}
	for name, src := range sources {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	listing := "heavy.go\nlight.go\nnone.go\n"

	var shard1, shard2 bytes.Buffer
	if err := run([]string{"-dir", dir, "-shard", "1", "-of", "2"}, strings.NewReader(listing), &shard1); err != nil {
		t.Fatalf("shard 1: %v", err)
	}
	if err := run([]string{"-dir", dir, "-shard", "2", "-of", "2"}, strings.NewReader(listing), &shard2); err != nil {
		t.Fatalf("shard 2: %v", err)
	}
	if got, want := shard1.String(), "heavy.go\n"; got != want {
		t.Errorf("shard 1 = %q, want %q", got, want)
	}
	if got, want := shard2.String(), "light.go\nnone.go\n"; got != want {
		t.Errorf("shard 2 = %q, want %q", got, want)
	}
}

func TestRunRefusesABadInvocation(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []struct {
		args    []string
		listing string
	}{
		{[]string{"-shard", "1", "-of", "1"}, ""},
		{[]string{"-dir", dir, "-shard", "1", "-of", "0"}, ""},
		{[]string{"-dir", dir, "-shard", "3", "-of", "2"}, ""},
		{[]string{"-dir", dir, "-shard", "0", "-of", "2"}, ""},
		{[]string{"-dir", dir, "-no-such-flag"}, ""},
		{[]string{"-dir", dir, "-shard", "1", "-of", "1"}, "missing.go\n"},
		{[]string{"-dir", dir, "-shard", "1", "-of", "1"}, "x_test.go\n"},
	} {
		if err := run(c.args, strings.NewReader(c.listing), &bytes.Buffer{}); err == nil {
			t.Errorf("run(%v) with listing %q succeeded, want an error", c.args, c.listing)
		}
	}
}
