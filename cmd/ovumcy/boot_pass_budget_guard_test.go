package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"testing"
	"time"
)

// TestBootPassContextCarriesAStorageBudget is the direct assertion on the
// helper: it must hand back a context that expires. Removing the WithTimeout —
// the one edit that silently restores the old unbounded behaviour — leaves a
// context with no deadline and fails here.
func TestBootPassContextCarriesAStorageBudget(t *testing.T) {
	ctx, cancel := bootPassContext()
	defer cancel()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("a boot pass must run under a deadline; this context has none, so a database that never answers hangs the boot with no listener and no refusal")
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		t.Fatalf("the budget is already spent at construction (remaining %s)", remaining)
	}
	if remaining > bootPassStorageBudget {
		t.Fatalf("deadline is %s away, further than the declared budget of %s", remaining, bootPassStorageBudget)
	}
}

// TestEveryBootPassRunsOnTheBoundedContext sweeps the boot sequence rather than
// a list of names.
//
// A boot pass is defined by WHERE it runs — after the repositories exist and
// before the dependencies are built, which is the window in main() where the
// database is open, the migrations have applied and no listener exists yet. So
// the guard reads that window out of main()'s own body and holds whatever it
// finds there to the budget. A fourth pass added to the window later is covered
// without anyone remembering to extend a list.
//
// The floor matters as much as the rule: a sweep that reached nothing and a
// sweep that passed print the same nothing. If the window yields fewer calls
// than the passes known to live there, the guard fails as unable to have
// measured anything rather than reporting success.
func TestEveryBootPassRunsOnTheBoundedContext(t *testing.T) {
	const (
		windowOpens  = "NewRepositories"
		windowCloses = "BuildDependencies"
		knownPasses  = 3
	)

	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "main.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}

	functions := map[string]*ast.FuncDecl{}
	for _, declaration := range file.Decls {
		if function, ok := declaration.(*ast.FuncDecl); ok && function.Body != nil {
			functions[function.Name.Name] = function
		}
	}
	mainFunc, ok := functions["main"]
	if !ok {
		t.Fatal("main() not found in main.go; the sweep has no boot sequence to read")
	}

	opensAt, closesAt := -1, -1
	for index, statement := range mainFunc.Body.List {
		if opensAt < 0 && callsSelector(statement, windowOpens) {
			opensAt = index
		}
		if opensAt >= 0 && callsSelector(statement, windowCloses) {
			closesAt = index
			break
		}
	}
	if opensAt < 0 || closesAt < 0 {
		t.Fatalf("could not locate the boot window in main(): %s at %d, %s at %d", windowOpens, opensAt, windowCloses, closesAt)
	}

	passNames := make([]string, 0, knownPasses)
	for _, statement := range mainFunc.Body.List[opensAt+1 : closesAt] {
		expression, ok := statement.(*ast.ExprStmt)
		if !ok {
			continue
		}
		call, ok := expression.X.(*ast.CallExpr)
		if !ok {
			continue
		}
		if identifier, ok := call.Fun.(*ast.Ident); ok {
			passNames = append(passNames, identifier.Name)
		}
	}

	if len(passNames) < knownPasses {
		t.Fatalf("the boot window yielded %d pass call(s) %v, fewer than the %d known to run there — the sweep did not reach what it is meant to guard", len(passNames), passNames, knownPasses)
	}

	for _, name := range passNames {
		function, ok := functions[name]
		if !ok {
			t.Fatalf("boot pass %s() is called in main() but not declared in main.go; extend the sweep to the file that holds it", name)
		}
		if !callsIdentifier(function.Body, "bootPassContext") {
			t.Errorf("boot pass %s() does not take its context from bootPassContext(), so it runs unbounded against storage", name)
		}
		if callsSelector(function.Body, "Background") {
			t.Errorf("boot pass %s() still reaches for context.Background(); an unbounded boot pass hangs the start instead of failing it", name)
		}
	}
}

// callsSelector reports whether the node contains a call whose function is a
// selector ending in name — context.Background, db.NewRepositories and so on.
func callsSelector(node ast.Node, name string) bool {
	found := false
	ast.Inspect(node, func(current ast.Node) bool {
		call, ok := current.(*ast.CallExpr)
		if !ok {
			return true
		}
		if selector, ok := call.Fun.(*ast.SelectorExpr); ok && selector.Sel.Name == name {
			found = true
			return false
		}
		return true
	})
	return found
}

// callsIdentifier reports whether the node contains a call to a package-local
// function of that name.
func callsIdentifier(node ast.Node, name string) bool {
	found := false
	ast.Inspect(node, func(current ast.Node) bool {
		call, ok := current.(*ast.CallExpr)
		if !ok {
			return true
		}
		if identifier, ok := call.Fun.(*ast.Ident); ok && identifier.Name == name {
			found = true
			return false
		}
		return true
	})
	return found
}

// TestBootPassBudgetGuardWouldSeeAnUnboundedPass proves the sweep can fail,
// because a guard that only ever passes and a guard that cannot fail print the
// same nothing. It runs the same two checks against synthetic sources: one pass
// that takes its context from bootPassContext and one that reaches for
// context.Background, and requires the guard to separate them.
func TestBootPassBudgetGuardWouldSeeAnUnboundedPass(t *testing.T) {
	for name, testCase := range map[string]struct {
		body            string
		wantBounded     bool
		wantsBackground bool
	}{
		"bounded pass": {
			body:        "ctx, cancel := bootPassContext(); defer cancel(); doWork(ctx)",
			wantBounded: true,
		},
		"unbounded pass": {
			body:            "doWork(context.Background())",
			wantsBackground: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			source := "package main\nfunc pass() {\n" + strings.ReplaceAll(testCase.body, "; ", "\n") + "\n}\n"
			file, err := parser.ParseFile(token.NewFileSet(), "synthetic.go", source, 0)
			if err != nil {
				t.Fatalf("parse synthetic source: %v", err)
			}
			body := file.Decls[0].(*ast.FuncDecl).Body

			if got := callsIdentifier(body, "bootPassContext"); got != testCase.wantBounded {
				t.Errorf("callsIdentifier(bootPassContext) = %v, want %v", got, testCase.wantBounded)
			}
			if got := callsSelector(body, "Background"); got != testCase.wantsBackground {
				t.Errorf("callsSelector(Background) = %v, want %v", got, testCase.wantsBackground)
			}
		})
	}
}
