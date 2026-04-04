package api

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParsePartialTemplatesIncludesBaseHelpers(t *testing.T) {
	t.Parallel()

	templateDir := t.TempDir()
	baseTemplate := `{{define "base"}}base{{end}}{{define "readonly_log_summary"}}<p>{{t .Messages "dashboard.period_day"}}: {{if .Log.IsPeriod}}{{t .Messages "common.yes"}}{{else}}{{t .Messages "common.no"}}{{end}}</p><p>{{t .Messages "dashboard.flow"}}: {{flowLabel .Messages .Log.Flow}}</p>{{end}}`
	partialTemplate := `{{define "day_editor_partial"}}{{template "readonly_log_summary" (dict "Messages" .Messages "Log" .Log)}}{{end}}`

	if err := os.WriteFile(filepath.Join(templateDir, "base.html"), []byte(baseTemplate), 0o600); err != nil {
		t.Fatalf("write base template: %v", err)
	}
	if err := os.WriteFile(filepath.Join(templateDir, "day_editor_partial.html"), []byte(partialTemplate), 0o600); err != nil {
		t.Fatalf("write partial template: %v", err)
	}

	partials, err := parsePartialTemplates(templateDir, newTemplateFuncMap(), []string{"day_editor_partial.html"})
	if err != nil {
		t.Fatalf("parse partial templates: %v", err)
	}

	payload := map[string]any{
		"Messages": map[string]string{
			"dashboard.period_day":       "Period day",
			"common.yes":                 "Yes",
			"common.no":                  "No",
			"dashboard.flow":             "Flow",
			"dashboard.flow.none":        "None",
		},
		"Log": struct {
			IsPeriod bool
			Flow     string
		}{
			IsPeriod: true,
			Flow:     "none",
		},
	}

	var output bytes.Buffer
	if err := partials["day_editor_partial"].ExecuteTemplate(&output, "day_editor_partial", payload); err != nil {
		t.Fatalf("execute partial template: %v", err)
	}

	rendered := output.String()
	expected := []string{"Period day", "Yes", "Flow", "None"}
	for _, fragment := range expected {
		if !strings.Contains(rendered, fragment) {
			t.Fatalf("expected rendered output to include %q, got %q", fragment, rendered)
		}
	}
}
