package i18n_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/i18n"
)

// A catalogue value handed to fmt.Sprintf is a contract between six JSON files
// and a Go argument list, and until this barrier nothing in the build read both
// ends of it. Key parity compares key SETS and never looks at a value; the
// template barrier reads values only for emptiness; `go vet`'s printf checker
// stops at the first non-constant format string, which is every one of these
// sites by construction. So a translator who writes %s where the code passes an
// int ships `%!s(int=12)` into the Insights summary, and one verb too few ships
// `%!(EXTRA string=days)` — on the page whose whole purpose is to state what
// the data supports.
//
// The barrier checks the verb SEQUENCE, not the verb count. A count-only check
// is satisfied by a locale that swapped %d and %s while keeping six of them,
// which is the drift that renders wrong rather than the drift that renders
// short. It checks all SIX catalogues, because a rule enforced in the reference
// file alone is a rule enforced in one language out of six. And it checks the
// English fallback literal beside each call site, because that literal is a
// second copy of the same format: the catalogue and the fallback have to stay
// in step with each other AND with the argument list, and only one of the three
// is visible from any single file.
//
// It does not duplicate TestPluralVariantPatternsKeepPrintfVerbs (plural_test.go),
// and that test is not a smaller version of this one. That barrier compares the
// number of "%d" occurrences among the CLDR variants of one plural family
// against each other, and never against a Go call site; it is count-based,
// "%d"-only, and has no opinion about a key no plural family contains. Every
// key below is outside its reach.
//
// What it does NOT see, stated so no reader concludes the broader claim: it is
// syntactic, so it cannot tell which of several literals a branch actually
// reaches at run time, and it cannot read the Go TYPE of an argument expression
// without a type checker. The type column below is declared, not inferred — but
// it is not free-floating: the argument COUNT at each site is read from the
// AST, the verb sequence is checked against the declared types by
// verbAcceptsGoType, and every catalogue value and every fallback is actually
// run through fmt.Sprintf with arguments built from that column, so a declared
// type that disagrees with its verb cannot stay green.
//
// The key reader follows a variable ONE hop — a local assignment, or a package
// constant — and no further. A key that reaches its lookup through anything
// deeper leaves the site with no key of its own, and a site with no key must
// declare a keyOrigin naming where its key is written. So the failure mode of
// the reader's shallowness is a table entry, not a silent gap.

// localizedFormatContract is one `fmt.Sprintf` site whose pattern comes from
// the locale catalogue.
//
// Sites are matched to entries by (file, function), and within that bucket by
// the KEY each site names — not by table order. Order pairing was the first
// draft and it is a trap: swapping two entries mispairs them, and where their
// argument counts happen to agree it fails silently and the arity check then
// verifies nothing. A bucket holding one site needs no matching at all.
//
// A sixth summary added to a function that already holds five fails until it is
// declared here. That is the point of the table: the recurring cost this
// barrier exists against is the NEXT summary string, not the ones below.
type localizedFormatContract struct {
	// file and function locate the fmt.Sprintf call. line is deliberately
	// absent: it drifts on every edit above it, and the pairing rule does not
	// need it.
	file     string
	function string

	// keys are the catalogue keys whose value can reach this call. More than
	// one entry means the same format is shared by several keys, and every one
	// of them is checked in every catalogue.
	keys []string

	// verbs is the ordered format-verb sequence the argument list requires,
	// written exactly as it must appear — "%.2f" is not "%f", because the
	// second renders a basal temperature as 36.500000.
	verbs []string

	// argumentTypes names the Go type of each argument passed at the site, in
	// order. See the file comment for what this column is and is not.
	argumentTypes []string

	// fallbacks are the English literals the code substitutes when the
	// catalogue does not answer. Empty when the site has none.
	fallbacks []string

	// keyOrigin names where the key literals are WRITTEN, for a site that does
	// not name them itself — a key that arrives as a parameter, or three calls
	// away in another layer. Nil means the site's own defining lookup carries
	// them, which the reader resolves directly.
	//
	// This field is the whole answer to a repointing: change the key at the
	// place named here and the set collected there no longer equals the set
	// declared below. Without it, a site whose function holds no key literal is
	// a site whose key nothing compares — which is what six of these eleven
	// keys were.
	keyOrigin *keyOrigin

	// fallbackArrivesIndirectly marks a site whose fallback is a parameter
	// rather than a literal assigned in the same function. Its literals are
	// then looked for in the shipped Go sources at large rather than in this
	// function's body.
	fallbackArrivesIndirectly bool

	// note names the argument expressions, so a reader can check the type
	// column against the call without opening the file.
	note string
}

// keyOrigin locates the call argument that carries a catalogue key for a site
// that does not name it itself: "the string literal at argument `argument` of
// every call to `callee`", optionally narrowed to the calls inside one
// function.
//
// One shape covers both cases on this tree. A helper reached with the key as an
// argument (formatBBTLocalizedMessage) needs no `within`, because every call to
// it is a call this contract is about. A key looked up in one layer and
// formatted in another (stats.cycle_label) narrows to the function that does
// the lookup, since `lookupMessage` itself is called twenty times in
// internal/api and only one of those calls feeds this site.
type keyOrigin struct {
	callee   string
	argument int
	within   string // "file#function", empty for every call to callee
}

func (origin keyOrigin) String() string {
	where := "anywhere in shipped Go"
	if origin.within != "" {
		where = "inside " + origin.within
	}
	return fmt.Sprintf("argument %d of %s(...), %s", origin.argument+1, origin.callee, where)
}

