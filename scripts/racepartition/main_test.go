package main

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// synthesisedListing builds a listing of its own rather than measuring this
// repository. A partitioner is judged on a property — every test lands in
// exactly one shard — and a fixture that reads the real tree would pass for as
// long as that tree happened to be convenient, which is the shape of a green
// fixture that asserts nothing.
func synthesisedListing(count int) []string {
	names := make([]string, 0, count)
	for index := range count {
		names = append(names, fmt.Sprintf("TestSynthesised%03d", index))
	}
	return names
}

// This is the property the whole design rests on. A test matching no shard's
// regexp would run NOWHERE while every shard reported success — a false green
// that no other check in the repository could see.
func TestPartitionsAreExhaustiveAndDisjoint(t *testing.T) {
	for _, of := range []int{1, 2, 3, 5, 8} {
		for _, count := range []int{0, 1, 2, 7, 99, 100} {
			names := synthesisedListing(count)

			seen := map[string]int{}
			for shard := 1; shard <= of; shard++ {
				matcher := regexp.MustCompile(BuildRunRegexp(names, shard, of))
				for _, name := range names {
					if matcher.MatchString(name) {
						seen[name]++
					}
				}
			}

			for _, name := range names {
				if seen[name] != 1 {
					t.Fatalf("of=%d count=%d: %q matched %d shards, want exactly 1",
						of, count, name, seen[name])
				}
			}
		}
	}
}

// A shard's regexp must not select a test whose name merely CONTAINS a
// selected one. `go test -run` is an unanchored match by default, so an
// alternation written without anchors would pull TestFoo's neighbours in with
// it and run them twice across the fleet.
func TestShardRegexpIsAnchored(t *testing.T) {
	names := []string{"TestFoo", "TestFooBar"}
	matcher := regexp.MustCompile(BuildRunRegexp(names, 1, 2))

	selected := []string{}
	for _, name := range names {
		if matcher.MatchString(name) {
			selected = append(selected, name)
		}
	}
	if len(selected) != 1 {
		t.Fatalf("shard 1 of 2 selected %v, want exactly one of %v", selected, names)
	}
}

// An empty shard must match NOTHING. The dangerous spelling of this edge is a
// regexp that matches everything, which would run the whole package on every
// shard and read only as "the lane got slower".
func TestEmptyShardMatchesNothing(t *testing.T) {
	matcher := regexp.MustCompile(BuildRunRegexp(synthesisedListing(2), 3, 3))

	for _, name := range []string{"TestSynthesised000", "TestSynthesised001", "", "Test"} {
		if matcher.MatchString(name) {
			t.Fatalf("empty shard matched %q", name)
		}
	}
}

// The split may not depend on the order the toolchain printed the listing, or
// two shards computed from differently-ordered runs would disagree about who
// owns a test — and a test owned by nobody runs nowhere.
func TestPartitionIgnoresListingOrder(t *testing.T) {
	names := synthesisedListing(20)
	reversed := make([]string, len(names))
	for index, name := range names {
		reversed[len(names)-1-index] = name
	}

	for shard := 1; shard <= 3; shard++ {
		if got, want := BuildRunRegexp(reversed, shard, 3), BuildRunRegexp(names, shard, 3); got != want {
			t.Fatalf("shard %d: reversed listing gave %q, want %q", shard, got, want)
		}
	}
}

// Benchmarks appear in the listing and must be recognised without being
// selected: `go test -run` never executes them. Measured against the real
// listing, which carries BenchmarkBuildCycleStats.
func TestReadNamesRecognisesBenchmarksWithoutSelectingThem(t *testing.T) {
	names, err := readNames(strings.NewReader("TestAlpha\nBenchmarkBuildCycleStats\n"))
	if err != nil {
		t.Fatalf("readNames refused a benchmark line: %v", err)
	}
	if len(names) != 1 || names[0] != "TestAlpha" {
		t.Fatalf("readNames = %v, want just [TestAlpha]", names)
	}
}

func TestReadNamesKeepsTestsAndToleratesTrailers(t *testing.T) {
	listing := strings.Join([]string{
		"TestAlpha",
		"ExampleBeta",
		"FuzzGamma",
		"",
		"ok  \tgithub.com/ovumcy/ovumcy-web/internal/services\t0.412s",
	}, "\n")

	names, err := readNames(strings.NewReader(listing))
	if err != nil {
		t.Fatalf("readNames: %v", err)
	}
	if want := []string{"TestAlpha", "ExampleBeta", "FuzzGamma"}; strings.Join(names, ",") != strings.Join(want, ",") {
		t.Fatalf("readNames = %v, want %v", names, want)
	}
}

// A listing carrying something this command cannot account for must fail
// loudly. Skipping the unknown line would drop whatever it named out of every
// shard, which is the exact false green the exhaustiveness property forbids.
func TestReadNamesRefusesAnUnaccountableLine(t *testing.T) {
	if _, err := readNames(strings.NewReader("TestAlpha\nFAIL\tsome/pkg [build failed]\n")); err == nil {
		t.Fatal("readNames accepted a listing it could not account for")
	}
}

// A name carrying a regexp metacharacter would change the alternation's
// meaning, so it is refused rather than escaped.
func TestReadNamesRefusesAMetacharacterName(t *testing.T) {
	if _, err := readNames(strings.NewReader("TestAlpha|TestBeta\n")); err == nil {
		t.Fatal("readNames accepted a name carrying a regexp metacharacter")
	}
}
