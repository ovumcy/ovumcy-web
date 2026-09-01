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

// Barrier for the calendar-day-as-instant class (issue #48 family).
//
// A value whose meaning is a CALENDAR DAY has exactly one safe shape in this
// tree: it is built through the package's single construction point
// (StartOfCalendarDay, reached via CalendarDay / DateAtLocation / ParseDayDate)
// and it is ordered against another calendar day through CalendarDaysBetween.
// Every instance of the class came from one of two shapes:
//
//	(a) a calendar day built directly as an instant in a non-UTC location —
//	    time.Date(..., location) or time.ParseInLocation — outside that
//	    construction point, so a zone whose midnight does not exist normalizes
//	    the value into the PREVIOUS day; and
//	(b) a raw Before/After/Equal/Sub between a value anchored at a location
//	    midnight and a value anchored at UTC midnight — the two name the same
//	    calendar day but are different instants, so the comparison misreads it
//	    every day, in every non-UTC zone, with no DST transition involved.
//
// The sweep below reads the shipped source rather than a list of known sites,
// so a site added later is covered the moment it is written. What it cannot see
// is stated on calendarDayBarrierBlindSpots and repeated in the failure text:
// shape (b) is decided from LOCAL variables inside one function only. An anchor
// that arrives as a parameter, a struct field or another package's return value
// is invisible to it, which is the deliberate trade — a barrier that guessed at
// those would flag safe code and be switched off.
//
// calendarDayBarrierAllowlist is the visible "still to do" list. Adding a line
// to it is a deliberate act and owes a reason, not a path.

// anchorClass is the midnight shape a value carries: which instant a calendar
// day was pinned to. Comparing two known-and-different classes is the defect.
type anchorClass int

const (
	anchorUnknown anchorClass = iota
	anchorUTC
	anchorLocation
)

// calendarDayConstructionPoint is the ONE function allowed to build a calendar
// day as an instant in a caller-supplied location. Everything else routes
// through it. It is spelled here rather than allowlisted so that moving or
// renaming it fails this test loudly instead of quietly exempting nothing.
const calendarDayConstructionPoint = "internal/services/day_utils.go:StartOfCalendarDay"

// calendarDayBarrierAllowlist carries the sites this sweep flags on the current
// tree and the reason each is tolerated. Three categories are mixed here on
// purpose, and each entry says which it is:
//
//   - deliberate carve-out: correct as written, the sweep cannot tell it apart;
//   - known-remaining: a real member of the class, not yet fixed;
//   - false positive: the sweep is wrong about it and says how.
//
// The key is `<path>:<enclosing function>:<kind>` — stable when lines move, so
// an unrelated edit above a site does not turn the barrier red. The set must
// match the sweep EXACTLY: an entry that no longer matches anything fails too,
// so a fixed site cannot leave a stale exemption behind.
var calendarDayBarrierAllowlist = map[string]string{
	"internal/reminders/next_run.go:fireOnCalendarDay:calendar day built in a location":                                      "deliberate carve-out: a scheduling instant at a runtime hour, not a calendar day, and the two lines below it check the requested date survived and fall back to services.StartOfCalendarDay when it did not.",
	"internal/services/calendar_feed_service.go:CalendarFeedService.ResolveFeed:calendar day stepped from a location anchor": "deliberate carve-out: a step of whole YEARS producing the lower bound of the log fetch window, not a calendar date anyone reads. The one-sided normalization widens the window by a day and never narrows it, and FetchLogsForUser re-resolves both bounds through DayRange.",
}

// calendarDayBarrierBlindSpots is what this sweep provably cannot see. It is
// printed with every failure so a reader never mistakes a green run for a
// cleared tree.
var calendarDayBarrierBlindSpots = []string{
	"shape (b) is decided from local variables inside a single function: an anchor reaching a comparison as a parameter, a struct field, a map/slice element or another package's return value is unclassified and therefore never flagged",
	"a location expression is anything that is not the literal time.UTC, so a variable that happens to hold time.UTC at run time counts as a location",
	"a local whose anchor is not the FIRST result of a multi-value call, and a local ever assigned something unclassifiable or two different anchors, is deliberately left unknown — the sweep prefers missing a site to flagging a safe one",
	"only Before/After/Equal/Sub are read as ordering; a comparison expressed some other way (sorting by UnixNano, a switch on Sub's sign through a helper) is invisible",
	"shape (c) reads the same local anchors as shape (b): a step whose receiver is a parameter, a struct field (stats.LastPeriodStart) or a range variable is unclassified and therefore never flagged, so the stepping class is covered only where the anchor is built in the same function",
	"non-Go surfaces are out of scope: the JavaScript bundle and the templates are not parsed here",
}