// localizedFormatContracts is the declared set. Measured 2026-08-24: nine
// dynamic-pattern fmt.Sprintf sites in shipped Go, carrying eleven catalogue
// keys across six languages.
//
// Two of the nine sit outside internal/api, which is where the class was first
// recorded. They are the same defect: `stats.cycle_label` is looked up in the
// transport layer and formatted three calls deep in internal/services, and the
// webhook reminder body is looked up and formatted entirely inside
// internal/services. A class guarded at seven of nine sites is a new defect,
// not a partial fix, so all nine are declared here.
var localizedFormatContracts = []localizedFormatContract{
	{
		file:          "internal/api/stats_page_helpers.go",
		function:      "buildStatsCycleChartSummary",
		keys:          []string{"stats.cycle_chart_summary"},
		verbs:         []string{"%d", "%d", "%s", "%d", "%s", "%d", "%d", "%s"},
		argumentTypes: []string{"int", "int", "string", "int", "string", "int", "int", "string"},
		fallbacks:     []string{"%d completed cycles shown. Latest cycle %d %s. Average %d %s. Range %d to %d %s."},
		note:          "len(ChartData.Values), latestCycleLength, daysShort, ChartBaseline, daysShort, minCycleLength, maxCycleLength, daysShort",
	},
	{
		file:          "internal/api/stats_page_helpers.go",
		function:      "buildStatsCycleChartSummary",
		keys:          []string{"stats.cycle_chart_summary_no_baseline"},
		verbs:         []string{"%d", "%d", "%s", "%d", "%d", "%s"},
		argumentTypes: []string{"int", "int", "string", "int", "int", "string"},
		fallbacks:     []string{"%d completed cycles shown. Latest cycle %d %s. Range %d to %d %s."},
		note:          "len(ChartData.Values), latestCycleLength, daysShort, minCycleLength, maxCycleLength, daysShort",
	},
	{
		file:          "internal/api/stats_page_helpers.go",
		function:      "buildStatsBBTChartSummary",
		keys:          []string{"stats.bbt_chart_summary_no_shift"},
		verbs:         []string{"%d"},
		argumentTypes: []string{"int"},
		fallbacks:     []string{"%d readings this cycle. No temperature shift detected yet."},
		note:          "readingsCount",
	},
	{
		file:          "internal/api/stats_page_helpers.go",
		function:      "buildStatsBBTChartSummary",
		keys:          []string{"stats.bbt_chart_summary_with_marker"},
		verbs:         []string{"%d", "%.2f", "%s", "%s"},
		argumentTypes: []string{"int", "float64", "string", "string"},
		fallbacks:     []string{"%d readings this cycle. Coverline %.2f %s. Marker: %s."},
		note:          "readingsCount, chart.Baseline (float64), unit, translateMessage(chart.MarkerLabelKey)",
	},
	{
		file:          "internal/api/stats_page_helpers.go",
		function:      "buildStatsBBTChartSummary",
		keys:          []string{"stats.bbt_chart_summary"},
		verbs:         []string{"%d", "%.2f", "%s"},
		argumentTypes: []string{"int", "float64", "string"},
		fallbacks:     []string{"%d readings this cycle. Coverline %.2f %s."},
		note:          "readingsCount, chart.Baseline (float64), unit",
	},
	{
		// One Sprintf, two keys: buildBBTFieldViewData reaches this helper
		// twice, with the hint key and the error key, and passes each one's
		// English fallback as an argument. Both are checked in all six
		// catalogues, and both fallback literals are checked as literals.
		//
		// The key origin is the argument position at those two calls, so
		// repointing either one at another catalogue key fails here — the case
		// that was invisible until the review.
		file:                      "internal/api/page_view_data_helpers.go",
		function:                  "formatBBTLocalizedMessage",
		keys:                      []string{"dashboard.bbt_range_hint", "dashboard.bbt_range_error"},
		verbs:                     []string{"%s", "%s", "%s"},
		argumentTypes:             []string{"string", "string", "string"},
		fallbacks:                 []string{"Allowed range: %s-%s %s.", "Enter a value between %s and %s %s."},
		keyOrigin:                 &keyOrigin{callee: "formatBBTLocalizedMessage", argument: 1},
		fallbackArrivesIndirectly: true,
		note:                      "minText, maxText, symbol — all pre-rendered strings, never the float",
	},
	{
		// The key is chosen by a branch into patternKey, so the lookup call
		// carries a variable rather than a literal — the reader resolves it
		// through the local assignment, which is why no key origin is needed.
		// The function also assigns a second, verb-free literal ("Saved.") to
		// the same pattern variable; a literal with no verb cannot misformat,
		// which is why the rule below tolerates one.
		file:          "internal/api/handlers_days_status_helpers.go",
		function:      "sendDaySaveStatus",
		keys:          []string{"common.saved_at"},
		verbs:         []string{"%s"},
		argumentTypes: []string{"string"},
		fallbacks:     []string{"Saved at %s"},
		note:          "timestamp, already formatted as 15:04",
	},
	{
		// Cross-layer: internal/api/stats_page_helpers.go looks the key up and
		// hands the pattern to the stats service, which formats it three calls
		// later. The transport side sees a catalogue value it never formats;
		// this side formats a value it never looked up. Neither end alone can
		// state the contract, which is why it is stated here. The pattern
		// reaches this function as a parameter, so the key origin points back
		// at the transport-side lookup that chose it.
		file:          "internal/services/stats_view_policy.go",
		function:      "BuildCycleTrendLabels",
		keys:          []string{"stats.cycle_label"},
		verbs:         []string{"%d"},
		argumentTypes: []string{"int"},
		fallbacks:     []string{"Cycle %d"},
		keyOrigin:     &keyOrigin{callee: "lookupMessage", argument: 1, within: "internal/api/stats_page_helpers.go#buildStatsPageData"},
		note:          "index+1, the 1-based chart point number",
	},
	{
		// The reminder body leaves the instance in a webhook payload rather
		// than on a page, so a formatting error here is read by whatever the
		// owner pointed the webhook at. There is no fallback literal: an
		// unanswered key sends the headline alone rather than verb residue.
		// Both keys reach the lookup as package constants; the reader resolves
		// those, so no key origin is needed.
		file:          "internal/services/webhook_notify_service.go",
		function:      "reminderCopy",
		keys:          []string{"webhook.reminder.period.message", "webhook.reminder.ovulation.message"},
		verbs:         []string{"%s"},
		argumentTypes: []string{"string"},
		note:          "reminder.EventDate formatted as 2006-01-02",
	},
}

// goFormatSite is one `fmt.Sprintf` whose format string is computed rather than
// written as a literal. Those are exactly the calls `go vet` cannot check.
type goFormatSite struct {
	file      string
	line      int
	function  string
	pattern   string // the identifier in the format position, "" for any other expression
	arguments int

	// keys are the catalogue keys the site's OWN defining lookup names,
	// resolved through local assignments and package constants. Empty when the
	// pattern arrives from somewhere this reader cannot follow — those sites
	// declare a keyOrigin instead.
	keys []string
}

func (site goFormatSite) String() string {
	return fmt.Sprintf("%s:%d in %s (fmt.Sprintf with %d argument(s))", site.file, site.line, site.function, site.arguments)
}

// goFormatBody is the surrounding evidence a site needs: the literals its
// function assigns to the pattern variable.
type goFormatBody struct {
	patternLiterals []string
}

// goKeyOriginLiteral is one string literal found at a call argument some
// contract's keyOrigin points at.
type goKeyOriginLiteral struct {
	callee   string
	argument int
	within   string // "file#function" of the enclosing declaration
	value    string
}

func goFormatBodyKey(file string, function string) string {
	return file + "#" + function
}

// keyOriginCallees is derived from the contract table, so the sweep reads only
// the calls some contract actually asks about. Deriving it rather than listing
// it is what keeps a new keyOrigin from pointing at a callee nothing collects —
// an entry that would then compare its declared keys against an empty set and
// pass.
var keyOriginCallees = func() map[string]bool {
	callees := map[string]bool{}
	for _, contract := range localizedFormatContracts {
		if contract.keyOrigin != nil {
			callees[contract.keyOrigin.callee] = true
		}
	}
	return callees
}()

