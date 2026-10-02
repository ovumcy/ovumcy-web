// Command mutationpartition splits a package's non-test Go files into N
// disjoint, exhaustive shards of near-equal mutation work and prints one
// shard's file names, one per line, sorted.
//
// scripts/mutation.sh used to deal the files round-robin over the sorted
// listing, which evens out the file COUNT. gremlins' work is mutants, not
// files, and files differ in mutants by an order of magnitude: on the tree of
// 2026-09-25 the five internal/services shards held 462..868 mutation
// candidates and the five internal/api shards 231..311, so the heaviest cells
// ran into mutation.yml's 180-minute cap while the lightest finished early.
// Each file is weighed here by the operators gremlins' default mutators
// rewrite, and files are dealt heaviest first to the lightest shard so far
// (longest-processing-time first), which evens the shards to within one
// file's weight.
//
// The operator count alone ranks files badly, though: on an even ~43-weight
// split of internal/api, the finished shards of run 36942283202 took 19..145
// minutes (internal_services_9 hit the 180-minute cap), because a mutant costs the time until the first test that
// kills it, and which tests reach a file differs by an order of magnitude. So
// each file's weight is scaled by a measured rate — seconds of wall time per
// candidate — from costs.json, keyed by the package directory's base name. The
// rates are the per-mutant completion gaps in that run's logs, summed per file
// and shrunk toward the package mean by five pseudo-mutants; a file the table
// does not list (new, or never reached) costs the package mean, and a package
// it does not list costs 1 per candidate, which is the plain operator count.
//
// The file NAMES come from stdin — mutation.sh's shard_files is the single
// source of the population, so the partition and the proof that it covers
// every file read the same list — and each file's source is read from -dir.
// The weight only decides where a file lands; exhaustiveness and
// disjointness hold whatever it says, and `verify-shards` re-proves both.
package main

