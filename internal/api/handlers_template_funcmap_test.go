package api

import (
	"io/fs"
	"sort"
	"strings"
	"testing"
	texttemplate "text/template"
	"text/template/parse"

	"github.com/ovumcy/ovumcy-web/internal/templates"
)

// collectCalledIdentifiers walks a parsed template's tree and records every
// identifier used in a command position — which is exactly what a function
// call is in a Go template.
//
// The walk exists instead of a grep because a grep cannot tell a call from a
// field or a variable, and this map registers names like `t` and `dict` that
// appear inside ordinary prose and attribute values all over the templates.
// The parser already draws that line precisely.
func collectCalledIdentifiers(node parse.Node, called map[string]bool) {
	switch typed := node.(type) {
	case nil:
		return
	case *parse.ListNode:
		if typed == nil {
			return
		}
		for _, child := range typed.Nodes {
			collectCalledIdentifiers(child, called)
		}
	case *parse.ActionNode:
		collectCalledIdentifiers(typed.Pipe, called)
	case *parse.PipeNode:
		if typed == nil {
			return
		}
		for _, command := range typed.Cmds {
			collectCalledIdentifiers(command, called)
		}
	case *parse.CommandNode:
		for _, arg := range typed.Args {
			collectCalledIdentifiers(arg, called)
		}
	case *parse.IdentifierNode:
		called[typed.Ident] = true
	case *parse.IfNode:
		collectBranch(&typed.BranchNode, called)
	case *parse.RangeNode:
		collectBranch(&typed.BranchNode, called)
	case *parse.WithNode:
		collectBranch(&typed.BranchNode, called)
	case *parse.TemplateNode:
		collectCalledIdentifiers(typed.Pipe, called)
	}
}

func collectBranch(branch *parse.BranchNode, called map[string]bool) {
	collectCalledIdentifiers(branch.Pipe, called)
	collectCalledIdentifiers(branch.List, called)
	collectCalledIdentifiers(branch.ElseList, called)
}

// callsAcrossEveryShippedTemplate parses every template embedded into the
// binary and returns the set of functions they call.
//
// It reads `templates.Files`, the embed.FS the runtime itself parses, rather
// than walking the directory: a template that exists on disk but is not
// embedded never ships, and one that is embedded is exactly what the app runs.
func callsAcrossEveryShippedTemplate(t *testing.T) map[string]bool {
	t.Helper()

	// html/template.FuncMap is an alias of the text/template one, so this
	// passes straight through — no conversion.
	funcMap := newTemplateFuncMap()
	called := map[string]bool{}
	parsed := 0

	err := fs.WalkDir(templates.Files, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".html") {
			return nil
		}

		source, readErr := fs.ReadFile(templates.Files, path)
		if readErr != nil {
			return readErr
		}

		// Parsing with the real func map is itself half the check: a template
		// calling a function the map does not register fails here, which is
		// the same defect seen from the other side.
		tmpl, parseErr := texttemplate.New(path).Funcs(funcMap).Parse(string(source))
		if parseErr != nil {
			t.Fatalf("parsing %s: %v", path, parseErr)
		}

		for _, associated := range tmpl.Templates() {
			if associated.Tree != nil {
				collectCalledIdentifiers(associated.Root, called)
			}
		}
		parsed++

		return nil
	})
	if err != nil {
		t.Fatalf("walking the embedded templates: %v", err)
	}

	// A walk that silently read nothing would make every assertion below pass
	// while measuring an empty set — the shape of a green fixture about
	// nothing. Measured 2026-08-15: 38 templates ship, 20 at the top level and
	// 18 under components/, and an earlier sweep that missed the second
	// directory reported 15 of the 29 entries as dead.
	if parsed < 30 {
		t.Fatalf("parsed only %d templates; the embedded set is far larger, so this walk measured the wrong thing", parsed)
	}

	return called
}

// A templateFuncMap entry that no template calls is dead weight which reads as
// LIVE to every analyser this repository runs. `deadcode` counts a caller as a
// caller even when the caller is itself dead, so a services function reached
// only from such an entry stays "reachable", and the locale keys that function
// names stay reachable behind it.
//
// That cascade ran three layers deep in the 2026-08-15 clean-up: three unused
// entries carried four Go declarations, one model field and six locale keys
// with them, and `deadcode -test` reported none of it. This test is the
// barrier for the top of that cascade, which is the only layer whose removal
// makes the rest visible.
func TestEveryTemplateFuncIsCalledByATemplate(t *testing.T) {
	called := callsAcrossEveryShippedTemplate(t)

	var uncalled []string
	for name := range newTemplateFuncMap() {
		if !called[name] {
			uncalled = append(uncalled, name)
		}
	}
	sort.Strings(uncalled)

	if len(uncalled) > 0 {
		t.Fatalf("newTemplateFuncMap registers %d function(s) no shipped template calls: %s\n"+
			"Remove the entry, then re-ask the reachability question of everything it called — "+
			"the services helper below it, and the locale keys below that.",
			len(uncalled), strings.Join(uncalled, ", "))
	}
}

// The walker must find a call nested inside a pipeline, a parenthesised
// sub-expression and a branch body, or the barrier above would pass by simply
// failing to look — the exact way a green check can assert nothing.
//
// This uses a SYNTHESISED template rather than one of the repository's own, so
// it keeps testing the walker on the day the real templates change shape.
func TestCollectCalledIdentifiersSeesNestedCalls(t *testing.T) {
	const source = `
{{if hasBBT .Value}}{{end}}
{{with .Log}}{{ topLevel . }}{{end}}
{{range .Items}}{{ .Name | inPipeline }}{{end}}
{{ outer (inner .Value) }}
{{ template "partial" (dict "k" .V) }}
`

	funcMap := texttemplate.FuncMap{
		"hasBBT":     func(any) bool { return true },
		"topLevel":   func(any) string { return "" },
		"inPipeline": func(any) string { return "" },
		"outer":      func(any) string { return "" },
		"inner":      func(any) any { return nil },
		"dict":       func(...any) any { return nil },
	}

	tmpl, err := texttemplate.New("probe").Funcs(funcMap).Parse(source)
	if err != nil {
		t.Fatalf("parsing the probe: %v", err)
	}

	called := map[string]bool{}
	collectCalledIdentifiers(tmpl.Root, called)

	for _, want := range []string{"hasBBT", "topLevel", "inPipeline", "outer", "inner", "dict"} {
		if !called[want] {
			t.Errorf("the walker missed %q; a call it cannot see would read as an unused entry", want)
		}
	}
}

// The other direction: a name that is only ever a field or a variable must not
// be counted as a call, or a genuinely dead entry sharing that name would hide
// behind it.
func TestCollectCalledIdentifiersIgnoresFieldsAndVariables(t *testing.T) {
	const source = `{{$t := .Messages}}{{$t}}{{.dict}}{{.Log.Value}}`

	tmpl, err := texttemplate.New("probe").Parse(source)
	if err != nil {
		t.Fatalf("parsing the probe: %v", err)
	}

	called := map[string]bool{}
	collectCalledIdentifiers(tmpl.Root, called)

	for _, name := range []string{"t", "dict", "Messages", "Value"} {
		if called[name] {
			t.Errorf("%q was counted as a function call, but it is a field or a variable here", name)
		}
	}
}
