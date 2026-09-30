// Command patchcov enforces the "every modified, coverable line is covered by
// tests" policy locally in CI, without depending on an external coverage
// service (which can stall and deadlock a required status check).
//
// It reads a Go coverage profile (go test -coverprofile, covermode=atomic) and
// the git diff of the current branch against a base ref, then fails when any
// added or modified Go line that is coverable (appears in the profile) has a
// zero execution count. Test files and tooling paths (scripts/, internal/testdb/)
// are excluded. Lines that do not appear in the profile at all are treated as
// non-coverable and ignored, matching codecov's "coverable lines" semantics.
//
// Usage (env): COVERAGE_FILE (default coverage.out), BASE_REF (default
// origin/main). Exit 0 when clean, 1 when uncovered modified lines exist.
package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

type coverBlock struct {
	start, end int
	covered    bool
}

const (
	stateNonCoverable = iota
	stateCovered
	stateUncovered
)

func main() {
	coverageFile := envOr("COVERAGE_FILE", "coverage.out")
	baseRef := envOr("BASE_REF", "origin/main")

	modulePath, err := readModulePath("go.mod")
	if err != nil {
		fatalf("read module path: %v", err)
	}

	profileData, err := os.ReadFile(coverageFile)
	if err != nil {
		fatalf("read coverage file %s: %v", coverageFile, err)
	}
	coverage := parseCoverageProfile(string(profileData), modulePath)

	diffOut, err := gitDiff(baseRef)
	if err != nil {
		fatalf("git diff against %s: %v", baseRef, err)
	}
	changed := parseDiffAddedLines(diffOut)

	var uncovered, underLeadingMarker []string
	for file, lines := range changed {
		blocks, ok := coverage[file]
		if !ok {
			continue // file absent from the profile -> non-coverable (no-test pkg, tooling)
		}
		fileUncovered, fileTrapped := uncoveredLines(file, blocks, lines)
		uncovered = append(uncovered, fileUncovered...)
		underLeadingMarker = append(underLeadingMarker, fileTrapped...)
	}

	if len(uncovered) > 0 {
		fmt.Fprint(os.Stderr, failureReport(uncovered, underLeadingMarker))
		os.Exit(1)
	}
	fmt.Println("patch coverage gate OK: every modified coverable line is covered by tests.")
}

// uncoveredLines returns the "file:line" entries of the changed lines that are
// uncovered and not excluded, plus the subset sitting under a leading
// codecov:ignore comment line, which the gate does not honour.
func uncoveredLines(file string, blocks []coverBlock, lines []int) (uncovered, underLeadingMarker []string) {
	ignored := markedLines(file)
	var source []string
	if data, err := os.ReadFile(file); err == nil {
		source = strings.Split(string(data), "\n")
	}
	for _, line := range lines {
		if lineState(blocks, line) != stateUncovered || lineIgnored(blocks, line, ignored) {
			continue
		}
		entry := fmt.Sprintf("%s:%d", file, line)
		uncovered = append(uncovered, entry)
		if hasLeadingIgnoreMarker(source, blocks, line) {
			underLeadingMarker = append(underLeadingMarker, entry)
		}
	}
	return uncovered, underLeadingMarker
}

// failureReport renders the gate's failure output. When some uncovered lines sit
// under a leading codecov:ignore comment, it names that trap and its fix; the
// verdict is unchanged, only the message grows.
func failureReport(uncovered, underLeadingMarker []string) string {
	uncovered = append([]string(nil), uncovered...)
	sort.Strings(uncovered)
	var b strings.Builder
	fmt.Fprintf(&b, "patch coverage gate FAILED: %d modified coverable line(s) not covered by tests:\n", len(uncovered))
	for _, u := range uncovered {
		fmt.Fprintf(&b, "  %s\n", u)
	}
	b.WriteString("\nAdd tests covering these lines, or, for a genuinely unreachable line,\n")
	b.WriteString("annotate it with a trailing // codecov:ignore (or wrap a region in\n")
	b.WriteString("// codecov:ignore:start ... // codecov:ignore:end) stating why.\n")
	if len(underLeadingMarker) > 0 {
		trapped := append([]string(nil), underLeadingMarker...)
		sort.Strings(trapped)
		b.WriteString("\nHint: these uncovered line(s) sit directly under a leading // codecov:ignore comment\n")
		b.WriteString("on its own line. The marker exempts only the line it is written on, so the comment\n")
		b.WriteString("exempts nothing beneath it. Move the marker to the end of the line it should exempt,\n")
		b.WriteString("or wrap the arm in // codecov:ignore:start ... // codecov:ignore:end:\n")
		for _, u := range trapped {
			fmt.Fprintf(&b, "  %s\n", u)
		}
	}
	return b.String()
}

// hasLeadingIgnoreMarker reports whether the nearest preceding non-blank line
// above the uncovered line — or above the start of an uncovered coverage block
// containing it, so the body of an if arm counts — is a comment-only line
// carrying codecov:ignore (not the :start or :end block delimiters).
func hasLeadingIgnoreMarker(source []string, blocks []coverBlock, line int) bool {
	starts := []int{line}
	for _, b := range blocks {
		if !b.covered && line >= b.start && line <= b.end {
			starts = append(starts, b.start)
		}
	}
	for _, start := range starts {
		for i := start - 2; i >= 0 && i < len(source); i-- {
			trimmed := strings.TrimSpace(source[i])
			if trimmed == "" {
				continue
			}
			if isLeadingIgnoreComment(trimmed) {
				return true
			}
			break
		}
	}
	return false
}