// recordGoFormatSites is called for every parsed Go file the shared collector
// walks (collectFromGoFile in locale_reachability_test.go).
func recordGoFormatSites(origin goOrigin, file *ast.File, evidence *sourceEvidence) {
	constants := fileStringConstants(file)

	for _, declaration := range file.Decls {
		function := ""
		if typed, ok := declaration.(*ast.FuncDecl); ok {
			function = typed.Name.Name
			// Two declarations of one name in one file merge into a single
			// bucket below, and every finding after that names a function
			// without saying which. Counted here, refused in the barrier.
			evidence.goFunctionDeclarations[goFormatBodyKey(origin.file, function)]++
		}

		evidence.goKeyOriginLiterals = append(evidence.goKeyOriginLiterals,
			readKeyOriginLiterals(origin, function, declaration, constants)...)

		sites, body := readGoFormatDeclaration(origin, function, declaration, constants)
		if len(sites) == 0 {
			continue
		}
		evidence.goFormatSites = append(evidence.goFormatSites, sites...)

		key := goFormatBodyKey(origin.file, function)
		merged := evidence.goFormatBodies[key]
		merged.patternLiterals = append(merged.patternLiterals, body.patternLiterals...)
		evidence.goFormatBodies[key] = merged
	}
}

// readKeyOriginLiterals collects the string literals sitting at the argument
// positions of calls to a callee some contract's keyOrigin names.
//
// This is the half the first draft of this barrier did not have, and its
// absence was the review's finding: a site whose key is written at its CALLER
// had nothing comparing that key to the table, so repointing
// buildBBTFieldViewData at another catalogue key left the barrier green while
// the dashboard rendered %!d(string=35.0).
func readKeyOriginLiterals(origin goOrigin, function string, declaration ast.Decl, constants map[string]string) []goKeyOriginLiteral {
	if len(keyOriginCallees) == 0 {
		return nil
	}

	var found []goKeyOriginLiteral
	ast.Inspect(declaration, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		callee := calleeName(call)
		if callee == "" || !keyOriginCallees[callee] {
			return true
		}
		for index, argument := range call.Args {
			for _, value := range resolveStringValues(argument, nil, constants) {
				found = append(found, goKeyOriginLiteral{
					callee:   callee,
					argument: index,
					within:   goFormatBodyKey(origin.file, function),
					value:    value,
				})
			}
		}
		return true
	})
	return found
}

// readGoFormatDeclaration walks one declaration in two passes: first to find
// the sites and the identifiers they format through, then to collect only the
// literals assigned to THOSE identifiers. Collecting every string assignment in
// the function instead would sweep up unrelated copy and turn the fallback rule
// below into noise.
func readGoFormatDeclaration(origin goOrigin, function string, declaration ast.Decl, constants map[string]string) ([]goFormatSite, goFormatBody) {
	var sites []goFormatSite
	sitePositions := []token.Pos{}
	patternNames := map[string]bool{}

	ast.Inspect(declaration, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		argument, ok := sprintfFormatArgument(call)
		if !ok {
			return true
		}
		if _, literal := goStringLiteralOf(argument); literal {
			// A literal format string is what `go vet` already checks.
			return true
		}
		if call.Ellipsis.IsValid() {
			// `fmt.Sprintf(format, args...)` spreads a variadic argument list:
			// a formatting helper forwarding its own caller's arguments, not a
			// catalogue value with a fixed argument list. internal/testdb's
			// skip helper is the only one on this tree.
			return true
		}

		site := goFormatSite{
			file:      origin.file,
			line:      origin.lineOf(call.Lparen),
			function:  function,
			arguments: len(call.Args) - 1,
		}
		if identifier, ok := argument.(*ast.Ident); ok {
			site.pattern = identifier.Name
			patternNames[identifier.Name] = true
		}
		sites = append(sites, site)
		sitePositions = append(sitePositions, call.Lparen)
		return true
	})

	if len(sites) == 0 {
		return nil, goFormatBody{}
	}

	// Second pass: the local string values of every identifier, and the
	// literals assigned to a pattern identifier. Local values are collected for
	// ALL identifiers rather than only the pattern ones, because a key reaches
	// its lookup through a variable of its own (`patternKey`, `messageKey`).
	var body goFormatBody
	locals := map[string][]string{}
	var definitions []patternDefinition

	ast.Inspect(declaration, func(node ast.Node) bool {
		assignment, ok := node.(*ast.AssignStmt)
		if !ok {
			return true
		}
		for index, target := range assignment.Lhs {
			identifier, ok := target.(*ast.Ident)
			if !ok || index >= len(assignment.Rhs) {
				continue
			}
			right := assignment.Rhs[index]
			if text, ok := goStringLiteralOf(right); ok {
				locals[identifier.Name] = append(locals[identifier.Name], text)
				if patternNames[identifier.Name] {
					body.patternLiterals = append(body.patternLiterals, text)
				}
				continue
			}
			if nested, ok := right.(*ast.Ident); ok {
				locals[identifier.Name] = append(locals[identifier.Name], locals[nested.Name]...)
				if value, known := constants[nested.Name]; known {
					locals[identifier.Name] = append(locals[identifier.Name], value)
				}
				continue
			}
			// A call on the right of a pattern identifier is the lookup that
			// defines it. Which call defines which site is settled by position
			// below; a single-result form (`x := f(...)`) lands here too, since
			// Lhs and Rhs are then the same length.
			if call, ok := right.(*ast.CallExpr); ok && patternNames[identifier.Name] {
				definitions = append(definitions, patternDefinition{name: identifier.Name, position: call.Lparen, call: call})
			}
		}
		return true
	})
	// Multi-result assignments (`pattern, translated := lookupMessage(...)`)
	// carry one call for several names. The loop above pairs Lhs[i] with
	// Rhs[i], so it reaches such a call only for the FIRST name — which is the
	// pattern today, and would silently stop being it the day a helper returned
	// them the other way round. This pass binds the call to every pattern name
	// on the left, whatever its position; the overlap with the loop above is a
	// repeated identical definition, which definingCallFor collapses.
	ast.Inspect(declaration, func(node ast.Node) bool {
		assignment, ok := node.(*ast.AssignStmt)
		if !ok || len(assignment.Rhs) != 1 || len(assignment.Lhs) < 2 {
			return true
		}
		call, ok := assignment.Rhs[0].(*ast.CallExpr)
		if !ok {
			return true
		}
		for _, target := range assignment.Lhs {
			identifier, ok := target.(*ast.Ident)
			if !ok || !patternNames[identifier.Name] {
				continue
			}
			definitions = append(definitions, patternDefinition{name: identifier.Name, position: call.Lparen, call: call})
		}
		return true
	})

	for index := range sites {
		definition, found := definingCallFor(definitions, sites[index].pattern, sitePositions[index])
		if !found {
			continue
		}
		keys := map[string]bool{}
		for _, argument := range definition.call.Args {
			for _, value := range resolveStringValues(argument, locals, constants) {
				keys[value] = true
			}
		}
		for key := range keys {
			sites[index].keys = append(sites[index].keys, key)
		}
		sort.Strings(sites[index].keys)
	}

	return sites, body
}

// patternDefinition is one assignment of a call's result to a pattern
// identifier — the lookup that decides which catalogue key a site formats.
type patternDefinition struct {
	name     string
	position token.Pos
	call     *ast.CallExpr
}

