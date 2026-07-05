package api

import (
	"html/template"
	"reflect"
	"strings"
	"testing"
)

// Compile-time pin: templateToJSON must return a plain string so html/template
// applies full contextual escaping in the attribute context (base.html
// data-supported-languages, stats.html data-chart). Reverting it to a typed
// string (template.JS / template.HTML) would bypass the attribute escaper and
// fail this assignment — update this test only together with a conscious
// security review of every template call site.
var _ func(any) string = templateToJSON

// TestTemplateToJSONEscapesSafelyInSingleQuotedAttribute is the regression for
// the retired `#nosec G203` templateToJSON: adversarial JSON content embedded
// in a single-quoted data-* attribute must not be able to break out of the
// attribute or introduce markup, and must survive the browser's
// getAttribute/dataset entity-decoding + JSON.parse round trip unchanged
// (chart-lite.js consumes data-chart exactly that way; the test mirrors it via
// extractStatsChartPayload's html.UnescapeString + json.Unmarshal).
func TestTemplateToJSONEscapesSafelyInSingleQuotedAttribute(t *testing.T) {
	t.Parallel()

	payload := statsChartPayload{
		Labels: []string{
			"a'b",
			`</script><script>alert(1)</script>`,
			`double " quote and backslash \`,
			"amp & angle < >",
		},
		Values: []int{1, 2, 3, 4},
	}

	probe := template.Must(template.New("probe").Funcs(newTemplateFuncMap()).Parse(
		`<div id="probe" data-chart='{{toJSON .}}'></div>`,
	))
	var rendered strings.Builder
	if err := probe.Execute(&rendered, payload); err != nil {
		t.Fatalf("execute probe template: %v", err)
	}
	output := rendered.String()

	// Attribute breakout: the template contains exactly one single-quoted
	// attribute, so exactly its two delimiter quotes may survive rendering. Any
	// third raw single quote means the payload escaped the attribute value.
	if got := strings.Count(output, "'"); got != 2 {
		t.Fatalf("expected exactly 2 raw single quotes (attribute delimiters), got %d in: %q", got, output)
	}

	// Markup injection: no literal script terminator / opener may appear
	// anywhere in the rendered document.
	lowered := strings.ToLower(output)
	if strings.Contains(lowered, "</script") || strings.Contains(lowered, "<script") {
		t.Fatalf("rendered output contains literal script markup: %q", output)
	}

	// Browser-equivalent round trip: entity-decode the attribute value and
	// JSON-parse it (what getAttribute + JSON.parse do in chart-lite.js). The
	// payload must come back byte-identical.
	roundTripped, err := extractStatsChartPayload(output)
	if err != nil {
		t.Fatalf("round-trip extraction failed: %v", err)
	}
	if !reflect.DeepEqual(roundTripped, payload) {
		t.Fatalf("payload mutated in the escape round trip:\n  want %#v\n  got  %#v", payload, roundTripped)
	}
}

// TestTemplateToJSONReturnsNullOnMarshalError pins the fail-safe branch: when a
// value cannot be JSON-serialized (e.g. a channel), templateToJSON returns the
// literal "null" rather than propagating an error or emitting a partial string.
func TestTemplateToJSONReturnsNullOnMarshalError(t *testing.T) {
	t.Parallel()

	if got := templateToJSON(make(chan int)); got != "null" {
		t.Fatalf("expected %q on marshal error, got %q", "null", got)
	}
}
