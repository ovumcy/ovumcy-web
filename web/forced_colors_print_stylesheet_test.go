package staticassets

import (
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