// definingCallFor picks the last definition of `pattern` that precedes the
// site. Go scoping would answer this exactly; position is enough here and is
// stated as the approximation it is. The shape it must get right is the one
// buildStatsCycleChartSummary has: two lookups in sibling blocks, both writing
// a variable named `pattern`, each feeding the Sprintf that follows it.
func definingCallFor(definitions []patternDefinition, pattern string, site token.Pos) (patternDefinition, bool) {
	best := patternDefinition{}
	found := false
	for _, definition := range definitions {
		if definition.name != pattern || definition.position >= site {
			continue
		}
		if !found || definition.position > best.position {
			best = definition
			found = true
		}
	}
	return best, found
}

// resolveStringValues answers what string literals an expression can carry: the
// literal itself, the literals a local variable was assigned, or the value of a
// package constant. One hop, deliberately — this is a reader that must be
// obvious, not a dataflow engine. A key it cannot follow leaves the site with
// no keys, and a site with no keys must declare a keyOrigin, so the failure
// mode is a table entry rather than a silent gap.
func resolveStringValues(expression ast.Expr, locals map[string][]string, constants map[string]string) []string {
	if text, ok := goStringLiteralOf(expression); ok {
		return []string{text}
	}
	identifier, ok := expression.(*ast.Ident)
	if !ok {
		return nil
	}
	var values []string
	values = append(values, locals[identifier.Name]...)
	if value, known := constants[identifier.Name]; known {
		values = append(values, value)
	}
	return values
}

// fileStringConstants reads the file's package-level string constants and vars,
// which is how the webhook reminder keys reach their lookup.
func fileStringConstants(file *ast.File) map[string]string {
	constants := map[string]string{}
	for _, declaration := range file.Decls {
		generic, ok := declaration.(*ast.GenDecl)
		if !ok || (generic.Tok != token.CONST && generic.Tok != token.VAR) {
			continue
		}
		for _, specification := range generic.Specs {
			value, ok := specification.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for index, name := range value.Names {
				if index >= len(value.Values) {
					continue
				}
				if text, ok := goStringLiteralOf(value.Values[index]); ok {
					constants[name.Name] = text
				}
			}
		}
	}
	return constants
}

// calleeName is the simple name of a called function, whether it is called
// bare (`lookupMessage(...)`) or through a receiver or package
// (`service.localized.Message(...)`).
func calleeName(call *ast.CallExpr) string {
	switch typed := call.Fun.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.SelectorExpr:
		if typed.Sel != nil {
			return typed.Sel.Name
		}
	}
	return ""
}

func sprintfFormatArgument(call *ast.CallExpr) (ast.Expr, bool) {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel == nil || selector.Sel.Name != "Sprintf" {
		return nil, false
	}
	packageName, ok := selector.X.(*ast.Ident)
	if !ok || packageName.Name != "fmt" || len(call.Args) == 0 {
		return nil, false
	}
	return call.Args[0], true
}

// goStringLiteralOf unquotes an expression that is a plain string literal. It
// wraps the shared goStringLiteral, which takes the *ast.BasicLit the reachability
// sweep already has in hand, so the two readers cannot disagree about what a
// string literal is.
func goStringLiteralOf(expression ast.Expr) (string, bool) {
	literal, ok := expression.(*ast.BasicLit)
	if !ok {
		return "", false
	}
	return goStringLiteral(literal)
}

// TestEveryLocalizedFormatPatternAgreesWithItsArgumentList is the barrier.
func TestEveryLocalizedFormatPatternAgreesWithItsArgumentList(t *testing.T) {
	root := moduleRoot(t)

	evidence := newSourceEvidence()
	if err := collectFromGoTree(root, evidence); err != nil {
		t.Fatalf("sweeping the Go sources: %v", err)
	}
	if err := collectFromShippedTemplates(evidence); err != nil {
		t.Fatalf("sweeping the shipped templates: %v", err)
	}
	// The shared anti-vacuity refusal: every package under internal/ must have
	// contributed a parsed file, and the literal and file counts must be of the
	// right order. A sweep that skipped internal/services would find neither of
	// the two cross-layer sites and pass about seven.
	refuseAnUnderfedSweep(t, root, evidence)

	// A guard fails by silence. Measured 2026-08-24 on the tree this barrier
	// ships with: nine dynamic-pattern fmt.Sprintf sites in shipped Go, eleven
	// catalogue keys, six catalogues. These are exact rather than slack
	// counts because the table below is exact — what the floors refuse is the
	// case the table cannot see, a sweep that walked the tree, recognised no
	// call at all, and had nothing to disagree with.
	if len(evidence.goFormatSites) < 9 {
		t.Fatalf("found only %d dynamic-pattern fmt.Sprintf site(s) in shipped Go; the sweep recognised no call, so every agreement it reports below is an agreement about nothing", len(evidence.goFormatSites))
	}
	if len(localizedFormatContracts) < 9 {
		t.Fatalf("the contract table declares only %d site(s); it was emptied rather than corrected", len(localizedFormatContracts))
	}

	manager, err := i18n.NewManager(i18n.LangEN)
	if err != nil {
		t.Fatalf("building the locale manager: %v", err)
	}
	languages := manager.SupportedLanguages()
	if len(languages) < 6 {
		t.Fatalf("the manager reports %d language(s); this barrier claims to check all six catalogues and must not report success about fewer", len(languages))
	}

	assertTheTableCoversEveryFormatSite(t, evidence)
	assertEveryContractIsInternallyConsistent(t)
	assertEveryFallbackLiteralStillMatches(t, evidence)
	assertEveryCatalogueMatchesItsContract(t, manager, languages)
}