// calendarDayBarrierFloors guard against a vacuous pass. Discovery walks the
// tree, so a broken walk, a renamed helper or a parser that silently matched
// nothing would otherwise sweep an empty set and report success.
const (
	calendarDayBarrierFileFloor        = 200
	calendarDayBarrierDateCallFloor    = 6
	calendarDayBarrierCompareCallFloor = 40
	calendarDayBarrierStepCallFloor    = 40
)

// calendarDayFinding is one flagged site.
type calendarDayFinding struct {
	key      string
	position string
	detail   string
}

// calendarDayBarrierScan is the result of one walk over the shipped source.
type calendarDayBarrierScan struct {
	findings     []calendarDayFinding
	files        int
	dateCalls    int
	compareCalls int
	stepCalls    int
}

// TestCalendarDayConstructionBarrier is the sweep. Every flagged site must
// either be fixed or carry a reason in calendarDayBarrierAllowlist.
func TestCalendarDayConstructionBarrier(t *testing.T) {
	t.Parallel()

	scan := scanCalendarDayShapes(t)

	if scan.files < calendarDayBarrierFileFloor {
		t.Fatalf("the sweep parsed %d shipped Go file(s), fewer than the floor of %d — discovery is broken, so a green verdict here would mean nothing", scan.files, calendarDayBarrierFileFloor)
	}
	if scan.dateCalls < calendarDayBarrierDateCallFloor {
		t.Fatalf("the sweep saw %d time.Date call site(s), fewer than the floor of %d — it is no longer reading the construction shape it exists to read", scan.dateCalls, calendarDayBarrierDateCallFloor)
	}
	if scan.compareCalls < calendarDayBarrierCompareCallFloor {
		t.Fatalf("the sweep saw %d time ordering call(s), fewer than the floor of %d — it is no longer reading the comparison shape it exists to read", scan.compareCalls, calendarDayBarrierCompareCallFloor)
	}
	if scan.stepCalls < calendarDayBarrierStepCallFloor {
		t.Fatalf("the sweep saw %d calendar-day step(s), fewer than the floor of %d — it is no longer reading the stepping shape it exists to read", scan.stepCalls, calendarDayBarrierStepCallFloor)
	}

	unexplained := make([]calendarDayFinding, 0, len(scan.findings))
	matched := map[string]bool{}
	sanctioned := 0
	for _, finding := range scan.findings {
		if strings.HasPrefix(finding.key, calendarDayConstructionPoint+":") {
			sanctioned++
			continue
		}
		if _, allowed := calendarDayBarrierAllowlist[finding.key]; allowed {
			matched[finding.key] = true
			continue
		}
		unexplained = append(unexplained, finding)
	}

	if sanctioned == 0 {
		t.Fatalf("nothing was found at %s — the single sanctioned construction point moved or was renamed, and this sweep is now exempting a function that no longer exists", calendarDayConstructionPoint)
	}

	if len(unexplained) > 0 {
		t.Errorf("%d calendar-day site(s) build or compare a calendar day as an instant:\n%s\n%s", len(unexplained), formatCalendarDayFindings(unexplained), calendarDayBarrierGuidance())
	}

	stale := make([]string, 0, len(calendarDayBarrierAllowlist))
	for key := range calendarDayBarrierAllowlist {
		if !matched[key] {
			stale = append(stale, key)
		}
	}
	if len(stale) > 0 {
		sort.Strings(stale)
		t.Errorf("%d allowlist entr(y/ies) match nothing in the tree:\n  %s\nA site that was fixed, moved or renamed must not leave its exemption behind — the allowlist is the repository's list of what is still to do, and a stale line makes it read shorter than it is.", len(stale), strings.Join(stale, "\n  "))
	}
}

