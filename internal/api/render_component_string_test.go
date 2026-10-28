package api

import (
	"html/template"
	"strings"
	"testing"
)

// The period-off header sends nothing when the question cannot be rendered,
// so both failures must come back as errors rather than as an empty question
// that would clear the toggle's.
func TestRenderComponentStringRefusesWhatItCannotRender(t *testing.T) {
	if _, err := (&Handler{}).renderComponentString("period_off_confirm", nil); err == nil {
		t.Fatal("a handler without parsed partials rendered a component")
	}

	tree := template.Must(template.New("page").Parse(`{{define "question"}}asks {{.}}{{end}}`))
	handler := &Handler{components: tree}
	if _, err := handler.renderComponentString("missing", nil); err == nil {
		t.Fatal("an undefined component rendered")
	}
	rendered, err := handler.renderComponentString("question", "twice")
	if err != nil || strings.TrimSpace(rendered) != "asks twice" {
		t.Fatalf("rendered %q, %v", rendered, err)
	}
}
