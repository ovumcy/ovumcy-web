// Command racepartition splits a `go test -list` listing into N disjoint,
// exhaustive shards and prints one of them as an anchored `-run` regexp.
//
// It exists because the race lane is dominated by a single package.
// internal/services took 461 s of the lane's 589 s (measured 2026-08-15), and
// 75% of that sits in the 99 tests whose files hash passwords: bcrypt is
// deliberately expensive and the race detector multiplies exactly that kind of
// code hardest. Go has no native test sharding below package granularity, so
// the split has to be expressed as a `-run` regexp.
//
// The names come from `go test -list`, never from a hand-written pattern. That
// is the whole design: a rule keyed on a SPELLING silently stops covering
// whatever the spelling missed, and here the failure would be a test that
// matches no shard's regexp and therefore runs NOWHERE while every shard stays
// green. Reading the list from the toolchain makes a new test land in a shard
// by construction, and `TestPartitionsAreExhaustiveAndDisjoint` pins that
// property against a synthesised listing.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
)

// testNamePattern is what a Go test identifier can be. Anything else in the
// listing is refused rather than escaped: a name carrying a regexp
// metacharacter would change the meaning of the alternation this builds, and
// silently selecting the wrong tests is the failure this command exists to
// prevent.
var testNamePattern = regexp.MustCompile(`^(?:Test|Example|Fuzz)[\p{L}\p{N}_]*$`)

// benchmarkPattern names the one kind of entry the listing carries that `-run`
// does not govern: benchmarks execute only under `-bench`. They are recognised
// and deliberately left out of the alternation rather than skipped as noise —
// an entry this command cannot name is an error, and one it can name but must
// not select is a decision worth having in the code.
//
// `go test -list` printing benchmarks at all was measured, not assumed:
// internal/services carries BenchmarkBuildCycleStats, and the first run against
// the real listing refused it, which is what this pattern answers.
var benchmarkPattern = regexp.MustCompile(`^Benchmark[\p{L}\p{N}_]*$`)

// matchesNothing is an empty character class: a negated class covering every
// code point can match no character, so the pattern matches no string at all —
// the empty string included.
const matchesNothing = `[^\x00-\x{10FFFF}]`

func main() {
	shard := flag.Int("shard", 0, "1-based index of the shard to print")
	of := flag.Int("of", 0, "total number of shards")
	flag.Parse()

	if *of < 1 {
		fail("-of must be at least 1, got %d", *of)
	}
	if *shard < 1 || *shard > *of {
		fail("-shard must be in [1,%d], got %d", *of, *shard)
	}

	names, err := readNames(os.Stdin)
	if err != nil {
		fail("%v", err)
	}

	fmt.Println(BuildRunRegexp(names, *shard, *of))
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "racepartition: "+format+"\n", args...)
	os.Exit(2)
}

// readNames keeps the test identifiers out of a `go test -list` listing. The
// listing ends with the package's own `ok  <pkg> <time>` line, and a build or
// vet failure can add anything at all; a line that is neither a test name nor
// one of those trailers is an error rather than something to skip past,
// because a listing this command cannot fully account for is a listing whose
// shards cannot be proven exhaustive.
func readNames(r io.Reader) ([]string, error) {
	var names []string
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case line == "":
			continue
		case testNamePattern.MatchString(line):
			names = append(names, line)
		case benchmarkPattern.MatchString(line):
			continue
		case strings.HasPrefix(line, "ok "), strings.HasPrefix(line, "?   "),
			strings.HasPrefix(line, "no test files"):
			continue
		default:
			return nil, fmt.Errorf("unrecognised line in the listing: %q", line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return names, nil
}

// BuildRunRegexp returns the anchored `-run` value selecting the given shard.
//
// Sorting first makes the assignment independent of the order the toolchain
// happened to print, so the same tree always produces the same split; the
// round-robin then spreads a run of related (and usually similarly priced)
// tests across shards instead of dropping a whole expensive file into one.
//
// An empty shard yields an empty character class, which matches no string at
// all. `^$` reads like the obvious spelling and is not the same thing: it
// matches the empty string, so the pattern's meaning would rest on "no test is
// named the empty string" rather than on the pattern itself. Returning
// something that matches EVERYTHING would be the dangerous version of the same
// edge — every shard would run the whole package and it would read only as the
// lane getting slower.
func BuildRunRegexp(names []string, shard, of int) string {
	sorted := append([]string(nil), names...)
	sort.Strings(sorted)

	var selected []string
	for index, name := range sorted {
		if index%of == shard-1 {
			selected = append(selected, name)
		}
	}

	if len(selected) == 0 {
		return matchesNothing
	}

	return "^(?:" + strings.Join(selected, "|") + ")$"
}