// calendarDayBarrierGuidance is the "what do I do now" half of a failure.
func calendarDayBarrierGuidance() string {
	var builder strings.Builder
	builder.WriteString("How to clear a finding:\n")
	builder.WriteString("  - building a calendar day: take the calendar components and call services.StartOfCalendarDay, or its wrappers CalendarDay (a date-only stored value), DateAtLocation (a real instant) or ParseDayDate (a YYYY-MM-DD input). Never time.Date/time.ParseInLocation with a non-UTC location: where a zone's midnight does not exist, both normalize the value into the previous calendar day.\n")
	builder.WriteString("  - comparing calendar days: use CalendarDaysBetween, which re-anchors both operands to UTC midnight of their own calendar day. A location midnight and a UTC midnight name the same day and are different instants, so Before/After/Equal between them is wrong every day in every non-UTC zone.\n")
	builder.WriteString("  - stepping calendar days: step from a UTC anchor — dateOnly(day).AddDate(0, 0, n), or forEachCalendarDay for a run of days. AddDate re-enters time.Date in the receiver's own location, so a step landing on a date whose local midnight the zone skips returns the previous calendar day at 23:00 instead.\n")
	builder.WriteString("  - the site is genuinely an INSTANT (a TTL, an expiry, a freshness window, a half-open range bound): say so in calendarDayBarrierAllowlist. Adding a line there is a deliberate act and owes a one-line reason, not a bare path.\n")
	builder.WriteString("What this sweep cannot see, so a green run is not a cleared tree:\n")
	for _, blindSpot := range calendarDayBarrierBlindSpots {
		builder.WriteString("  - " + blindSpot + "\n")
	}
	return builder.String()
}

func formatCalendarDayFindings(findings []calendarDayFinding) string {
	lines := make([]string, 0, len(findings))
	for _, finding := range findings {
		lines = append(lines, fmt.Sprintf("  %s\n    key: %s\n    %s", finding.position, finding.key, finding.detail))
	}
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// scanCalendarDayShapes parses every shipped (non-test) Go file in the module
// and reports the sites matching either shape.
func scanCalendarDayShapes(t *testing.T) calendarDayBarrierScan {
	t.Helper()

	root := calendarDayBarrierRepoRoot(t)
	fileSet := token.NewFileSet()
	scan := calendarDayBarrierScan{}

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
		inspectCalendarDayFile(fileSet, parsed, filepath.ToSlash(relative), &scan)
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk %s: %v", root, walkErr)
	}
	return scan
}

// calendarDayBarrierRepoRoot climbs to the directory holding go.mod, so the
// sweep covers the whole module rather than the package it happens to live in.
func calendarDayBarrierRepoRoot(t *testing.T) string {
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
			t.Fatalf("no go.mod above the working directory — the sweep cannot find the module root")
		}
		dir = parent
	}
}

