package api

import (
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"sort"
	"strings"
	"testing"
)

// Which calendar day it is, in this package, comes from Handler.clockNow and
// nowhere else. A route test fixes today through onboardingTestAppOptions.now,
// and only clockNow reads that option: a handler still on time.Now answers at
// the real today whatever the test set, so a year-9999 or month-boundary case
// written against it passes or fails by the date it runs on.
//
// This package turns an instant into a day by localising it — time.Time.In
// with the request's location, then services.DateAtLocation or a date format.
// So the barrier's subject is every production function declared in
// internal/api whose body references both the time.Now object and the
// (time.Time).In method, both resolved through go/types rather than by
// spelling: a function that localises the wall clock is reading "today" off it.
// Cookie and token expiry read time.Now unlocalised and are not the subject.
//
// Limit, stated so no reader concludes more: the sweep judges one function at a
// time, so a helper that returns time.Now() to a caller that localises it
// crosses a function boundary this barrier does not follow.

// wallClockLocalisedForExpiry names a function that localises the wall clock
// for a timestamp that is an expiry clock rather than a calendar day. Each
// entry carries the reason; an entry whose function no longer localises the
// wall clock is stale and fails the barrier.
var wallClockLocalisedForExpiry = map[string]string{
	"Handler.Register":       "the account's created-at stamp and the register-pickup cookie's issue time; instants, not a calendar day",
	"Handler.ForgotPassword": "the recovery attempt's issue time, which only the reset token's TTL reads",
}

// wallClockLoadBearingSite is the function every page view takes its today
// from. The barrier must see it localising an instant and reading clockNow, or
// it resolved nothing where it matters.
const wallClockLoadBearingSite = "Handler.currentPageViewContext"

func TestNoAPIFunctionTurnsTheWallClockIntoADay(t *testing.T) {
	evidence := loadTreeEvidence(t)
	apiPkg := evidence.packageByPath(apiPackagePath)
	if apiPkg == nil {
		t.Fatalf("the sweep did not load %s; there is nothing to judge", apiPackagePath)
	}

	objects := wallClockObjectsFor(t, apiPkg.Types)
	summaries := summariseClockReads(apiPkg.Fset, apiPkg.Syntax, apiPkg.TypesInfo, objects)

	anchor, ok := summaries[wallClockLoadBearingSite]
	if !ok {
		t.Fatalf("the sweep found no declaration of %s; it is judging the wrong package", wallClockLoadBearingSite)
	}
	if !anchor.localises || !anchor.readsHandlerClock {
		t.Fatalf("%s: localises=%v readsHandlerClock=%v; the sweep resolved neither time.Time.In nor Handler.clockNow at the site every page view reads today from",
			wallClockLoadBearingSite, anchor.localises, anchor.readsHandlerClock)
	}
	if clock := summaries["Handler.clockNow"]; !clock.readsWallClock {
		t.Fatalf("Handler.clockNow references no time.Now; the sweep's time.Now resolution is broken")
	}

	var findings []string
	for name, summary := range summaries {
		if !summary.localisesWallClock() {
			if _, exempt := wallClockLocalisedForExpiry[name]; exempt {
				findings = append(findings, fmt.Sprintf("  %s (stale exemption: it no longer localises time.Now)\n      declared at %s", name, relativePosition(t, summary.position)))
			}
			continue
		}
		if _, exempt := wallClockLocalisedForExpiry[name]; exempt {
			continue
		}
		findings = append(findings, fmt.Sprintf("  %s\n      declared at %s", name, relativePosition(t, summary.position)))
	}
	for name := range wallClockLocalisedForExpiry {
		if _, declared := summaries[name]; !declared {
			findings = append(findings, fmt.Sprintf("  %s (stale exemption: no such declaration)", name))
		}
	}
	if len(findings) == 0 {
		return
	}
	sort.Strings(findings)
	t.Fatalf("%d function(s) in %s turn time.Now into a local time:\n%s\n"+
		"Read today from handler.clockNow(); a clock that is an expiry, not a day, goes in wallClockLocalisedForExpiry with its reason.",
		len(findings), apiPackagePath, strings.Join(findings, "\n"))
}