func isLeadingIgnoreComment(trimmed string) bool {
	return strings.HasPrefix(trimmed, "//") &&
		strings.Contains(trimmed, "codecov:ignore") &&
		!strings.Contains(trimmed, "codecov:ignore:start") &&
		!strings.Contains(trimmed, "codecov:ignore:end")
}

// parseCoverageProfile maps each repo-relative .go file to its coverage blocks.
// Profile lines look like:
//
//	<module>/<relpath>.go:<sl>.<sc>,<el>.<ec> <numStmts> <count>
func parseCoverageProfile(content, modulePath string) map[string][]coverBlock {
	result := map[string][]coverBlock{}
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}
		count, err := strconv.Atoi(fields[2])
		if err != nil {
			continue
		}
		colon := strings.LastIndex(fields[0], ":")
		if colon < 0 {
			continue
		}
		rel := strings.TrimPrefix(fields[0][:colon], modulePath+"/")
		startLine, endLine, ok := parseSpan(fields[0][colon+1:])
		if !ok {
			continue
		}
		result[rel] = append(result[rel], coverBlock{start: startLine, end: endLine, covered: count > 0})
	}
	return result
}

// parseSpan parses "<sl>.<sc>,<el>.<ec>" into start and end line numbers.
func parseSpan(span string) (start, end int, ok bool) {
	left, right, found := strings.Cut(span, ",")
	if !found {
		return 0, 0, false
	}
	start = lineNumber(left)
	end = lineNumber(right)
	if start == 0 || end == 0 {
		return 0, 0, false
	}
	return start, end, true
}

func lineNumber(s string) int {
	dot := strings.IndexByte(s, '.')
	if dot < 0 {
		return 0
	}
	n, err := strconv.Atoi(s[:dot])
	if err != nil {
		return 0
	}
	return n
}

// lineState reports whether a line is covered, uncovered, or non-coverable
// across the blocks of its file. A line is covered if it falls in any block
// with a non-zero count; uncovered if it falls only in zero-count blocks.
func lineState(blocks []coverBlock, line int) int {
	state := stateNonCoverable
	for _, b := range blocks {
		if line >= b.start && line <= b.end {
			if b.covered {
				return stateCovered
			}
			state = stateUncovered
		}
	}
	return state
}

// markedLines returns the 1-based line numbers in file annotated for exclusion
// from the gate, via a trailing "// codecov:ignore" comment or a
// "// codecov:ignore:start" ... "// codecov:ignore:end" block. Genuinely
// unreachable defensive code — e.g. crypto-primitive errors that cannot occur,
// or main() dependency wiring — is excluded this way rather than covered with a
// brittle fault-injection test. A missing/unreadable file marks nothing.
func markedLines(file string) map[int]bool {
	marked := map[int]bool{}
	data, err := os.ReadFile(file)
	if err != nil {
		return marked
	}
	inBlock := false
	for i, raw := range strings.Split(string(data), "\n") {
		lineNo := i + 1
		switch {
		case strings.Contains(raw, "codecov:ignore:start"):
			inBlock = true
			marked[lineNo] = true
		case strings.Contains(raw, "codecov:ignore:end"):
			inBlock = false
			marked[lineNo] = true
		case inBlock, strings.Contains(raw, "codecov:ignore"):
			marked[lineNo] = true
		}
	}
	return marked
}

// lineIgnored reports whether an uncovered line is excluded: either annotated
// directly, or sharing a zero-count coverage block with an annotated line — so a
// single marker on an error branch also excludes its body and closing brace.
func lineIgnored(blocks []coverBlock, line int, marked map[int]bool) bool {
	if marked[line] {
		return true
	}
	for _, b := range blocks {
		if line < b.start || line > b.end || b.covered {
			continue
		}
		for ln := b.start; ln <= b.end; ln++ {
			if marked[ln] {
				return true
			}
		}
	}
	return false
}

// parseDiffAddedLines parses `git diff --unified=0` output into the set of added
// new-file line numbers per gated .go file.
func parseDiffAddedLines(diff string) map[string][]int {
	result := map[string][]int{}
	var file string
	newLine := 0
	scanner := bufio.NewScanner(strings.NewReader(diff))
	scanner.Buffer(make([]byte, 1024*1024), 32*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "+++ b/"):
			file = strings.TrimPrefix(line, "+++ b/")
		case strings.HasPrefix(line, "@@"):
			newLine = hunkNewStart(line)
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			if isGatedFile(file) {
				result[file] = append(result[file], newLine)
			}
			newLine++
		}
	}
	return result
}

// hunkNewStart extracts c from a "@@ -a,b +c,d @@" hunk header.
func hunkNewStart(hunk string) int {
	plus := strings.IndexByte(hunk, '+')
	if plus < 0 {
		return 0
	}
	rest := hunk[plus+1:]
	end := strings.IndexAny(rest, ", ")
	if end < 0 {
		return 0
	}
	n, err := strconv.Atoi(rest[:end])
	if err != nil {
		return 0
	}
	return n
}

// isGatedFile reports whether a path is production Go code subject to the gate.
func isGatedFile(file string) bool {
	if !strings.HasSuffix(file, ".go") || strings.HasSuffix(file, "_test.go") {
		return false
	}
	for _, skip := range []string{"scripts/", "internal/testdb/"} {
		if strings.HasPrefix(file, skip) {
			return false
		}
	}
	return true
}

func readModulePath(goMod string) (string, error) {
	data, err := os.ReadFile(goMod)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "module "); ok {
			return strings.TrimSpace(rest), nil
		}
	}
	return "", fmt.Errorf("module directive not found in %s", goMod)
}

func gitDiff(baseRef string) (string, error) {
	out, err := exec.Command("git", "diff", "--unified=0", "--no-color", baseRef+"...HEAD", "--", "*.go").Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(2)
}