// inspectCalendarDayFile records both shapes for one file. Shape (a) is read
// per call site; shape (b) is read per function, because its anchor tracking is
// scoped to a function's own local variables.
func inspectCalendarDayFile(fileSet *token.FileSet, file *ast.File, relative string, scan *calendarDayBarrierScan) {
	functions := calendarDayFunctionRanges(file)
	position := func(node ast.Node) (string, string) {
		point := fileSet.Position(node.Pos())
		return fmt.Sprintf("%s:%d", relative, point.Line), calendarDayEnclosingFunction(functions, node.Pos())
	}

	// Shape (a): construction as an instant in a location.
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch calendarDayCalleeName(call.Fun) {
		case "time.Date":
			scan.dateCalls++
			if len(call.Args) == 8 && !calendarDayIsUTCLocation(call.Args[7]) {
				where, function := position(call)
				scan.findings = append(scan.findings, calendarDayFinding{
					key:      relative + ":" + function + ":calendar day built in a location",
					position: where,
					detail:   "time.Date resolves a wall clock in a location; where that wall clock does not exist it normalizes into the previous calendar day.",
				})
			}
		case "time.ParseInLocation":
			where, function := position(call)
			scan.findings = append(scan.findings, calendarDayFinding{
				key:      relative + ":" + function + ":calendar day parsed in a location",
				position: where,
				detail:   "time.ParseInLocation resolves a nonexistent local midnight exactly as time.Date does; the single parse entry point is ParseDayDate, which parses in UTC and then resolves the day.",
			})
		}
		return true
	})

	// Shape (b): a raw ordering between two different midnight anchors.
	ast.Inspect(file, func(node ast.Node) bool {
		function, ok := node.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			return true
		}
		anchors := calendarDayLocalAnchors(function)
		ast.Inspect(function.Body, func(inner ast.Node) bool {
			call, isCall := inner.(*ast.CallExpr)
			if !isCall || len(call.Args) != 1 {
				return true
			}
			selector, isSelector := call.Fun.(*ast.SelectorExpr)
			if !isSelector {
				return true
			}
			switch selector.Sel.Name {
			case "Before", "After", "Equal", "Sub":
			default:
				return true
			}
			scan.compareCalls++

			left := calendarDayClassify(selector.X, anchors)
			right := calendarDayClassify(call.Args[0], anchors)
			if left == anchorUnknown || right == anchorUnknown || left == right {
				return true
			}
			where, enclosing := position(call)
			scan.findings = append(scan.findings, calendarDayFinding{
				key:      relative + ":" + enclosing + ":calendar days compared across midnight anchors",
				position: where,
				detail:   fmt.Sprintf("%s between a %s-anchored value and a %s-anchored one: the two name the same calendar day and are different instants.", selector.Sel.Name, calendarDayAnchorName(left), calendarDayAnchorName(right)),
			})
			return true
		})

		// Shape (c): a calendar-day STEP taken from a location anchor. Shares
		// this function's anchor map with shape (b) for the same reason — the
		// receiver's anchor is only knowable from the function's own locals.
		ast.Inspect(function.Body, func(inner ast.Node) bool {
			call, isCall := inner.(*ast.CallExpr)
			if !isCall || len(call.Args) != 3 {
				return true
			}
			selector, isSelector := call.Fun.(*ast.SelectorExpr)
			if !isSelector || selector.Sel.Name != "AddDate" || calendarDayIsPackageIdent(selector.X) {
				return true
			}
			scan.stepCalls++

			if calendarDayClassify(selector.X, anchors) != anchorLocation {
				return true
			}
			where, enclosing := position(call)
			scan.findings = append(scan.findings, calendarDayFinding{
				key:      relative + ":" + enclosing + ":calendar day stepped from a location anchor",
				position: where,
				detail:   "AddDate re-enters time.Date in the receiver's location: where the resulting day's local midnight does not exist (America/Santiago 2026-09-06, America/Havana 2026-03-08) it normalizes BACKWARD, so the step names the previous calendar day.",
			})
			return true
		})

		// The floor counts converted steps as well. Counting only AddDate would
		// tie the anti-vacuity anchor to the very thing the sweep judges: every
		// site fixed lowers it, so a fully converted tree would fail as "discovery
		// is broken" — which is exactly what happened once this class was swept.
		// A guard's floor must not depend on the data it judges
		// (`.claude/rules/testing.md`).
		ast.Inspect(function.Body, func(inner ast.Node) bool {
			call, isCall := inner.(*ast.CallExpr)
			if !isCall || len(call.Args) != 3 {
				return true
			}
			if strings.TrimPrefix(calendarDayCalleeName(call.Fun), "services.") == "AddCalendarDays" {
				scan.stepCalls++
			}
			return true
		})
		return true
	})
}

func calendarDayAnchorName(class anchorClass) string {
	if class == anchorUTC {
		return "UTC-midnight"
	}
	return "location-midnight"
}

// calendarDayFunctionRange pairs a declared function with the source span it
// covers, so a finding can name the function that owns it.
type calendarDayFunctionRange struct {
	name  string
	start token.Pos
	end   token.Pos
}

func calendarDayFunctionRanges(file *ast.File) []calendarDayFunctionRange {
	ranges := make([]calendarDayFunctionRange, 0, len(file.Decls))
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok {
			continue
		}
		name := function.Name.Name
		if function.Recv != nil && len(function.Recv.List) == 1 {
			name = calendarDayReceiverName(function.Recv.List[0].Type) + "." + name
		}
		ranges = append(ranges, calendarDayFunctionRange{name: name, start: function.Pos(), end: function.End()})
	}
	return ranges
}

func calendarDayReceiverName(expr ast.Expr) string {
	switch node := expr.(type) {
	case *ast.StarExpr:
		return calendarDayReceiverName(node.X)
	case *ast.Ident:
		return node.Name
	case *ast.IndexExpr:
		return calendarDayReceiverName(node.X)
	default:
		return "?"
	}
}

func calendarDayEnclosingFunction(ranges []calendarDayFunctionRange, pos token.Pos) string {
	for _, candidate := range ranges {
		if pos >= candidate.start && pos < candidate.end {
			return candidate.name
		}
	}
	return "<file scope>"
}

