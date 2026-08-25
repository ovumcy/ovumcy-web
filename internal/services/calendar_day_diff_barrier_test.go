package services

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Barrier for the "day difference computed as an hour difference" class, the
// arithmetic sibling of the anchor class swept by
// calendar_day_construction_barrier_test.go.
//
// Measuring the distance between two time.Time values through
// `a.Sub(b).Hours()` asks how many HOURS separate two instants. Every caller in
// this tree wants a different number: how many CALENDAR DAYS separate two dates.
// The two answers agree only while both operands happen to be anchored at the
// same midnight. They diverge in two independent ways:
//
//   - a DST transition makes a calendar day 23 or 25 hours long, so a two-day
//     span across Europe/Berlin 2026-03-28 -> 2026-03-30 measures 47 hours and
//     truncates to 1; and
//   - a location midnight against a UTC midnight is a sub-day offset that
//     truncates the same way on every ordinary day in every non-UTC zone.
//
// CalendarDaysBetween exists to be the one answer: it re-anchors both operands
// to UTC midnight of their own calendar day first, so neither divergence can
// reach the subtraction. A site that spells the subtraction out again is safe
// only for as long as an unstated invariant about its CALLER holds, and nothing
// at the site says which invariant that is.
//
// This sweep therefore refuses the SHAPE, not a spelling of the divisor: any
// `<value>.Sub(<value>).Hours()` in shipped Go. Dividing by 24, by a named
// constant, or comparing the hours against a literal are the same defect.
//
// Scope note (deliberate, not an oversight): _test.go files are not swept. A
// test that computed its expectation with CalendarDaysBetween would be checking
// CalendarDaysBetween against itself; an independent oracle spelled out in hours
// is what makes such a test worth running.
const calendarDayDiffPoint = "internal/services/day_utils.go:CalendarDaysBetween"

// calendarDayDiffAllowlist carries the sites this sweep flags and tolerates,
// each with its reason. The key is `<path>:<enclosing function>` so an
// unrelated edit above a site does not turn the barrier red.
//
// An entry belongs here ONLY when the site is genuinely asking for elapsed
// HOURS — a TTL, a freshness window, a rate-limit interval — because such a
// site is not measuring a calendar-day span at all and CalendarDaysBetween
// would be the wrong instrument for it. A site that wants days belongs in
// CalendarDaysBetween, never here. The set must match the sweep EXACTLY: a
// stale entry fails too, so a fixed site cannot leave its exemption behind.
var calendarDayDiffAllowlist = map[string]string{}

// calendarDayDiffBlindSpots is what this sweep provably cannot see, printed
// with every failure so a green run is never read as a cleared tree.
var calendarDayDiffBlindSpots = []string{
	"only the direct `x.Sub(y).Hours()` chain is read: a Sub result stored in a local (or returned by a helper) and converted to hours somewhere else is invisible",
	"other routes from a Duration to days are invisible too — Duration/(24*time.Hour), Duration.Truncate, UnixNano differences, a helper that hides either",
	"_test.go files are out of scope on purpose (see the note above), so a test oracle spelled in hours never appears here",
	"non-Go surfaces are out of scope: the JavaScript bundle and the templates are not parsed here",
}

// calendarDayDiffFileFloor guards the WALK against a vacuous pass: a broken
// discovery would otherwise sweep an empty tree and report success.
//
// The MATCHER is guarded by a positive control instead of by a count, because
// after this class is cleared the whole module holds only a handful of
// `.Sub(...)` sites and a count that low proves nothing: CalendarDaysBetween
// itself is a member of the shape, so if the sweep reports no finding at
// calendarDayDiffPoint it has stopped reading the chain and the run fatals.
const calendarDayDiffFileFloor = 200

type calendarDayDiffFinding struct {
	key      string
	position string
}

type calendarDayDiffScan struct {
	findings []calendarDayDiffFinding
	files    int
}

