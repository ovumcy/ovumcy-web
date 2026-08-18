// Command covmerge combines the per-shard Go coverage profiles produced by the
// sharded unit lane (.github/workflows/ci.yml, jobs test-go-shard and
// test-go-rest) into the single profile its two consumers read: the Codecov
// upload and the required patch-coverage gate (scripts/patchcov).
//
// It exists because sharding a `go test` run splits its coverage with it. Both
// consumers used to receive ONE artifact produced by ONE run; with the lane
// split they receive one profile per shard, and a consumer handed a single
// shard's profile would report the coverage of a fraction of the suite as
// though it were the whole. patchcov would then fail a pull request for lines
// another shard covered.
//
// The lane runs with `-coverpkg` over every module tree, so each shard's
// profile lists every coverable block in the module — including blocks in
// packages that shard never executed, at count 0. Merging is therefore a union
// over block keys with the counts summed, and the union is what keeps a
// package that only one shard ran from reading as uncovered in the merged
// profile.
//
// Three properties are refused rather than papered over, because each one
// means the profiles being merged did not come from the same tree, and a
// merged profile that quietly disagrees with the source is worse than no
// profile at all: a mode line that differs between inputs, the same block
// carrying a different statement count in two inputs, and a line this command
// cannot parse. `-expect` refuses the failure that leaves no trace at all — an
// artifact that never arrived, which would silently merge fewer shards than
// the matrix produced and read as a coverage drop in the consumers.
//
// Usage: covmerge -in <dir> -glob <pattern> -out <file> -expect <n>
package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"flag"
)

// blockKey identifies one coverable block exactly as the profile line spells
// it: the file (module-relative import path plus file name, as `go test`
// writes it) and the source span. The statement count is deliberately NOT part
// of the key — two inputs disagreeing about it is the mismatch this command
// reports, and folding it into the key would turn that mismatch into two
// separate blocks nobody would notice.
type blockKey struct {
	file      string
	startLine int
	startCol  int
	endLine   int
	endCol    int
}

type blockValue struct {
	numStmt int
	count   int64
	source  string // the input that first carried this block, named in a mismatch
}

func main() {
	inDir := flag.String("in", "", "directory to search for shard coverage profiles")
	pattern := flag.String("glob", "coverage.out", "glob matched against each file's BASENAME under -in")
	outPath := flag.String("out", "", "path to write the merged profile")
	expect := flag.Int("expect", 0, "exact number of profiles that must be found (0 disables the check)")
	flag.Parse()

	if *inDir == "" || *outPath == "" {
		fatalf("usage: covmerge -in <dir> -glob <pattern> -out <file> [-expect <n>]")
	}

	matches, err := findProfiles(*inDir, *pattern)
	if err != nil {
		fatalf("find shard profiles: %v", err)
	}
	if len(matches) == 0 {
		fatalf("no coverage profile matched %s under %s — nothing to merge", *pattern, *inDir)
	}
	if err := RequireCount(matches, *expect); err != nil {
		fatalf("under %s: %v", *inDir, err)
	}

	mode, blocks, err := mergeProfiles(matches)
	if err != nil {
		fatalf("%v", err)
	}

	if err := os.MkdirAll(filepath.Dir(*outPath), 0o755); err != nil {
		fatalf("create output dir: %v", err)
	}
	if err := os.WriteFile(*outPath, []byte(Render(mode, blocks)), 0o644); err != nil {
		fatalf("write merged profile: %v", err)
	}

	fmt.Printf("merged %d shard profile(s), %d block(s), into %s\n", len(matches), len(blocks), *outPath)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "covmerge: "+format+"\n", args...)
	os.Exit(1)
}

// RequireCount refuses a set of inputs that is not the size the caller was
// promised. The check is against what the matrix was supposed to produce, not
// against "more than zero": a shard whose upload never arrived leaves the
// remaining profiles parsing cleanly and covering less, which reads downstream
// as a coverage drop rather than as a missing shard — and the required patch
// gate would fail a pull request over lines the absent shard had covered.
//
// `expect` of 0 disables the check, for a caller that genuinely does not know
// the count. CI always knows it: it is the matrix size plus the whole-package
// lane, both read from the workflow rather than written as a literal.
func RequireCount(matches []string, expect int) error {
	if expect <= 0 || len(matches) == expect {
		return nil
	}
	return fmt.Errorf("found %d profile(s), expected exactly %d: %s",
		len(matches), expect, strings.Join(matches, " "))
}

// findProfiles walks dir and returns every regular file whose BASENAME matches
// pattern, sorted. Shard profiles land in per-artifact subdirectories after
// actions/download-artifact, so the walk is the part that matters; the same
// shape as scripts/mutationmerge, which solves this for the mutation matrix.
func findProfiles(dir, pattern string) ([]string, error) {
	var matches []string
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		ok, err := filepath.Match(pattern, entry.Name())
		if err != nil {
			return err
		}
		if ok {
			matches = append(matches, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	return matches, nil
}

// mergeProfiles reads every profile and folds them into one block table.
func mergeProfiles(paths []string) (string, map[blockKey]blockValue, error) {
	mode := ""
	modeSource := ""
	blocks := map[blockKey]blockValue{}

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return "", nil, fmt.Errorf("read %s: %w", path, err)
		}
		fileMode, err := Accumulate(blocks, string(data), path)
		if err != nil {
			return "", nil, err
		}
		if mode == "" {
			mode, modeSource = fileMode, path
			continue
		}
		// Mixing modes is refused rather than resolved. `set` records 0/1 and
		// `atomic`/`count` record executions, so there is no combination rule
		// that is right for both, and picking one would silently rewrite what
		// half the inputs meant.
		if fileMode != mode {
			return "", nil, fmt.Errorf("mode mismatch: %s declares %q, %s declares %q",
				path, fileMode, modeSource, mode)
		}
	}

	return mode, blocks, nil
}

