package staticassets

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

// A Windows contrast theme repaints every fill, shadow and border colour in
// the reader's system colours, so a state drawn only by one of those collapses
// into an empty box: the period toggle's thumb, the logged-day dot, and the
// difference between a period, a fertile and a plain calendar day all vanished
// (WEB-262, reproduced in Chrome and Edge under the Aquatic and Desert themes).
// The fix restates each state in geometry inside a forced-colors block. The
// stale-bundle check in CI cannot see that block deleted from the source and
// rebuilt away — source and bundle then agree — and no Go or unit test renders
// forced colours, so the block that carries the MEANING gets its own barrier.
//
// It reads the EMBEDDED stylesheet, because embed.FS is what the binary serves.
func TestForcedColorsBlockRestatesEveryBackgroundOnlyState(t *testing.T) {
	t.Parallel()

	rules := servedStylesheet(t)
	block, depth, found := cutAtRule(rules, "@media (forced-colors:active){")
	if !found {
		t.Fatal("the served stylesheet has no forced-colors block; under a contrast theme the toggle thumb, the logged dot and every calendar day state are painted away")
	}
	// Outside every cascade layer: an unlayered rule outranks the layered
	// utilities at any specificity, so no dark-theme variant can tie with one
	// of these and silently win.
	if depth != 0 {
		t.Fatalf("the forced-colors block is nested %d levels deep (inside a cascade layer); a layered utility of higher specificity would override it", depth)
	}

	for _, want := range []struct {
		selector    string
		declaration string
		why         string
	}{
		{".period-toggle input{", "forced-color-adjust:none", "the toggle draws itself in system colours"},
		{".period-toggle input:after{", "background:canvastext", "the off thumb stays visible"},
		{".period-toggle input:checked{", "background:highlight", "on differs from off by more than an outline"},
		{".period-toggle input:checked:after{", "background:highlighttext", "the on thumb stays visible on the track"},
		{".calendar-data-marker,", "background:canvastext", "the logged-day dot stays drawn"},
		{".calendar-cell-period,.legend-swatch-period{", "border-width:3px", "a recorded period day is a thick border, in the grid and the key"},
		{".calendar-cell-predicted,.legend-swatch-predicted{", "border-style:dashed", "a predicted period day is dashed"},
		{".calendar-cell-start-window,.legend-swatch-start-window{", "border-style:dotted", "the start window is dotted"},
		{".calendar-cell-fertile,.legend-swatch-fertile{", "border-style:double", "a fertile day is double, not the plain cell's single line"},
		{".calendar-cell-overlap-period-fertile,.legend-swatch-overlap-period-fertile{", "outline:1px dashed canvastext", "a predicted day in the window carries both marks"},
		{".calendar-cell-today,.legend-swatch-today{", "outline:2px solid canvastext", "today keeps a ring once its shadow ring is dropped"},
		{".calendar-cell.selected,.calendar-cell:focus-visible{", "outline:3px solid highlight", "the selected and focused day stay marked"},
		{".dashboard-cycle-ribbon,.stats-cycle-stack-band{", "forced-color-adjust:none", "the phase pictures keep the colours that are their data"},
	} {
		rule, ok := cutRule(block, want.selector)
		if !ok {
			t.Errorf("the forced-colors block has no %s rule (%s)", want.selector, want.why)
			continue
		}
		if !strings.Contains(rule, want.declaration) {
			t.Errorf("the forced-colors %s rule lacks %s (%s): %s", want.selector, want.declaration, want.why, rule)
		}
	}
}

// Under forced colours only geometry tells one day from another, so every
// state the cell ladder can emit needs a shape of its own. Two states
// (pre-fertile and tentative ovulation) are dashed from their base rules, so
// a block that restated only the states it knew by name left both reading as a
// predicted PERIOD day: a fertility-tier day must never read as a period day.
//
// The set is read from the ladder itself, never listed here, so a state added
// to it without a forced-colours shape turns this red.
func TestForcedColorsGivesEveryLadderStateItsOwnShape(t *testing.T) {
	t.Parallel()

	ladder := ladderCellClasses(t)

	// Classes the ladder emits that are not a day STATE of their own.
	notStates := map[string]string{
		"calendar-cell-out":                  "dims an out-of-month day; it rides on whichever state the day has",
		"calendar-cell-today":                "a ring and an underlined number on top of the state, restated by its own rule",
		"calendar-cell-fertile-peak":         "a colour refinement of the fertile state, which owns the shape",
		"calendar-cell-fertile-edge":         "a colour refinement of the fertile state, which owns the shape",
		"calendar-cell-overlap-start-window": "a dotted variant of the overlap state, pinned beside it by the restated-states test",
	}
	// Anti-vacuity: the states this test exists for, by name.
	for _, name := range []string{
		"calendar-cell-period",
		"calendar-cell-predicted",
		"calendar-cell-start-window",
		"calendar-cell-fertile",
		"calendar-cell-overlap-period-fertile",
		"calendar-cell-pre-fertile",
		"calendar-cell-ovulation-tentative",
	} {
		if !ladder[name] {
			t.Fatalf("the cell ladder no longer emits %s; the shape check would pass without it", name)
		}
	}
	for name, why := range notStates {
		if !ladder[name] {
			t.Errorf("%s is excluded as a non-state (%s) but the ladder does not emit it; drop the stale exclusion", name, why)
		}
	}

	block, _, found := cutAtRule(servedStylesheet(t), "@media (forced-colors:active){")
	if !found {
		t.Fatal("the served stylesheet has no forced-colors block")
	}

	// The plain day: the base cell's 1px solid line, no outline.
	seen := map[string]string{"solid|1px|": "a plain day"}
	for name := range ladder {
		if _, skip := notStates[name]; skip {
			continue
		}
		decls, ok := forcedColorsRuleFor(block, "."+name)
		if !ok {
			t.Errorf("the forced-colors block has no rule for .%s; it keeps whatever its base rule draws, which may be another state's shape", name)
			continue
		}
		style, width := decls["border-style"], decls["border-width"]
		if style == "" || width == "" {
			t.Errorf(".%s declares border-style %q and border-width %q under forced colours; both must be explicit, or the missing half is inherited from the base rule", name, style, width)
			continue
		}
		shape := style + "|" + width + "|" + decls["outline"]
		if other, dup := seen[shape]; dup {
			t.Errorf(".%s draws the same shape (%s) as %s under forced colours; the two days read as one", name, shape, other)
			continue
		}
		seen[shape] = "." + name
	}
}

