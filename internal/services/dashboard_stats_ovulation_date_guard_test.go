package services

import (
	"io/fs"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"text/template/parse"

	"github.com/ovumcy/ovumcy-web/internal/templates"
)

// DashboardViewData.Stats.OvulationDate keeps the confirmed day under the
// fertility gate (the overview's put-back), so a template reading it would name
// a day the gate withheld. The dashboard names the day from DisplayOvulationDate.
// The guard walks the parsed tree of every embedded template — the dashboard's
// partials included — and reds a field chain through Stats to OvulationDate, or
// a `with`/`range`/variable/`template` call that rebinds Stats and so hides the
// chain from this walk. Both names are read off the struct, so a rename turns the
// guard red instead of vacuous.
func TestNoTemplateReadsTheDashboardStatsOvulationDate(t *testing.T) {
	statsField, ok := reflect.TypeOf(DashboardViewData{}).FieldByName("Stats")
	if !ok {
		t.Fatal("DashboardViewData has no Stats field; the guard's subject is gone")
	}
	dateField, ok := statsField.Type.FieldByName("OvulationDate")
	if !ok {
		t.Fatalf("%s has no OvulationDate field; the guard's subject is gone", statsField.Type)
	}

	var violations []string
	statsReads := map[string]bool{}
	err := fs.WalkDir(templates.Files, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() || !strings.HasSuffix(path, ".html") {
			return walkErr
		}
		source, err := fs.ReadFile(templates.Files, path)
		if err != nil {
			return err
		}
		found, err := statsOvulationReads(path, string(source), statsField.Name, dateField.Name, statsReads)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		violations = append(violations, found...)
		return nil
	})
	if err != nil {
		t.Fatalf("walk templates: %v", err)
	}
	for _, violation := range violations {
		t.Errorf("a template reads or aliases Stats.OvulationDate: %s", violation)
	}
	// The walk must reach a read of Stats that is known to be there, or it proved
	// nothing: the dashboard header takes its phase from Stats.CurrentPhase.
	if site := "dashboard.html: Stats.CurrentPhase"; !statsReads[site] {
		t.Fatalf("the walk did not reach %s; the guard is vacuous", site)
	}
}

// A partial invoked as `{{template "x" .Stats}}` receives Stats as its dot, so a
// bare `.OvulationDate` inside it is a chain through Stats that no field walk of
// the partial can see. The call itself is the finding. Each case feeds the walk
// one source: the rebinding call must red, and a call handing on anything else
// must not.
func TestStatsOvulationDateGuardSeesAPartialHandedStatsAsDot(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		source string
		want   int
	}{
		{"template call handing on Stats", `{{define "x"}}{{.OvulationDate}}{{end}}{{template "x" .Stats}}`, 1},
		{"template call handing on Stats through the root variable", `{{define "x"}}{{.OvulationDate}}{{end}}{{template "x" $.Stats}}`, 1},
		{"template call handing on another field", `{{define "x"}}{{.OvulationDate}}{{end}}{{template "x" .Other}}`, 0},
		{"template call with no argument", `{{define "x"}}{{.Other}}{{end}}{{template "x"}}`, 0},
		{"direct chain through Stats", `{{.Stats.OvulationDate}}`, 1},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			found, err := statsOvulationReads("fixture.html", testCase.source, "Stats", "OvulationDate", map[string]bool{})
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if len(found) != testCase.want {
				t.Errorf("violations = %v, want %d", found, testCase.want)
			}
		})
	}
}