// assertTheTableCoversEveryFormatSite is the completeness half: an undeclared
// site fails, a declared site that no longer exists fails, and the argument
// count read from the AST must equal the declared verb count.
//
// Without this, the table is a list of the sites someone remembered, and the
// eighth summary string added next month is silently uncovered — which is the
// recurring cost this whole barrier exists against.
//
// It is also where each site's KEY is held to the table, at whichever end
// writes it: the site's own lookup when the reader can resolve it, and the
// contract's declared keyOrigin when it cannot. Repointing a call at another
// catalogue key is the defect that closes here, and it is the one the first
// draft of this barrier could not see for six of its eleven keys.
func assertTheTableCoversEveryFormatSite(t *testing.T, evidence *sourceEvidence) {
	t.Helper()

	declared := map[string][]localizedFormatContract{}
	var order []string
	for _, contract := range localizedFormatContracts {
		key := goFormatBodyKey(contract.file, contract.function)
		if len(declared[key]) == 0 {
			order = append(order, key)
		}
		declared[key] = append(declared[key], contract)
	}

	found := map[string][]goFormatSite{}
	for _, site := range evidence.goFormatSites {
		key := goFormatBodyKey(site.file, site.function)
		found[key] = append(found[key], site)
	}
	for key := range found {
		sites := found[key]
		sort.Slice(sites, func(a, b int) bool { return sites[a].line < sites[b].line })
	}

	for key, sites := range found {
		if len(declared[key]) > 0 {
			continue
		}
		var lines []string
		for _, site := range sites {
			lines = append(lines, "  "+site.String())
		}
		sort.Strings(lines)
		t.Errorf("undeclared localized format site(s):\n%s\n"+
			"A fmt.Sprintf whose pattern is computed is invisible to `go vet`'s printf checker, so nothing else in "+
			"the build reads its verbs. If the pattern comes from the locale catalogue, declare it in "+
			"localizedFormatContracts with its keys, its verb sequence and its argument types. If it does not — a "+
			"helper forwarding a variadic argument list, say — the recogniser above is what needs the exemption, "+
			"stated in one place with its reason.", strings.Join(lines, "\n"))
	}

	for _, key := range order {
		contracts := declared[key]
		sites := found[key]
		if len(sites) == 0 {
			t.Errorf("localizedFormatContracts declares %d site(s) at %s and the sweep found none; either the call moved and the entry is stale, or the recogniser stopped seeing it", len(contracts), key)
			continue
		}
		// One name declared twice in one file merges two functions into this
		// bucket, and every message below would name a function without saying
		// which of the two. reminderCopy is already a method, so the table
		// addresses methods by bare name; refusing the collision is cheaper and
		// more honest than carrying a receiver column that has nothing to say
		// on a tree where no name repeats.
		if count := evidence.goFunctionDeclarations[key]; count > 1 {
			t.Errorf("%s is declared %d times in that file, so this barrier cannot tell the two apart: their format sites, pattern literals and keys all merge into one bucket and every finding below would name a function without naming which. Rename one, or give the table a receiver column", key, count)
			continue
		}
		if len(sites) != len(contracts) {
			t.Errorf("%s holds %d localized fmt.Sprintf site(s) and the table declares %d; an added or removed call has to be declared before this barrier can say anything about it", key, len(sites), len(contracts))
			continue
		}

		for _, pair := range pairSitesToContracts(t, key, sites, contracts) {
			site, contract := pair.site, pair.contract
			named := strings.Join(contract.keys, "/")
			if site.arguments != len(contract.verbs) {
				t.Errorf("%s:%d (%s) passes %d argument(s) to a pattern declared with %d verb(s) (%s); one verb too few renders %%!(EXTRA ...) and one too many renders %%!(MISSING)",
					site.file, site.line, named, site.arguments, len(contract.verbs), strings.Join(contract.verbs, " "))
			}

			// The keys that can reach THIS site must be exactly the declared
			// ones. Repointing a call at another catalogue key is the defect
			// this closes, and it is checked at whichever end writes the key.
			want := append([]string(nil), contract.keys...)
			sort.Strings(want)
			got := site.keys
			where := site.String()
			if contract.keyOrigin != nil {
				got = keysAtOrigin(evidence, *contract.keyOrigin)
				where = contract.keyOrigin.String()
				if len(site.keys) > 0 {
					t.Errorf("%s:%d names the key(s) [%s] itself, yet the table sends this barrier to %s to find them; a site that carries its own key must not declare a key origin, or the two answers can disagree in silence",
						site.file, site.line, strings.Join(site.keys, ", "), where)
				}
			}
			if strings.Join(got, ",") == strings.Join(want, ",") {
				continue
			}
			t.Errorf("the key(s) reaching %s:%d are [%s] and the table declares [%s], read at %s:\n"+
				"A call repointed at another catalogue key formats that key's verbs against this site's argument list — "+
				"three strings against %%d %%.2f %%s renders %%!d(string=...) onto the page. Update the table with the "+
				"key that is actually formatted here, and check its verb sequence while you are in it.",
				site.file, site.line, strings.Join(got, ", "), strings.Join(want, ", "), where)
		}
	}
}

// sitePairing is one contract matched to the site it describes.
type sitePairing struct {
	site     goFormatSite
	contract localizedFormatContract
}

// pairSitesToContracts matches by the keys a site names rather than by table
// order. Order pairing was the first draft and it is a trap: swapping two table
// entries mispairs them, and where the two argument counts agree it fails
// silently and the arity check verifies nothing.
//
// A bucket holding a single site needs no matching. Beyond that the site's own
// resolved key set is the identity; a bucket whose sites carry no keys at all
// falls back to source order, which is sound only because such a bucket has one
// site — and that is asserted rather than assumed.
func pairSitesToContracts(t *testing.T, bucket string, sites []goFormatSite, contracts []localizedFormatContract) []sitePairing {
	t.Helper()

	if len(sites) == 1 {
		return []sitePairing{{site: sites[0], contract: contracts[0]}}
	}

	byKeys := map[string]localizedFormatContract{}
	for _, contract := range contracts {
		keys := append([]string(nil), contract.keys...)
		sort.Strings(keys)
		identity := strings.Join(keys, ",")
		if _, clash := byKeys[identity]; clash {
			t.Errorf("%s declares two entries for the same key set [%s]; the pairing below cannot tell them apart", bucket, identity)
			return nil
		}
		byKeys[identity] = contract
	}

	var pairs []sitePairing
	for _, site := range sites {
		contract, ok := byKeys[strings.Join(site.keys, ",")]
		if !ok {
			t.Errorf("%s:%d formats the key(s) [%s] and no table entry for %s declares that set; entries are matched to sites by key rather than by table order, so an entry whose keys the code no longer names cannot be matched at all",
				site.file, site.line, strings.Join(site.keys, ", "), bucket)
			continue
		}
		pairs = append(pairs, sitePairing{site: site, contract: contract})
	}
	return pairs
}