// ladderCellClasses parses the cell ladder and returns every calendar-cell-*
// class a rung appends to the cell's class string.
func ladderCellClasses(t *testing.T) map[string]bool {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "../internal/api/calendar_days_view_helpers.go", nil, 0)
	if err != nil {
		t.Fatalf("parse the cell ladder: %v", err)
	}
	classes := map[string]bool{}
	ast.Inspect(file, func(node ast.Node) bool {
		fn, ok := node.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "buildCalendarDays" {
			return true
		}
		ast.Inspect(fn, func(inner ast.Node) bool {
			assign, ok := inner.(*ast.AssignStmt)
			if !ok || assign.Tok != token.ADD_ASSIGN || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
				return true
			}
			target, ok := assign.Lhs[0].(*ast.Ident)
			literal, isLiteral := assign.Rhs[0].(*ast.BasicLit)
			if !ok || target.Name != "cellClass" || !isLiteral || literal.Kind != token.STRING {
				return true
			}
			value, err := strconv.Unquote(literal.Value)
			if err != nil {
				t.Fatalf("unquote %s: %v", literal.Value, err)
			}
			for _, class := range strings.Fields(value) {
				if strings.HasPrefix(class, "calendar-cell-") {
					classes[class] = true
				}
			}
			return true
		})
		return false
	})
	return classes
}

// forcedColorsRuleFor returns the declarations of the block's rule whose
// selector list holds exactly selector as one item (so the compound
// ".a.b" selector is not mistaken for ".a").
func forcedColorsRuleFor(block string, selector string) (map[string]string, bool) {
	for _, rule := range strings.Split(block, "}") {
		head, body, ok := strings.Cut(rule, "{")
		if !ok {
			continue
		}
		for _, item := range strings.Split(head, ",") {
			if strings.TrimSpace(item) != selector {
				continue
			}
			decls := map[string]string{}
			for _, decl := range strings.Split(body, ";") {
				if property, value, ok := strings.Cut(decl, ":"); ok {
					decls[strings.TrimSpace(property)] = strings.TrimSpace(value)
				}
			}
			return decls, true
		}
	}
	return nil, false
}

// The fixed tab bar printed over the page content and a dark-theme page
// printed pale text; the print block hides the interface chrome and flips the
// ink tokens in either theme.
func TestPrintBlockHidesChromeAndPrintsDarkInk(t *testing.T) {
	t.Parallel()

	rules := servedStylesheet(t)
	block, depth, found := cutAtRule(rules, "@media print{")
	if !found {
		t.Fatal("the served stylesheet has no print block; the fixed tab bar prints over the content")
	}
	if depth != 0 {
		t.Fatalf("the print block is nested %d levels deep (inside a cascade layer); a layered utility would override it", depth)
	}

	chrome, ok := cutRule(block, ".skip-link,.paper-header,.mobile-tabbar,.toast-stack,.mobile-install-offer{")
	if !ok || !strings.Contains(chrome, "display:none") {
		t.Errorf("the print block does not hide the header, tab bar, toasts and install offer: %q", chrome)
	}
	tokens, ok := cutRule(block, ":root,:root[data-theme=dark]{")
	if !ok {
		t.Fatal("the print block does not reset the tokens for the dark theme as well as the light one")
	}
	if !strings.Contains(tokens, "--text-primary:#000") {
		t.Errorf("the print block leaves the body ink as it is on screen; a dark-theme page prints pale text: %s", tokens)
	}
}

func servedStylesheet(t *testing.T) string {
	t.Helper()
	stylesheet, err := Files.ReadFile("static/css/tailwind.css")
	if err != nil {
		t.Fatalf("read the embedded stylesheet: %v", err)
	}
	return string(stylesheet)
}

// cutAtRule returns the body of the at-rule that opens at the first occurrence
// of open, matched to its closing brace, and the brace depth the at-rule
// itself sits at (0 = top level, outside every @layer). The minified bundle
// carries no braces inside strings in these blocks, so brace counting is
// enough; it is not a CSS parser and does not pretend to be one.
func cutAtRule(rules string, open string) (string, int, bool) {
	at := strings.Index(rules, open)
	if at < 0 {
		return "", 0, false
	}
	depth := strings.Count(rules[:at], "{") - strings.Count(rules[:at], "}")
	start := at + len(open)
	level := 1
	for i := start; i < len(rules); i++ {
		switch rules[i] {
		case '{':
			level++
		case '}':
			level--
			if level == 0 {
				return rules[start:i], depth, true
			}
		}
	}
	return "", depth, false
}

// cutRule returns the declarations of the rule whose selector list starts
// with selector inside block. A selector given with a trailing comma matches
// the head of a longer list.
func cutRule(block string, selector string) (string, bool) {
	at := strings.Index(block, selector)
	if at < 0 {
		return "", false
	}
	rest := block[at:]
	open := strings.Index(rest, "{")
	end := strings.Index(rest, "}")
	if open < 0 || end < open {
		return "", false
	}
	return rest[open+1 : end], true
}
