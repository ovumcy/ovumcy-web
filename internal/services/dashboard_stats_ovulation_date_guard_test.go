package services

import (
	"io/fs"
	"reflect"
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
// a `with`/`range`/variable that rebinds Stats and so hides the chain from this
// walk. Both names are read off the struct, so a rename turns the guard red
// instead of vacuous.
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
		trees := map[string]*parse.Tree{}
		root := parse.New(path)
		root.Mode = parse.SkipFuncCheck
		if _, err := root.Parse(string(source), "", "", trees); err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, tree := range trees {
			walkTemplateNode(tree.Root, func(node parse.Node) {
				switch typed := node.(type) {
				case *parse.FieldNode:
					if chainReads(typed.Ident, statsField.Name, dateField.Name) {
						violations = append(violations, path+": "+typed.String())
					}
					if len(typed.Ident) >= 2 && typed.Ident[0] == statsField.Name {
						statsReads[path+": "+strings.Join(typed.Ident[:2], ".")] = true
					}
				case *parse.VariableNode:
					if chainReads(typed.Ident[1:], statsField.Name, dateField.Name) {
						violations = append(violations, path+": "+typed.String())
					}
				case *parse.IfNode:
					if len(typed.Pipe.Decl) > 0 {
						violations = append(violations, rebindsStats(path, typed.Pipe, statsField.Name)...)
					}
				case *parse.WithNode:
					violations = append(violations, rebindsStats(path, typed.Pipe, statsField.Name)...)
				case *parse.RangeNode:
					violations = append(violations, rebindsStats(path, typed.Pipe, statsField.Name)...)
				case *parse.ActionNode:
					if len(typed.Pipe.Decl) > 0 {
						violations = append(violations, rebindsStats(path, typed.Pipe, statsField.Name)...)
					}
				}
			})
		}
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

func chainReads(idents []string, structName, fieldName string) bool {
	for index := 0; index+1 < len(idents); index++ {
		if idents[index] == structName && idents[index+1] == fieldName {
			return true
		}
	}
	return false
}

// rebindsStats reports a pipeline that hands Stats on under another name — a
// `with .Stats`, a `range .Stats`, or `$x := .Stats` — after which a bare
// `.OvulationDate` or `$x.OvulationDate` is no longer a chain through Stats.
func rebindsStats(path string, pipe *parse.PipeNode, structName string) []string {
	var found []string
	for _, command := range pipe.Cmds {
		for _, arg := range command.Args {
			if field, ok := arg.(*parse.FieldNode); ok && len(field.Ident) > 0 && field.Ident[len(field.Ident)-1] == structName {
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