// calendarDayLocalAnchors classifies the function's local variables by the
// midnight anchor they carry. It runs to a fixed point so a chain
// (`start := CalendarDay(...); end := start.AddDate(...)`) resolves, then bans
// every name that was ever assigned something it could not classify or that
// carried two different anchors — both are conservative, and a banned name is
// simply never half of a flagged pair.
func calendarDayLocalAnchors(function *ast.FuncDecl) map[string]anchorClass {
	assignments := calendarDayAssignments(function)
	classes := map[string]anchorClass{}
	banned := map[string]bool{}

	for range 4 {
		changed := false
		for _, assignment := range assignments {
			if banned[assignment.name] {
				continue
			}
			class := calendarDayClassify(assignment.value, classes)
			if class == anchorUnknown {
				continue
			}
			existing, known := classes[assignment.name]
			switch {
			case !known:
				classes[assignment.name] = class
				changed = true
			case existing != class:
				delete(classes, assignment.name)
				banned[assignment.name] = true
				changed = true
			}
		}
		if !changed {
			break
		}
	}

	for _, assignment := range assignments {
		if calendarDayClassify(assignment.value, classes) == anchorUnknown {
			delete(classes, assignment.name)
		}
	}
	return classes
}

// calendarDayAssignment is one `name = value` or `name := value` inside a
// function body. A multi-value assignment contributes its targets with a nil
// value, which classifies as unknown and therefore bans them.
type calendarDayAssignment struct {
	name  string
	value ast.Expr
}

func calendarDayAssignments(function *ast.FuncDecl) []calendarDayAssignment {
	var assignments []calendarDayAssignment
	record := func(target ast.Expr, value ast.Expr) {
		identifier, ok := target.(*ast.Ident)
		if !ok || identifier.Name == "_" {
			return
		}
		assignments = append(assignments, calendarDayAssignment{name: identifier.Name, value: value})
	}

	ast.Inspect(function.Body, func(node ast.Node) bool {
		switch statement := node.(type) {
		case *ast.AssignStmt:
			if len(statement.Lhs) == len(statement.Rhs) {
				for index, target := range statement.Lhs {
					record(target, statement.Rhs[index])
				}
				return true
			}
			// `value, err := ParseDayDate(...)` — every construction helper this
			// sweep knows returns the time.Time first, so the first target keeps
			// the call's anchor and the rest are unknown.
			for index, target := range statement.Lhs {
				if index == 0 && len(statement.Rhs) == 1 {
					if _, isCall := statement.Rhs[0].(*ast.CallExpr); isCall {
						record(target, statement.Rhs[0])
						continue
					}
				}
				record(target, nil)
			}
		case *ast.ValueSpec:
			for index, name := range statement.Names {
				if len(statement.Values) == len(statement.Names) {
					record(name, statement.Values[index])
					continue
				}
				record(name, nil)
			}
		case *ast.RangeStmt:
			record(statement.Key, nil)
			record(statement.Value, nil)
		}
		return true
	})
	return assignments
}

// calendarDayClassify reports the midnight anchor an expression carries, or
// anchorUnknown when it cannot tell — which is the common case by design.
func calendarDayClassify(expr ast.Expr, anchors map[string]anchorClass) anchorClass {
	switch node := expr.(type) {
	case nil:
		return anchorUnknown
	case *ast.ParenExpr:
		return calendarDayClassify(node.X, anchors)
	case *ast.Ident:
		return anchors[node.Name]
	case *ast.CallExpr:
		if selector, ok := node.Fun.(*ast.SelectorExpr); ok && !calendarDayIsPackageIdent(selector.X) {
			switch selector.Sel.Name {
			case "AddDate":
				// The anchor is carried through — but only because a step taken
				// from a location anchor is itself shape (c) and flagged at its
				// own call site. AddDate re-enters time.Date in the receiver's
				// location, so on a date whose local midnight the zone skips it
				// does NOT keep the anchor: it returns the previous calendar day
				// at 23:00. Reading the result as "same anchor, later day" is
				// exactly how the class spread, and this line is only sound
				// standing next to that finding.
				return calendarDayClassify(selector.X, anchors)
			case "UTC":
				return anchorUTC
			}
		}
		return calendarDayClassifyCall(node)
	}
	return anchorUnknown
}

// calendarDayLocationArgument maps each construction helper to the index of its
// *time.Location argument. dateOnly takes none: it is UTC by definition.
var calendarDayLocationArgument = map[string]int{
	"AddCalendarDays":      2,
	"CalendarDay":          1,
	"DateAtLocation":       1,
	"StartOfCalendarDay":   3,
	"ParseDayDate":         1,
	"time.Date":            7,
	"time.ParseInLocation": 2,
}