// TestCalendarDayDiffBarrier refuses every shipped site that measures the
// distance between two time values in hours, except CalendarDaysBetween itself.
func TestCalendarDayDiffBarrier(t *testing.T) {
	t.Parallel()

	scan := scanCalendarDayDiffShapes(t)

	if scan.files < calendarDayDiffFileFloor {
		t.Fatalf("the sweep parsed %d shipped Go file(s), fewer than the floor of %d — discovery is broken, so a green verdict here would mean nothing", scan.files, calendarDayDiffFileFloor)
	}
	unexplained := make([]calendarDayDiffFinding, 0, len(scan.findings))
	matched := map[string]bool{}
	sanctioned := 0
	for _, finding := range scan.findings {
		if finding.key == calendarDayDiffPoint {
			sanctioned++
			continue
		}
		if _, allowed := calendarDayDiffAllowlist[finding.key]; allowed {
			matched[finding.key] = true
			continue
		}
		unexplained = append(unexplained, finding)
	}

	if sanctioned == 0 {
		t.Fatalf("nothing was found at %s — the single sanctioned day-difference site moved or was renamed, and this sweep is now exempting a function that no longer exists", calendarDayDiffPoint)
	}

	if len(unexplained) > 0 {
		lines := make([]string, 0, len(unexplained))
		for _, finding := range unexplained {
			lines = append(lines, fmt.Sprintf("  %s\n    key: %s", finding.position, finding.key))
		}
		sort.Strings(lines)
		t.Errorf("%d site(s) measure a day difference in hours:\n%s\n%s", len(unexplained), strings.Join(lines, "\n"), calendarDayDiffGuidance())
	}

	stale := make([]string, 0, len(calendarDayDiffAllowlist))
	for key := range calendarDayDiffAllowlist {
		if !matched[key] {
			stale = append(stale, key)
		}
	}
	if len(stale) > 0 {
		sort.Strings(stale)
		t.Errorf("%d allowlist entr(y/ies) match nothing in the tree:\n  %s\nA site that was fixed, moved or renamed must not leave its exemption behind — a stale line makes the list read shorter than it is.", len(stale), strings.Join(stale, "\n  "))
	}
}

func calendarDayDiffGuidance() string {
	var builder strings.Builder
	builder.WriteString("How to clear a finding:\n")
	builder.WriteString("  - the site wants CALENDAR DAYS (a cycle length, a gap between period days, a countdown to a predicted date): call CalendarDaysBetween(from, to), which re-anchors both operands to UTC midnight of their own calendar day before subtracting. It is exactly `int(hours/24)` on operands a DST transition and a location midnight can no longer reach.\n")
	builder.WriteString("  - the site really wants elapsed HOURS (a TTL, a freshness window, a rate-limit interval): it is not a calendar-day span at all, so say so in calendarDayDiffAllowlist. Adding a line there is a deliberate act and owes a one-line reason, not a bare path.\n")
	builder.WriteString("What this sweep cannot see, so a green run is not a cleared tree:\n")
	for _, blindSpot := range calendarDayDiffBlindSpots {
		builder.WriteString("  - " + blindSpot + "\n")
	}
	return builder.String()
}

// scanCalendarDayDiffShapes parses every shipped (non-test) Go file in the
// module and reports each `x.Sub(y).Hours()` chain.
func scanCalendarDayDiffShapes(t *testing.T) calendarDayDiffScan {
	t.Helper()

	root := calendarDayDiffRepoRoot(t)
	fileSet := token.NewFileSet()
	scan := calendarDayDiffScan{}

	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			name := entry.Name()
			if path != root && (strings.HasPrefix(name, ".") || name == "node_modules") {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		parsed, parseErr := parser.ParseFile(fileSet, path, nil, 0)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", path, parseErr)
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			t.Fatalf("relative path of %s: %v", path, relErr)
		}
		scan.files++
		inspectCalendarDayDiffFile(fileSet, parsed, filepath.ToSlash(relative), &scan)
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk %s: %v", root, walkErr)
	}
	return scan
}

// calendarDayDiffRepoRoot climbs to the directory holding go.mod, so the sweep
// covers the whole module rather than the package it happens to live in.
func calendarDayDiffRepoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod found above %s", dir)
		}
		dir = parent
	}
}

func inspectCalendarDayDiffFile(fileSet *token.FileSet, file *ast.File, relative string, scan *calendarDayDiffScan) {
	ranges := calendarDayFunctionRanges(file)

	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "Hours" || len(call.Args) != 0 {
			return true
		}
		inner, ok := selector.X.(*ast.CallExpr)
		if !ok || !calendarDayDiffIsSubCall(inner) {
			return true
		}

		position := fileSet.Position(call.Pos())
		scan.findings = append(scan.findings, calendarDayDiffFinding{
			key:      relative + ":" + calendarDayEnclosingFunction(ranges, call.Pos()),
			position: fmt.Sprintf("%s:%d:%d", relative, position.Line, position.Column),
		})
		return true
	})
}

// calendarDayDiffIsSubCall reports the one-argument call shape `<expr>.Sub(<expr>)`,
// which is what time.Time.Sub looks like. Only the `.Hours()` chain on top of it
// produces a finding, so a one-argument Sub on some other type is inert here.
func calendarDayDiffIsSubCall(call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	return selector.Sel.Name == "Sub" && len(call.Args) == 1
}