// keysAtOrigin collects the literals the sweep found at a contract's declared
// key origin.
func keysAtOrigin(evidence *sourceEvidence, origin keyOrigin) []string {
	seen := map[string]bool{}
	for _, literal := range evidence.goKeyOriginLiterals {
		if literal.callee != origin.callee || literal.argument != origin.argument {
			continue
		}
		if origin.within != "" && literal.within != origin.within {
			continue
		}
		seen[literal.value] = true
	}

	var keys []string
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// assertEveryContractIsInternallyConsistent ties the declared type column to
// the declared verb sequence, so the column cannot drift into decoration.
func assertEveryContractIsInternallyConsistent(t *testing.T) {
	t.Helper()

	for _, contract := range localizedFormatContracts {
		where := goFormatBodyKey(contract.file, contract.function) + " " + strings.Join(contract.keys, "/")
		if len(contract.keys) == 0 {
			t.Errorf("%s declares no catalogue key; an entry that names no key checks no catalogue", where)
		}
		if len(contract.verbs) == 0 {
			t.Errorf("%s declares no verb; a format with no verb needs no barrier and no entry", where)
		}
		if len(contract.verbs) != len(contract.argumentTypes) {
			t.Errorf("%s declares %d verb(s) and %d argument type(s); the two columns describe the same list and must have the same length", where, len(contract.verbs), len(contract.argumentTypes))
			continue
		}
		for index, verb := range contract.verbs {
			if !verbAcceptsGoType(verb, contract.argumentTypes[index]) {
				t.Errorf("%s declares %s at position %d for a Go %s; that pair renders as %%!-residue, so one of the two columns is wrong", where, verb, index+1, contract.argumentTypes[index])
			}
		}
	}
}

// assertEveryFallbackLiteralStillMatches is the second-copy half. The English
// literal beside each call is a duplicate of the catalogue format, and it is
// the copy that renders whenever the catalogue does not answer — so it drifts
// silently in exactly the cases nobody is looking at.
func assertEveryFallbackLiteralStillMatches(t *testing.T, evidence *sourceEvidence) {
	t.Helper()

	declaredByFunction := map[string]map[string]bool{}
	for _, contract := range localizedFormatContracts {
		key := goFormatBodyKey(contract.file, contract.function)
		if declaredByFunction[key] == nil {
			declaredByFunction[key] = map[string]bool{}
		}
		for _, fallback := range contract.fallbacks {
			declaredByFunction[key][fallback] = true
		}
	}

	for _, contract := range localizedFormatContracts {
		where := goFormatBodyKey(contract.file, contract.function)
		body := evidence.goFormatBodies[where]
		local := map[string]bool{}
		for _, literal := range body.patternLiterals {
			local[literal] = true
		}

		for _, fallback := range contract.fallbacks {
			present := local[fallback]
			if contract.fallbackArrivesIndirectly {
				// The literal is passed in by the caller, so it is not in this
				// function's body; the shipped-Go literal sweep is where it has
				// to still exist.
				present = evidence.literals[fallback]
			}
			if !present {
				t.Errorf("%s declares the fallback %q and no shipped Go file carries that literal any more; the fallback was edited and this entry was not, so the copy the owner reads when a catalogue misses is no longer the copy this barrier checks", where, fallback)
				continue
			}
			verbs, err := formatVerbsOf(fallback)
			if err != nil {
				t.Errorf("%s: reading the fallback %q: %v", where, fallback, err)
				continue
			}
			if strings.Join(verbs, " ") != strings.Join(contract.verbs, " ") {
				t.Errorf("%s: the fallback %q carries the verbs [%s] and the argument list requires [%s]; the fallback is the copy that renders when a catalogue misses, so this is the untranslated page rendering wrong",
					where, fallback, strings.Join(verbs, " "), strings.Join(contract.verbs, " "))
				continue
			}
			assertPatternFormatsCleanly(t, where+" fallback", fallback, contract.argumentTypes)
		}
	}

	// The reverse: a literal assigned to a pattern variable inside a declared
	// function that the table does not know about. A verb-free literal is
	// tolerated — it cannot misformat, and sendDaySaveStatus legitimately
	// assigns one ("Saved.") to the same variable on a branch that never
	// formats.
	for where, body := range evidence.goFormatBodies {
		declared := declaredByFunction[where]
		if declared == nil {
			continue
		}
		for _, literal := range body.patternLiterals {
			if declared[literal] {
				continue
			}
			verbs, err := formatVerbsOf(literal)
			if err != nil {
				t.Errorf("%s assigns the pattern literal %q: %v", where, literal, err)
				continue
			}
			if len(verbs) == 0 {
				continue
			}
			t.Errorf("%s assigns the undeclared pattern literal %q, carrying the verbs [%s]; a fallback the table does not know about is a second format nothing compares against the catalogue", where, literal, strings.Join(verbs, " "))
		}
	}
}

// assertEveryCatalogueMatchesItsContract is the catalogue half: every declared
// key, in every shipped language, must carry exactly the declared verb
// sequence and must survive a real fmt.Sprintf with the declared argument list.
//
// It reads manager.Messages(language) rather than the raw JSON on purpose:
// that overlaid map is precisely what a handler is handed and what
// lookupMessage reads, so a key a translator left out falls back to the English
// value here exactly as it does at run time.
func assertEveryCatalogueMatchesItsContract(t *testing.T, manager *i18n.Manager, languages []string) {
	t.Helper()

	checked := 0
	for _, language := range languages {
		messages := manager.Messages(language)
		for _, contract := range localizedFormatContracts {
			for _, key := range contract.keys {
				value, present := messages[key]
				if !present || strings.TrimSpace(value) == "" {
					// lookupMessage counts a blank value as a miss, so the
					// English fallback renders instead — checked above. What
					// fails here is only the key no catalogue answers at all,
					// which the webhook site has no fallback for.
					t.Errorf("%s: the %s catalogue does not answer %q, which %s formats", language, language, key, goFormatBodyKey(contract.file, contract.function))
					continue
				}
				checked++

				verbs, err := formatVerbsOf(value)
				if err != nil {
					t.Errorf("%s %s: %v", language, key, err)
					continue
				}
				if strings.Join(verbs, " ") != strings.Join(contract.verbs, " ") {
					t.Errorf("%s %s carries the verbs [%s] and %s passes [%s]:\n  %s = %q\n"+
						"The sequence is the contract, not the count: a locale that swapped two verbs while keeping the count renders "+
						"%%!s(int=12) or %%!d(string=days) into the page, in that language only.",
						language, key, strings.Join(verbs, " "), goFormatBodyKey(contract.file, contract.function), strings.Join(contract.verbs, " "), language, value)
					continue
				}
				assertPatternFormatsCleanly(t, language+" "+key, value, contract.argumentTypes)
			}
		}
	}

	// Anti-vacuity, measured 2026-08-24: eleven declared keys across six
	// languages is 66 checked pairs. The floor refuses a run that read the
	// catalogues and compared nothing.
	if checked < 66 {
		t.Fatalf("compared only %d (catalogue, key) pair(s); eleven keys across six languages is 66, so this run measured a fraction of what it claims to", checked)
	}
}

// assertPatternFormatsCleanly runs the pattern through Go's own formatter with
// arguments of the declared types. It is the check that needs no parser of
// mine: fmt itself marks a wrong type as %!s(int=...) and a wrong arity as
// %!(EXTRA ...) or %!(MISSING), and "%!" appears in no shipped copy.
func assertPatternFormatsCleanly(t *testing.T, where string, pattern string, argumentTypes []string) {
	t.Helper()

	arguments, err := sampleArgumentsFor(argumentTypes)
	if err != nil {
		t.Errorf("%s: %v", where, err)
		return
	}
	rendered := fmt.Sprintf(pattern, arguments...)
	if !strings.Contains(rendered, "%!") {
		return
	}
	t.Errorf("%s renders formatting residue:\n  pattern: %q\n  renders: %q\n"+
		"Go writes %%! wherever a verb and its argument disagree or an argument is missing. Whatever this string is, "+
		"that is what the page shows.", where, pattern, rendered)
}

// sampleArgumentsFor builds one argument per declared type. The values are
// arbitrary; only their dynamic types matter to fmt.
func sampleArgumentsFor(argumentTypes []string) ([]any, error) {
	arguments := make([]any, 0, len(argumentTypes))
	for _, declared := range argumentTypes {
		switch declared {
		case "int":
			arguments = append(arguments, 12)
		case "string":
			arguments = append(arguments, "days")
		case "float64":
			arguments = append(arguments, 36.55)
		default:
			return nil, fmt.Errorf("no sample value for the declared argument type %q; add one beside the verbs it is allowed to pair with", declared)
		}
	}
	return arguments, nil
}

// verbAcceptsGoType is the declared pairing between a format verb and a Go
// type. It is deliberately narrow: %v accepts anything and would make the type
// column meaningless, so a catalogue string is not allowed to reach for it.
func verbAcceptsGoType(verb string, goType string) bool {
	if verb == "" {
		return false
	}
	switch verb[len(verb)-1] {
	case 'd':
		return goType == "int"
	case 's':
		return goType == "string"
	case 'f':
		return goType == "float64"
	default:
		return false
	}
}

// formatVerbsOf extracts the ordered format-verb sequence of a pattern.
//
// The token returned is the verb as WRITTEN — "%.2f" and "%f" are different
// answers, because they render a basal temperature as 36.55 and 36.550000. A
// doubled "%%" is a literal percent sign and consumes no argument, so it is not
// a verb. An explicit argument index ("%[2]s") and a star width ("%*d") are
// refused rather than read: no catalogue uses either today, and a reader that
// accepted an index while comparing sequences positionally would agree with a
// string it had misunderstood.
func formatVerbsOf(pattern string) ([]string, error) {
	const flags = "+-# 0"
	const verbLetters = "bcdeEfFgGopqstTvxXU"

	var verbs []string
	for index := 0; index < len(pattern); index++ {
		if pattern[index] != '%' {
			continue
		}
		start := index
		index++
		if index < len(pattern) && pattern[index] == '%' {
			continue
		}
		for index < len(pattern) && strings.IndexByte(flags, pattern[index]) >= 0 {
			index++
		}
		if index < len(pattern) && (pattern[index] == '[' || pattern[index] == '*') {
			return nil, fmt.Errorf("%q uses an explicit argument index or a star width at byte %d; this barrier compares verb sequences positionally and refuses to guess at a reordered one", pattern, start)
		}
		for index < len(pattern) && pattern[index] >= '0' && pattern[index] <= '9' {
			index++
		}
		if index < len(pattern) && pattern[index] == '.' {
			index++
			if index < len(pattern) && pattern[index] == '*' {
				return nil, fmt.Errorf("%q uses a star precision at byte %d, which takes an argument of its own", pattern, start)
			}
			for index < len(pattern) && pattern[index] >= '0' && pattern[index] <= '9' {
				index++
			}
		}
		if index >= len(pattern) || strings.IndexByte(verbLetters, pattern[index]) < 0 {
			return nil, fmt.Errorf("%q carries a %% at byte %d that no verb letter closes; Go renders that as %%!(NOVERB) and a lone percent sign has to be written %%%%", pattern, start)
		}
		verbs = append(verbs, pattern[start:index+1])
	}
	return verbs, nil
}

// The barrier is only as good as the two readers underneath it, so both are
// measured on synthesised input rather than on the repository's own — a fixture
// that measures ovumcy-web against itself only proves ovumcy-web agrees with
// itself.
func TestFormatVerbReaderReadsTheSequenceAndNotTheCount(t *testing.T) {
	for _, probe := range []struct {
		pattern string
		want    string
		why     string
	}{
		{pattern: "", want: "", why: "a pattern with no verb has an empty sequence, not an error"},
		{pattern: "%d completed cycles shown. Latest cycle %d %s.", want: "%d %d %s", why: "the ordinary case"},
		{pattern: "%d readings this cycle. Coverline %.2f %s.", want: "%d %.2f %s", why: "the precision is part of the answer: %f renders 36.550000 where %.2f renders 36.55"},
		{pattern: "%s %d", want: "%s %d", why: "the same two verbs in the other order must not read as the same sequence — that swap is the whole reason this is not a count"},
		{pattern: "100%% sure, %s", want: "%s", why: "a doubled percent is a literal sign and consumes no argument"},
		{pattern: "%-8s|%+d", want: "%-8s %+d", why: "flags and widths belong to the verb, not before it"},
		// Not an error, and deliberately so: Go reads "% o" as the octal verb
		// with a space flag, so an undoubled percent sign in copy silently
		// becomes a verb. The sequence check is what catches it.
		{pattern: "50% off, %s", want: "% o %s", why: "an undoubled percent sign in front of a letter IS a verb to Go, and this reader must say what Go says"},
	} {
		verbs, err := formatVerbsOf(probe.pattern)
		if err != nil {
			t.Errorf("reading %q: %v (%s)", probe.pattern, err, probe.why)
			continue
		}
		if got := strings.Join(verbs, " "); got != probe.want {
			t.Errorf("read %q as [%s], want [%s] — %s", probe.pattern, got, probe.want, probe.why)
		}
	}

	for _, probe := range []struct {
		pattern string
		why     string
	}{
		{pattern: "%d of %d, 100%", why: "a trailing percent closes no verb; Go renders %!(NOVERB)"},
		{pattern: "%[2]s then %[1]s", why: "an explicit argument index reorders the list, and a positional comparison must refuse it rather than misread it"},
		{pattern: "%*d", why: "a star width consumes an argument of its own"},
		{pattern: "%.*f", why: "a star precision consumes an argument of its own"},
	} {
		if verbs, err := formatVerbsOf(probe.pattern); err == nil {
			t.Errorf("read %q as [%s] instead of refusing it — %s", probe.pattern, strings.Join(verbs, " "), probe.why)
		}
	}
}

func TestLocalizedFormatSiteReaderSeesOnlyTheComputedPatterns(t *testing.T) {
	const fixture = `package fixture

import "fmt"

func probeLocalizedSummary(messages map[string]string) string {
	pattern, translated := lookupMessage(messages, "probe.summary")
	if !translated {
		pattern = "%d items, %.2f average."
	}
	return fmt.Sprintf(pattern, 2, 1.5)
}

func probeLiteralPattern() string {
	return fmt.Sprintf("%d plain", 3)
}

func probeVariadicPassthrough(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}

func probeNoFormatting(messages map[string]string) string {
	message, _ := lookupMessage(messages, "probe.plain")
	return message
}
`

	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, "fixture.go", fixture, 0)
	if err != nil {
		t.Fatalf("parsing the fixture: %v", err)
	}

	evidence := newSourceEvidence()
	collectFromGoFile(goOrigin{file: "fixture.go", fileSet: fileSet}, parsed, evidence)

	if len(evidence.goFormatSites) != 1 {
		t.Fatalf("read %d site(s) from the fixture, want 1: the literal pattern is what `go vet` already checks, and the variadic passthrough forwards its caller's arguments rather than a catalogue value — recording either would make the contract table a list of every Sprintf in the repository", len(evidence.goFormatSites))
	}

	site := evidence.goFormatSites[0]
	if site.function != "probeLocalizedSummary" || site.pattern != "pattern" {
		t.Errorf("read the site as %s formatting through %q; the enclosing function and the pattern identifier are what pair a site to its contract", site.function, site.pattern)
	}
	if site.arguments != 2 {
		t.Errorf("counted %d argument(s) at the site, want 2; the argument count is the half of the arity check that is read from the code rather than declared", site.arguments)
	}
	if want := lineContaining(fixture, "fmt.Sprintf(pattern"); site.file != "fixture.go" || site.line != want {
		t.Errorf("the site reported %s:%d, want fixture.go:%d; a finding that cannot be opened is a finding nobody acts on", site.file, site.line, want)
	}

	body := evidence.goFormatBodies[goFormatBodyKey("fixture.go", "probeLocalizedSummary")]
	if strings.Join(body.patternLiterals, "|") != "%d items, %.2f average." {
		t.Errorf("read the pattern literals as %v; the fallback beside the call is the second copy of the format and is the whole reason this half exists", body.patternLiterals)
	}
	if strings.Join(site.keys, "|") != "probe.summary" {
		t.Errorf("resolved the site's key(s) as %v; a key the reader cannot see is a key the table is never held to", site.keys)
	}

	if _, recorded := evidence.goFormatBodies[goFormatBodyKey("fixture.go", "probeNoFormatting")]; recorded {
		t.Errorf("recorded a body for a function that formats nothing; catalogue keys matter to this barrier only where a Sprintf can reach them")
	}
}