// statsOvulationReads parses one template file and returns every place that
// reads Stats.OvulationDate or hands Stats on under another dot. statsReads
// collects the Stats.<Field> reads it walked, for the caller's anti-vacuity check.
func statsOvulationReads(path string, source string, statsName string, dateName string, statsReads map[string]bool) ([]string, error) {
	trees := map[string]*parse.Tree{}
	root := parse.New(path)
	root.Mode = parse.SkipFuncCheck
	if _, err := root.Parse(source, "", "", trees); err != nil {
		return nil, err
	}
	var violations []string
	for _, tree := range trees {
		walkTemplateNode(tree.Root, func(node parse.Node) {
			switch typed := node.(type) {
			case *parse.FieldNode:
				if chainReads(typed.Ident, statsName, dateName) {
					violations = append(violations, path+": "+typed.String())
				}
				if len(typed.Ident) >= 2 && typed.Ident[0] == statsName {
					statsReads[path+": "+strings.Join(typed.Ident[:2], ".")] = true
				}
			case *parse.VariableNode:
				if chainReads(typed.Ident[1:], statsName, dateName) {
					violations = append(violations, path+": "+typed.String())
				}
			case *parse.IfNode:
				if len(typed.Pipe.Decl) > 0 {
					violations = append(violations, rebindsStats(path, typed.Pipe, statsName)...)
				}
			case *parse.WithNode:
				violations = append(violations, rebindsStats(path, typed.Pipe, statsName)...)
			case *parse.RangeNode:
				violations = append(violations, rebindsStats(path, typed.Pipe, statsName)...)
			case *parse.ActionNode:
				if len(typed.Pipe.Decl) > 0 {
					violations = append(violations, rebindsStats(path, typed.Pipe, statsName)...)
				}
			case *parse.TemplateNode:
				// The named partial runs with the pipeline's value as its dot.
				if typed.Pipe != nil {
					for _, found := range rebindsStats(path, typed.Pipe, statsName) {
						violations = append(violations, found+" (to template "+strconv.Quote(typed.Name)+")")
					}
				}
			}
		})
	}
	return violations, nil
}

func chainReads(idents []string, structName, fieldName string) bool {
	for index := 0; index+1 < len(idents); index++ {
		if idents[index] == structName && idents[index+1] == fieldName {
			return true
		}
	}
	return false
}

// rebindsStats reports a pipeline that hands Stats on under another name — a
// `with .Stats`, a `range .Stats`, `$x := .Stats`, or `template "x" .Stats` —
// after which a bare `.OvulationDate` or `$x.OvulationDate` is no longer a chain
// through Stats.
func rebindsStats(path string, pipe *parse.PipeNode, structName string) []string {
	var found []string
	for _, command := range pipe.Cmds {
		for _, arg := range command.Args {
			var idents []string
			switch typed := arg.(type) {
			case *parse.FieldNode:
				idents = typed.Ident
			case *parse.VariableNode:
				idents = typed.Ident[1:]
			}
			if len(idents) > 0 && idents[len(idents)-1] == structName {
				found = append(found, path+": "+pipe.String()+" rebinds "+structName)
			}
		}
	}
	return found
}

// walkTemplateNode visits every node under node, the control structures'
// branches and the pipelines' commands and arguments included.
func walkTemplateNode(node parse.Node, visit func(parse.Node)) {
	if node == nil || reflect.ValueOf(node).IsNil() {
		return
	}
	visit(node)
	switch typed := node.(type) {
	case *parse.ListNode:
		for _, child := range typed.Nodes {
			walkTemplateNode(child, visit)
		}
	case *parse.ActionNode:
		walkTemplateNode(typed.Pipe, visit)
	case *parse.PipeNode:
		for _, command := range typed.Cmds {
			walkTemplateNode(command, visit)
		}
	case *parse.CommandNode:
		for _, arg := range typed.Args {
			walkTemplateNode(arg, visit)
		}
	case *parse.IfNode:
		walkTemplateNode(typed.Pipe, visit)
		walkTemplateNode(typed.List, visit)
		walkTemplateNode(typed.ElseList, visit)
	case *parse.RangeNode:
		walkTemplateNode(typed.Pipe, visit)
		walkTemplateNode(typed.List, visit)
		walkTemplateNode(typed.ElseList, visit)
	case *parse.WithNode:
		walkTemplateNode(typed.Pipe, visit)
		walkTemplateNode(typed.List, visit)
		walkTemplateNode(typed.ElseList, visit)
	case *parse.TemplateNode:
		walkTemplateNode(typed.Pipe, visit)
	case *parse.ChainNode:
		walkTemplateNode(typed.Node, visit)
	}
}