// TestWallClockBarrierRecognisesItsOwnFixtures anchors the classifier on source
// it owns, so the barrier cannot go green by resolving nothing in the tree.
func TestWallClockBarrierRecognisesItsOwnFixtures(t *testing.T) {
	const fixture = `package fixture

import "time"

type Handler struct{}

func (Handler) clockNow() time.Time { return time.Now() }

func (h Handler) localisesInOneExpression(loc *time.Location) time.Time { return time.Now().In(loc) }

func (h Handler) localisesInTwoSteps(loc *time.Location) time.Time {
	now := time.Now()
	return now.In(loc)
}

func (h Handler) localisesInAClosure(loc *time.Location) func() time.Time {
	return func() time.Time { return time.Now().In(loc) }
}

func (h Handler) localisesAFunctionValue(loc *time.Location) time.Time {
	read := time.Now
	return read().In(loc)
}

func (h Handler) readsTheHandlerClock(loc *time.Location) time.Time { return h.clockNow().In(loc) }

func (h Handler) expiresAToken() time.Time { return time.Now().Add(time.Minute) }
`
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "fixture.go", fixture, 0)
	if err != nil {
		t.Fatalf("parsing the fixture: %v", err)
	}
	info := &types.Info{Uses: map[*ast.Ident]types.Object{}, Defs: map[*ast.Ident]types.Object{}}
	pkg, err := (&types.Config{Importer: importer.Default()}).Check("fixture", fileSet, []*ast.File{file}, info)
	if err != nil {
		t.Fatalf("type-checking the fixture: %v", err)
	}

	summaries := summariseClockReads(fileSet, []*ast.File{file}, info, wallClockObjectsFor(t, pkg))
	for name, want := range map[string]bool{
		"Handler.localisesInOneExpression": true,
		"Handler.localisesInTwoSteps":      true,
		"Handler.localisesInAClosure":      true,
		"Handler.localisesAFunctionValue":  true,
		"Handler.readsTheHandlerClock":     false,
		"Handler.expiresAToken":            false,
		"Handler.clockNow":                 false,
	} {
		summary, ok := summaries[name]
		if !ok {
			t.Fatalf("the classifier found no declaration of %s", name)
		}
		if got := summary.localisesWallClock(); got != want {
			t.Errorf("%s: localisesWallClock=%v, want %v", name, got, want)
		}
	}
	if !summaries["Handler.readsTheHandlerClock"].readsHandlerClock {
		t.Errorf("the classifier did not see Handler.readsTheHandlerClock read clockNow")
	}
}

type wallClockObjects struct {
	wallNow    types.Object
	localise   types.Object
	handlerNow types.Object
}

type clockReadSummary struct {
	position          token.Position
	readsWallClock    bool
	localises         bool
	readsHandlerClock bool
}

func (summary clockReadSummary) localisesWallClock() bool {
	return summary.readsWallClock && summary.localises
}

// wallClockObjectsFor resolves time.Now, (time.Time).In and Handler.clockNow as
// the objects pkg itself sees.
func wallClockObjectsFor(t *testing.T, pkg *types.Package) wallClockObjects {
	t.Helper()

	var timePkg *types.Package
	for _, imported := range pkg.Imports() {
		if imported.Path() == "time" {
			timePkg = imported
		}
	}
	if timePkg == nil {
		t.Fatalf("%s imports no time package", pkg.Path())
	}
	objects := wallClockObjects{wallNow: timePkg.Scope().Lookup("Now")}
	if _, ok := objects.wallNow.(*types.Func); !ok {
		t.Fatalf("time.Now did not resolve to a function")
	}
	objects.localise = methodNamed(t, timePkg.Scope().Lookup("Time"), "In")
	objects.handlerNow = methodNamed(t, pkg.Scope().Lookup("Handler"), "clockNow")
	return objects
}

func methodNamed(t *testing.T, typeName types.Object, method string) types.Object {
	t.Helper()
	named, ok := typeName.Type().(*types.Named)
	if !ok {
		t.Fatalf("%s is not a named type", typeName.Name())
	}
	for index := range named.NumMethods() {
		if candidate := named.Method(index); candidate.Name() == method {
			return candidate
		}
	}
	t.Fatalf("%s declares no method %s", named.Obj().Name(), method)
	return nil
}

// summariseClockReads records, per function declaration with a body, which of
// the three clock objects its body references — closures included, since a
// closure is the declaring function's code.
func summariseClockReads(fileSet *token.FileSet, files []*ast.File, info *types.Info, objects wallClockObjects) map[string]clockReadSummary {
	summaries := map[string]clockReadSummary{}
	for _, file := range files {
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			defined, ok := info.Defs[function.Name].(*types.Func)
			if !ok {
				continue
			}
			summary := clockReadSummary{position: fileSet.Position(function.Pos())}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				identifier, ok := node.(*ast.Ident)
				if !ok {
					return true
				}
				switch info.Uses[identifier] {
				case objects.wallNow:
					summary.readsWallClock = true
				case objects.localise:
					summary.localises = true
				case objects.handlerNow:
					summary.readsHandlerClock = true
				}
				return true
			})
			name := declaredFunctionName(defined)
			if _, taken := summaries[name]; taken {
				// Several init functions share one name; none may overwrite another.
				name += " at " + summary.position.String()
			}
			summaries[name] = summary
		}
	}
	return summaries
}

func declaredFunctionName(function *types.Func) string {
	signature, ok := function.Type().(*types.Signature)
	if !ok || signature.Recv() == nil {
		return function.Name()
	}
	receiver := signature.Recv().Type()
	if pointer, ok := receiver.(*types.Pointer); ok {
		receiver = pointer.Elem()
	}
	if named, ok := receiver.(*types.Named); ok {
		return named.Obj().Name() + "." + function.Name()
	}
	return function.Name()
}