// The review's finding in fixture form: a site whose key is written by its
// CALLER, and a site whose key reaches the lookup through a local variable and
// a package constant. The first draft of this reader saw none of the three, so
// six of eleven keys were declared in the table and compared against nothing.
func TestLocalizedFormatSiteReaderResolvesKeysItDoesNotSeeAtTheLookup(t *testing.T) {
	const fixture = `package fixture

import "fmt"

const probeConstantKey = "probe.from.constant"

func probeThroughCaller(messages map[string]string, key string, fallback string, value string) string {
	pattern, translated := lookupMessage(messages, key)
	if !translated {
		pattern = fallback
	}
	return fmt.Sprintf(pattern, value)
}

func probeCallerOne(messages map[string]string) string {
	return probeThroughCaller(messages, "probe.hint", "Hint: %s.", "x")
}

func probeCallerTwo(messages map[string]string) string {
	return probeThroughCaller(messages, "probe.error", "Error: %s.", "y")
}

func probeThroughLocals(messages map[string]string, branch bool) string {
	messageKey := probeConstantKey
	if branch {
		messageKey = "probe.from.literal"
	}
	pattern, translated := lookupMessage(messages, messageKey)
	if !translated {
		pattern = "%s fallback."
	}
	return fmt.Sprintf(pattern, "z")
}
`

	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, "fixture.go", fixture, 0)
	if err != nil {
		t.Fatalf("parsing the fixture: %v", err)
	}

	evidence := newSourceEvidence()
	collectFromGoFile(goOrigin{file: "fixture.go", fileSet: fileSet}, parsed, evidence)

	var throughLocals goFormatSite
	for _, site := range evidence.goFormatSites {
		if site.function == "probeThroughLocals" {
			throughLocals = site
		}
	}
	if got := strings.Join(throughLocals.keys, ","); got != "probe.from.constant,probe.from.literal" {
		t.Errorf("resolved the local-variable site's key(s) as [%s], want both the package constant and the literal the branch assigns; a key the reader drops is a key the table is never held to", got)
	}

	// The key-origin reader answers a narrower question than the site reader:
	// what literal stands AT the named argument, with no local resolution. A
	// call that passes a variable there records nothing, which is safe only
	// because an origin that collects nothing then compares an empty set
	// against the declared keys and fails by name rather than passing quietly.
	// That is asserted directly below, since it is the vacuity this shape is
	// most able to hide.
	for _, literal := range evidence.goKeyOriginLiterals {
		if literal.callee == "lookupMessage" && literal.within == goFormatBodyKey("fixture.go", "probeThroughLocals") {
			t.Errorf("the key-origin reader resolved %q at a call that passes a variable; it must read the literal standing at the argument and nothing else, or two readers with different rules would disagree about the same call", literal.value)
		}
	}
	if got := keysAtOrigin(evidence, keyOrigin{callee: "lookupMessage", argument: 1, within: goFormatBodyKey("fixture.go", "probeThroughLocals")}); len(got) != 0 {
		t.Errorf("an origin pointed at a variable argument collected %v; it must collect nothing, so the comparison against the declared keys fails instead of passing about an empty set", got)
	}

	evidence = newSourceEvidence()
	withFixtureKeyOriginCallee(t, "probeThroughCaller", func() {
		collectFromGoFile(goOrigin{file: "fixture.go", fileSet: fileSet}, parsed, evidence)
	})

	var keys []string
	for _, literal := range evidence.goKeyOriginLiterals {
		if literal.callee == "probeThroughCaller" && literal.argument == 1 {
			keys = append(keys, literal.value)
		}
	}
	sort.Strings(keys)
	if strings.Join(keys, ",") != "probe.error,probe.hint" {
		t.Errorf("collected the caller-written key(s) as [%s], want [probe.error, probe.hint]; this is exactly the repointing the barrier missed before the review — the key lives at the call, not at the lookup", strings.Join(keys, ", "))
	}

	// And the fallbacks travel with them, which is why fallbackArrivesIndirectly
	// looks in the shipped-Go literal set rather than in the callee's body.
	if !evidence.literals["Hint: %s."] || !evidence.literals["Error: %s."] {
		t.Errorf("the caller-written fallback literals were not collected; a fallback the sweep cannot see is a fallback the table can quietly outlive")
	}
}

