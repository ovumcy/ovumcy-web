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
	// Bounded on BOTH sides against the declared constant. An upper bound alone
	// would let the helper be rewritten with a literal far below it — one second
	// passes "positive and under five minutes" while making the constant
	// decorative and turning a slow-but-working start into a failing one, which
	// is the exact risk the constant's rationale is written to avoid.
	remaining := time.Until(deadline)
	const slack = 30 * time.Second
	if remaining > bootPassStorageBudget {
		t.Fatalf("deadline is %s away, further than the declared budget of %s", remaining, bootPassStorageBudget)
	}
	if remaining < bootPassStorageBudget-slack {
		t.Fatalf("deadline is only %s away against a declared budget of %s; the helper is not using the constant it documents", remaining, bootPassStorageBudget)
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
// without anyone remembering to extend a list — including one wired behind a
// config flag, since collectBootPassCalls descends into nested blocks. What it
// does not claim to cover is written on that function, and the floor below is
// what keeps the gap from reading as success.
//
// The floor matters as much as the rule: a sweep that reached nothing and a
// sweep that passed print the same nothing. If the window yields fewer calls
// than the passes known to live there, the guard fails as unable to have
// measured anything rather than reporting success.
func TestEveryBootPassRunsOnTheBoundedContext(t *testing.T) {
	const (
		windowOpens  = "BuildRepositories"
		windowCloses = "BuildDependencies"
		knownPasses  = 4
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

	passNames := collectBootPassCalls(mainFunc.Body.List[opensAt+1 : closesAt])

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

// collectBootPassCalls returns, in order and without repeats, the names of the
// package-local functions INVOKED AS STATEMENTS anywhere inside the given
// statements — including inside an if, a for or a nested block, which is the
// shape a conditional boot pass takes (the reminder scheduler two lines below
// the window is exactly that).
//
// It deliberately looks at statements rather than at every call expression: a
// boot pass is invoked, not passed as an argument, and collecting argument-level
// calls would demand a budget of helpers that merely compute a parameter.
//
// The two statement shapes it recognises are a bare call and an assignment from
// one. A pass wired in some third shape — a call inside a `switch` guard, say —
// would not be seen, which is why the caller keeps a floor on the count rather
// than trusting this to be exhaustive.
func collectBootPassCalls(statements []ast.Stmt) []string {
	names := make([]string, 0, 4)
	seen := map[string]bool{}

	record := func(expression ast.Expr) {
		call, ok := expression.(*ast.CallExpr)
		if !ok {
			return
		}
		identifier, ok := call.Fun.(*ast.Ident)
		if !ok || seen[identifier.Name] {
			return
		}
		// The budget helper is the one name worth excluding by name: hoisting
		// `ctx, cancel := bootPassContext()` into the window is a reasonable
		// refactor, and without this the guard would record the helper as a pass
		// and then report that bootPassContext does not take its context from
		// bootPassContext — a failure describing nothing anyone can act on.
		if identifier.Name == "bootPassContext" {
			return
		}
		seen[identifier.Name] = true
		names = append(names, identifier.Name)
	}

	for _, statement := range statements {
		ast.Inspect(statement, func(current ast.Node) bool {
			switch node := current.(type) {
			case *ast.ExprStmt:
				record(node.X)
			case *ast.AssignStmt:
				for _, value := range node.Rhs {
					record(value)
				}
			}
			return true
		})
	}
	return names
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

// TestCollectBootPassCallsReachesPassesInsideBlocks is the test for the claim
// the sweep rests on. Reading only the top-level statements of the window would
// find the three passes wired today and silently miss a fourth added behind a
// config flag — the shape the reminder scheduler already uses two lines below
// the window — so the guard would report success over an unbounded pass. Each
// case names the shape it stands for.
func TestCollectBootPassCallsReachesPassesInsideBlocks(t *testing.T) {
	source := `package main
func main() {
	plainPass(repositories)
	if config.Enabled {
		conditionalPass(repositories)
	}
	outcome := assignedPass(repositories)
	wrapper(argumentHelper())
	log.Print(notAPass())
	ctx, cancel := bootPassContext()
	_ = outcome
	_ = ctx
	_ = cancel
}
`
	file, err := parser.ParseFile(token.NewFileSet(), "synthetic.go", source, 0)
	if err != nil {
		t.Fatalf("parse synthetic source: %v", err)
	}
	body := file.Decls[0].(*ast.FuncDecl).Body

	got := collectBootPassCalls(body.List)
	want := []string{"plainPass", "conditionalPass", "assignedPass", "wrapper"}
	if len(got) != len(want) {
		t.Fatalf("collected %v, want %v", got, want)
	}
	for index, name := range want {
		if got[index] != name {
			t.Fatalf("collected %v, want %v", got, want)
		}
	}
	// argumentHelper and notAPass are calls, but neither is INVOKED as a
	// statement: one computes an argument, the other sits inside a selector
	// call. Demanding a storage budget of those would make the guard fire on
	// helpers that never touch storage.
	for _, name := range got {
		if name == "argumentHelper" || name == "notAPass" {
			t.Fatalf("argument-level call %q must not be read as a boot pass", name)
		}
		if name == "bootPassContext" {
			t.Fatal("the budget helper must not be read as a boot pass; hoisting it into the window would make the guard demand it take its context from itself")
		}
	}
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