func calendarDayClassifyCall(call *ast.CallExpr) anchorClass {
	name := strings.TrimPrefix(calendarDayCalleeName(call.Fun), "services.")
	if name == "dateOnly" {
		return anchorUTC
	}
	index, known := calendarDayLocationArgument[name]
	if !known || index >= len(call.Args) {
		return anchorUnknown
	}
	if calendarDayIsUTCLocation(call.Args[index]) {
		return anchorUTC
	}
	return anchorLocation
}

// calendarDayCalleeName renders a callee as `name` or `pkg.Name`, and "" for
// anything else (a method on a value, a func literal, an index expression).
func calendarDayCalleeName(expr ast.Expr) string {
	switch node := expr.(type) {
	case *ast.Ident:
		return node.Name
	case *ast.SelectorExpr:
		qualifier, ok := node.X.(*ast.Ident)
		if !ok {
			return ""
		}
		return qualifier.Name + "." + node.Sel.Name
	default:
		return ""
	}
}

// calendarDayIsUTCLocation reports the literal time.UTC and nothing else. A
// variable that holds time.UTC at run time reads as a location here — stated as
// a blind spot rather than guessed at.
func calendarDayIsUTCLocation(expr ast.Expr) bool {
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	qualifier, isIdent := selector.X.(*ast.Ident)
	return isIdent && qualifier.Name == "time" && selector.Sel.Name == "UTC"
}

// calendarDayIsPackageIdent distinguishes `time.Date(...)` from `value.AddDate(...)`
// well enough for this sweep: only the packages whose helpers it reads matter.
func calendarDayIsPackageIdent(expr ast.Expr) bool {
	identifier, ok := expr.(*ast.Ident)
	if !ok {
		return false
	}
	switch identifier.Name {
	case "time", "services":
		return true
	default:
		return false
	}
}

// calendarDayBarrierShapeCFixture is the sweep's own positive-and-negative pair
// for the stepping shape, parsed from source this test owns rather than found in
// the tree.
//
// Without it the shape's only live positive example is an allowlist entry, so the
// day that site is converted the classifier stops being exercised and the barrier
// passes while measuring nothing — the anti-vacuity anchor would depend on the
// data it judges, which `.claude/rules/testing.md` forbids. The floors do not
// cover this: they prove the parser SEES steps, not that the classifier still
// tells a location anchor from a UTC one.
const calendarDayBarrierShapeCFixture = `package sample

import "time"

func steppedFromALocationAnchor(value time.Time, location *time.Location) time.Time {
	anchor := CalendarDay(value, location)
	return anchor.AddDate(0, 0, 5)
}

func steppedFromAUTCAnchor(value time.Time) time.Time {
	anchor := dateOnly(value)
	return anchor.AddDate(0, 0, 5)
}

func steppedFromAnExplicitUTCConstruction(value time.Time) time.Time {
	anchor := CalendarDay(value, time.UTC)
	return anchor.AddDate(0, 0, 5)
}
`

func TestCalendarDayBarrierClassifiesTheSteppingShape(t *testing.T) {
	t.Parallel()

	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, "sample.go", calendarDayBarrierShapeCFixture, 0)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}

	scan := calendarDayBarrierScan{}
	inspectCalendarDayFile(fileSet, parsed, "sample.go", &scan)

	stepping := make(map[string]bool, len(scan.findings))
	for _, finding := range scan.findings {
		if strings.HasSuffix(finding.key, ":calendar day stepped from a location anchor") {
			stepping[finding.key] = true
		}
	}

	wantFlagged := "sample.go:steppedFromALocationAnchor:calendar day stepped from a location anchor"
	if !stepping[wantFlagged] {
		t.Errorf("the sweep no longer flags a step taken from a location anchor; stepping findings = %v", stepping)
	}
	for _, mustNotFlag := range []string{"steppedFromAUTCAnchor", "steppedFromAnExplicitUTCConstruction"} {
		key := "sample.go:" + mustNotFlag + ":calendar day stepped from a location anchor"
		if stepping[key] {
			t.Errorf("%s steps from a UTC anchor and must not be flagged", mustNotFlag)
		}
	}
	if scan.stepCalls < 3 {
		t.Errorf("the fixture holds three steps; the sweep counted %d, so it is not reading them", scan.stepCalls)
	}
}
