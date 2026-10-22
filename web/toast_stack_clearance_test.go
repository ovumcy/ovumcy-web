package staticassets

import (
	"strings"
	"testing"
)

// A toast used to sit at bottom:1rem on every width, which on a phone put it
// squarely over the floating tab bar (z-index 10000 against 40): every tap on
// the primary navigation landed on the toast for the ~5 s it stayed up. The
// stack now starts above the bar wherever the bar is shown, and the stack box
// itself, which spans the viewport width and exists empty from init, never
// takes a tap.
//
// The assertions read the embedded bundle (what the binary serves), not the
// file on disk, and key the clearance on the bar's own breakpoint: the rule
// must apply exactly where `.sm:hidden` does NOT hide the bar.
func TestToastStackClearsTheMobileTabBarInTheServedStylesheet(t *testing.T) {
	t.Parallel()

	raw, err := Files.ReadFile("static/css/tailwind.css")
	if err != nil {
		t.Fatalf("read the embedded stylesheet: %v", err)
	}
	css := string(raw)

	// The bar is hidden by `sm:hidden`; its media condition is where the bar
	// stops showing.
	const barHidden = `.sm\:hidden{display:none}`
	barAt := strings.Index(css, barHidden)
	if barAt < 0 {
		t.Fatalf("the served stylesheet carries no %s rule, so the tab bar's breakpoint cannot be derived", barHidden)
	}
	barMedia := innermostMedia(css, barAt)
	if barMedia == "" {
		t.Fatalf("%s is not inside an @media block; the tab bar would be hidden on every width", barHidden)
	}

	const clearanceSelector = ":root:has(.mobile-tabbar) .toast-stack{"
	clearAt := strings.Index(css, clearanceSelector)
	if clearAt < 0 {
		t.Fatalf("the served stylesheet carries no %s rule; a toast on a phone covers the tab bar and swallows its taps", clearanceSelector)
	}
	clearMedia := innermostMedia(css, clearAt)
	if want := barMediaComplement(barMedia); clearMedia != want {
		t.Fatalf("the toast clearance applies under %q, but the tab bar is shown exactly under %q (the complement of %q)", clearMedia, want, barMedia)
	}
	clearRule, ok := declarationBlock(css, clearAt+len(clearanceSelector))
	if !ok || !strings.Contains(clearRule, "bottom:var(--mobile-tabbar-clearance)") {
		t.Fatalf("the clearance rule does not lift the stack by --mobile-tabbar-clearance: %q", clearRule)
	}
	if !strings.Contains(css, "--mobile-tabbar-clearance:") {
		t.Fatal("--mobile-tabbar-clearance is read by the toast stack but never defined")
	}

	baseRule, ok := declarationBlock(css, strings.Index(css, ".toast-stack{")+len(".toast-stack{"))
	if !ok || !strings.Contains(baseRule, "pointer-events:none") {
		t.Fatalf("the toast stack box takes taps across the viewport width; it must carry pointer-events:none: %q", baseRule)
	}
	childRule, ok := declarationBlock(css, strings.Index(css, ".toast-stack>*{")+len(".toast-stack>*{"))
	if !ok || !strings.Contains(childRule, "pointer-events:auto") {
		t.Fatalf("with the stack at pointer-events:none, its toasts must restore pointer-events:auto or the close button stops working: %q", childRule)
	}
}

// barMediaComplement names the condition under which a min-width-gated
// `hidden` leaves an element shown, in the spelling the minifier emits.
func barMediaComplement(media string) string {
	return "@media not all and " + strings.TrimPrefix(media, "@media ")
}

// declarationBlock returns the text from start up to the next closing brace.
func declarationBlock(css string, start int) (string, bool) {
	if start < 0 || start > len(css) {
		return "", false
	}
	end := strings.IndexByte(css[start:], '}')
	if end < 0 {
		return "", false
	}
	return css[start : start+end], true
}

// innermostMedia returns the prelude of the innermost @media block enclosing
// offset at, or "" when there is none. It tracks block nesting and skips quoted
// strings; it is not a CSS parser beyond that.
func innermostMedia(css string, at int) string {
	var stack []string
	boundary := 0
	for i := 0; i < at && i < len(css); i++ {
		switch c := css[i]; c {
		case '"', '\'':
			if end := strings.IndexByte(css[i+1:], c); end >= 0 {
				i += end + 1
			}
		case '{':
			stack = append(stack, strings.TrimSpace(css[boundary:i]))
			boundary = i + 1
		case '}':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			boundary = i + 1
		case ';':
			boundary = i + 1
		}
	}
	for i := len(stack) - 1; i >= 0; i-- {
		if strings.HasPrefix(stack[i], "@media") {
			return stack[i]
		}
	}
	return ""
}