// Accumulate folds one profile's blocks into blocks and returns its mode.
//
// `set` mode is combined with a logical OR rather than a sum: its counts are
// booleans, and 1+1=2 would produce a profile whose own mode line says such a
// value cannot exist. `count` and `atomic` are execution counts and add.
func Accumulate(blocks map[blockKey]blockValue, profile, source string) (string, error) {
	scanner := bufio.NewScanner(strings.NewReader(profile))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	mode := ""
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if rest, found := strings.CutPrefix(line, "mode: "); found {
			if mode != "" {
				return "", fmt.Errorf("%s:%d: a second mode line %q", source, lineNumber, line)
			}
			mode = rest
			continue
		}
		if mode == "" {
			return "", fmt.Errorf("%s:%d: a block line before the mode line: %q", source, lineNumber, line)
		}

		key, numStmt, count, err := parseBlock(line)
		if err != nil {
			return "", fmt.Errorf("%s:%d: %w", source, lineNumber, err)
		}

		previous, seen := blocks[key]
		if seen && previous.numStmt != numStmt {
			// The same span carrying a different statement count means the two
			// profiles were produced from different source trees. Summing them
			// would hand the consumers a profile that matches neither tree.
			return "", fmt.Errorf("%s:%d: block %s:%d.%d,%d.%d has %d statements here and %d in %s — profiles from different trees",
				source, lineNumber, key.file, key.startLine, key.startCol, key.endLine, key.endCol,
				numStmt, previous.numStmt, previous.source)
		}

		merged := blockValue{numStmt: numStmt, count: count, source: source}
		if seen {
			merged.source = previous.source
			if mode == "set" {
				merged.count = max(previous.count, count)
			} else {
				merged.count = previous.count + count
			}
		}
		blocks[key] = merged
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read %s: %w", source, err)
	}
	if mode == "" {
		return "", fmt.Errorf("%s: no mode line — not a Go coverage profile", source)
	}

	return mode, nil
}

// parseBlock reads one profile line: `<file>:<line>.<col>,<line>.<col> <numStmt> <count>`.
//
// Every part is parsed rather than pattern-matched loosely: a line this
// command cannot fully account for is an error, because a profile it silently
// dropped a block from would understate coverage in the required patch gate.
func parseBlock(line string) (blockKey, int, int64, error) {
	fields := strings.Fields(line)
	if len(fields) != 3 {
		return blockKey{}, 0, 0, fmt.Errorf("unrecognised profile line: %q", line)
	}

	span := fields[0]
	colon := strings.LastIndex(span, ":")
	if colon < 1 {
		return blockKey{}, 0, 0, fmt.Errorf("unrecognised profile line: %q", line)
	}
	file := span[:colon]

	start, end, found := strings.Cut(span[colon+1:], ",")
	if !found {
		return blockKey{}, 0, 0, fmt.Errorf("unrecognised profile line: %q", line)
	}
	startLine, startCol, err := parsePosition(start)
	if err != nil {
		return blockKey{}, 0, 0, fmt.Errorf("unrecognised profile line: %q", line)
	}
	endLine, endCol, err := parsePosition(end)
	if err != nil {
		return blockKey{}, 0, 0, fmt.Errorf("unrecognised profile line: %q", line)
	}

	numStmt, err := strconv.Atoi(fields[1])
	if err != nil || numStmt < 0 {
		return blockKey{}, 0, 0, fmt.Errorf("unrecognised statement count in %q", line)
	}
	count, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil || count < 0 {
		return blockKey{}, 0, 0, fmt.Errorf("unrecognised execution count in %q", line)
	}

	return blockKey{file: file, startLine: startLine, startCol: startCol, endLine: endLine, endCol: endCol},
		numStmt, count, nil
}

func parsePosition(position string) (int, int, error) {
	line, column, found := strings.Cut(position, ".")
	if !found {
		return 0, 0, fmt.Errorf("not a line.column position: %q", position)
	}
	lineNumber, err := strconv.Atoi(line)
	if err != nil || lineNumber < 1 {
		return 0, 0, fmt.Errorf("not a line number: %q", line)
	}
	columnNumber, err := strconv.Atoi(column)
	if err != nil || columnNumber < 1 {
		return 0, 0, fmt.Errorf("not a column number: %q", column)
	}
	return lineNumber, columnNumber, nil
}

// Render writes the merged table back in profile syntax, sorted by file and
// then by position. `go tool cover` and Codecov both accept any order, and
// determinism is for the humans: a merged profile that reorders itself between
// runs cannot be diffed when a coverage number moves unexpectedly.
func Render(mode string, blocks map[blockKey]blockValue) string {
	keys := make([]blockKey, 0, len(blocks))
	for key := range blocks {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left, right := keys[i], keys[j]
		switch {
		case left.file != right.file:
			return left.file < right.file
		case left.startLine != right.startLine:
			return left.startLine < right.startLine
		case left.startCol != right.startCol:
			return left.startCol < right.startCol
		case left.endLine != right.endLine:
			return left.endLine < right.endLine
		default:
			return left.endCol < right.endCol
		}
	})

	var out strings.Builder
	fmt.Fprintf(&out, "mode: %s\n", mode)
	for _, key := range keys {
		block := blocks[key]
		fmt.Fprintf(&out, "%s:%d.%d,%d.%d %d %d\n",
			key.file, key.startLine, key.startCol, key.endLine, key.endCol, block.numStmt, block.count)
	}
	return out.String()
}