// withFixtureKeyOriginCallee widens the derived callee set for one fixture run.
// The set is derived from the shipped table on purpose, so a self-test that
// needs another callee has to say so here rather than by adding a fake contract
// to the table the barrier judges the repository with.
func withFixtureKeyOriginCallee(t *testing.T, callee string, run func()) {
	t.Helper()

	if keyOriginCallees[callee] {
		t.Fatalf("%q is already a derived key-origin callee; this helper would hide a real entry rather than add a fixture one", callee)
	}
	keyOriginCallees[callee] = true
	defer delete(keyOriginCallees, callee)
	run()
}

// lineContaining is the fixture's own line lookup, so the expected line number
// is derived from the fixture rather than counted by hand into a constant that
// drifts the first time the fixture gains a line.
func lineContaining(source string, needle string) int {
	for index, line := range strings.Split(source, "\n") {
		if strings.Contains(line, needle) {
			return index + 1
		}
	}
	return 0
}

// A barrier that reports success on an empty sweep is the failure this class of
// test cannot see in itself.
func TestLocalizedFormatBarrierRefusesASweepThatSawNoFormatSite(t *testing.T) {
	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, "fixture.go", "package fixture\n\nfunc probe() string { return \"nothing to format\" }\n", 0)
	if err != nil {
		t.Fatalf("parsing the fixture: %v", err)
	}

	evidence := newSourceEvidence()
	collectFromGoFile(goOrigin{file: "fixture.go", fileSet: fileSet}, parsed, evidence)
	if len(evidence.goFormatSites) != 0 || len(evidence.goFormatBodies) != 0 {
		t.Fatalf("a file with no formatting reported %d site(s) and %d body(ies); it must report neither, which is precisely why the barrier refuses a sweep this small instead of reading its verdict", len(evidence.goFormatSites), len(evidence.goFormatBodies))
	}
}