import (
	"bufio"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/scanner"
	"go/token"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

//go:embed costs.json
var costsJSON []byte

// PackageCosts is one package's measured seconds per mutation candidate: Files
// per file name, Default for a file the table does not list.
type PackageCosts struct {
	Default float64            `json:"default"`
	Files   map[string]float64 `json:"files"`
}

// loadCosts parses the embedded table, keyed by package directory base name.
func loadCosts() (map[string]PackageCosts, error) {
	var costs map[string]PackageCosts
	if err := json.Unmarshal(costsJSON, &costs); err != nil {
		return nil, fmt.Errorf("costs.json: %w", err)
	}
	return costs, nil
}

// Rate is the seconds one candidate of the named file costs: 1 for a package
// the table does not list.
func Rate(costs map[string]PackageCosts, pkg, name string) float64 {
	entry, ok := costs[pkg]
	if !ok {
		return 1
	}
	if rate, ok := entry.Files[name]; ok {
		return rate
	}
	return entry.Default
}

// Cost scales a candidate count by its rate, to whole seconds.
func Cost(weight int, rate float64) int {
	return int(math.Round(float64(weight) * rate))
}

// candidateWeight is how many mutants gremlins v0.6.0's default-enabled
// mutators (arithmetic-base, conditionals-boundary, conditionals-negation,
// increment-decrement, invert-negatives) make from one token. A comparison
// other than ==/!= yields a boundary and a negation mutant; `-` is either
// binary (arithmetic-base) or unary (invert-negatives), one mutant either way.
var candidateWeight = map[token.Token]int{
	token.ADD: 1, token.SUB: 1, token.MUL: 1, token.QUO: 1, token.REM: 1,
	token.EQL: 1, token.NEQ: 1,
	token.LSS: 2, token.LEQ: 2, token.GTR: 2, token.GEQ: 2,
	token.INC: 1, token.DEC: 1,
}

// fileNamePattern is what mutation.sh can turn into an exact --exclude-files
// regexp by escaping only the extension dot. A name outside it is refused
// rather than passed on: an unescaped metacharacter in an exclusion would
// match other files, and the shard would silently mutate the wrong set.
var fileNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.]+\.go$`)

// File is one member of the population with its estimated mutation weight.
type File struct {
	Name   string
	Weight int
}

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "mutationpartition: %v\n", err)
		os.Exit(2)
	}
}

func run(args []string, stdin io.Reader, stdout io.Writer) error {
	flags := flag.NewFlagSet("mutationpartition", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dir := flags.String("dir", "", "package directory the file names are read from")
	shard := flags.Int("shard", 0, "1-based index of the shard to print")
	of := flags.Int("of", 0, "total number of shards")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *dir == "" {
		return errors.New("-dir is required")
	}
	if *of < 1 {
		return fmt.Errorf("-of must be at least 1, got %d", *of)
	}
	if *shard < 1 || *shard > *of {
		return fmt.Errorf("-shard must be in [1,%d], got %d", *of, *shard)
	}

	names, err := readNames(stdin)
	if err != nil {
		return err
	}
	costs, err := loadCosts()
	if err != nil {
		return err
	}
	pkg := filepath.Base(*dir)
	files := make([]File, 0, len(names))
	for _, name := range names {
		src, err := os.ReadFile(filepath.Join(*dir, name))
		if err != nil {
			return err
		}
		files = append(files, File{Name: name, Weight: Cost(Weigh(src), Rate(costs, pkg, name))})
	}

	for _, name := range Partition(files, *of)[*shard-1] {
		if _, err := fmt.Fprintln(stdout, name); err != nil {
			return err
		}
	}
	return nil
}

// readNames reads the population, one base name per line. A test file, a
// name outside fileNamePattern or a duplicate is an error: each means the
// list is not the population shard_files is meant to produce.
func readNames(r io.Reader) ([]string, error) {
	var names []string
	seen := map[string]bool{}
	lines := bufio.NewScanner(r)
	for lines.Scan() {
		name := strings.TrimSpace(lines.Text())
		switch {
		case name == "":
			continue
		case !fileNamePattern.MatchString(name):
			return nil, fmt.Errorf("refusing file name %q: outside [A-Za-z0-9_.]+.go", name)
		case strings.HasSuffix(name, "_test.go"):
			return nil, fmt.Errorf("refusing %q: test files are not mutated", name)
		case seen[name]:
			return nil, fmt.Errorf("refusing %q: listed twice", name)
		}
		seen[name] = true
		names = append(names, name)
	}
	if err := lines.Err(); err != nil {
		return nil, err
	}
	return names, nil
}

// Weigh counts the mutation candidates in one Go source file. It scans tokens
// rather than text, so an operator inside a comment or a string literal does
// not count.
func Weigh(src []byte) int {
	fset := token.NewFileSet()
	file := fset.AddFile("", fset.Base(), len(src))
	var s scanner.Scanner
	s.Init(file, src, nil, 0)
	weight := 0
	for {
		_, tok, _ := s.Scan()
		if tok == token.EOF {
			return weight
		}
		weight += candidateWeight[tok]
	}
}

// Partition deals files into `of` shards, heaviest first, each to the shard
// with the least weight so far; a tie goes to the shard holding fewer files,
// then to the lower index. Within a weight, files go in name order. Every
// file lands in exactly one shard, the result does not depend on the input
// order, and each shard's names come back sorted.
//
// The file-count tie-break is what keeps a shard from coming up empty while
// there are files to spare: an empty shard always has the least weight AND
// the fewest files, so it takes the next file — zero-weight files included,
// which on a weight-only tie-break would all pile into the first shard.
func Partition(files []File, of int) [][]string {
	order := append([]File(nil), files...)
	sort.Slice(order, func(i, j int) bool {
		if order[i].Weight != order[j].Weight {
			return order[i].Weight > order[j].Weight
		}
		return order[i].Name < order[j].Name
	})

	shards := make([][]string, of)
	load := make([]int, of)
	for _, file := range order {
		lightest := 0
		for index := 1; index < of; index++ {
			if load[index] < load[lightest] ||
				(load[index] == load[lightest] && len(shards[index]) < len(shards[lightest])) {
				lightest = index
			}
		}
		shards[lightest] = append(shards[lightest], file.Name)
		load[lightest] += file.Weight
	}
	for _, names := range shards {
		sort.Strings(names)
	}
	return shards
}
