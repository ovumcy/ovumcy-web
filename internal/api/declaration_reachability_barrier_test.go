package api

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"text/template/parse"

	"github.com/ovumcy/ovumcy-web/internal/templates"
	"golang.org/x/tools/go/packages"
)

// A view-model field nothing renders reads as LIVE to every analyser this
// repository runs. `deadcode` analyses functions only — types, fields and vars
// are out of its scope (the CI step says so in its own comment) — the compiler
// is satisfied by the assignment that writes the field, and a unit test that
// asserts the written value keeps both the compiler and the suite agreeing that
// the field matters. What the field does not have is a reader: no template
// interpolates it, no production Go selects it.
//
// The cost is not the memory. `buildCalendarDays` was a nine-branch precedence
// ladder emitting three parallel outputs per branch, two of which the calendar
// renders and one of which nothing did, so every change to calendar state
// semantics cost a reader the work of deciding which of the three mattered.
//
// This file is a tree barrier rather than a route regression, which is why it is
// its own file instead of an entry in an area aggregator: it parses the embedded
// templates and type-checks the shipped packages, and it reports on declarations
// rather than on responses.
//
// # How a template read is attributed to a type
//
// This is the whole difficulty, and getting it wrong once already cost a field.
// A Go template is dynamically typed: `{{.IsPeriod}}` carries no record of what
// it was selected on, and `IsPeriod` is rendered across the dashboard and the
// log summary on models.DailyLog. A sweep that collects field names tree-wide
// therefore waves through a CalendarDay.IsPeriod that nothing renders —
// measured, on the very field this barrier was written for, which is why the
// scoping below exists rather than a note saying to read for it by hand.
//
// So the reader set is resolved per type, in two tiers:
//
//   - BOUND. A page-data key whose value is (a slice of) a view model —
//     `"CalendarDays": days` in calendar_page_helpers.go, resolved through
//     go/types, never by spelling — binds that key to that type. The reader set
//     is then only the field selections inside the template block that ranges
//     over the key, with `$.`-rooted selections excluded because the root
//     context is not the ranged element. That is an exact answer for the shape
//     this package actually renders.
//   - UNBOUND. A view model with no such key falls back to matching by bare
//     name across every template, which is the weak tier. Falling back is a
//     declared decision — unboundViewModelTypes — never a silent one, because
//     a type that quietly slid into the weak tier is a barrier that reports
//     success while measuring almost nothing.
//
// It measures two things over the same loaded tree:
//
//   - TestEveryViewModelFieldIsReadByATemplateOrProductionGo — every exported
//     field of a view-model struct in handler_types.go.
//   - TestEveryTypeDeclaredInTheAPIPackageIsNamedByProductionGo — every
//     top-level type declaration in internal/api.
//
// Both refuse to pass on silence: a sweep that recognised no view model, bound
// a key to a range block it then read no field from, parsed no templates or
// resolved no selections fails loudly instead of reporting a clean tree it
// never looked at.

// viewModelSourceFile is the file whose structs are the field subject. It is
// deliberately one named file rather than "every struct in internal/api": the
// package also declares wire DTOs, whose readers are `encoding/json` and the
// schema decoder, and a barrier that could not tell the two apart would either
// exempt the view models or accuse the DTOs.
const viewModelSourceFile = "handler_types.go"

// modulePath is this module's import prefix; apiPackagePath is the package
// whose declarations this barrier judges.
const (
	modulePath     = "github.com/ovumcy/ovumcy-web"
	apiPackagePath = modulePath + "/internal/api"
)

// reflectivelyReadViewModelFields is the escape hatch for a field whose only
// reader is reflection — a struct tag consumer, a funcMap helper that ranges
// over fields, a marshaller. It is a map so every entry has to carry the reason
// it is exempt; it is empty because no such field exists today, and an entry
// added without a reader named in its value is not an exemption but a silence.
var reflectivelyReadViewModelFields = map[string]string{}

// typeDeclarationsOnlyTestsName is the same escape hatch for the type sweep: a
// production type declaration whose only consumers are tests, kept deliberately.
// Empty for the same reason.
var typeDeclarationsOnlyTestsName = map[string]string{}

// codecOwnedViewModelTypes names every struct in handler_types.go that is out
// of the field subject because a codec, not a template, reads it.
//
// Declared rather than inferred, because the exclusion is by TYPE: one
// `json:"…"` tag added to a view model takes all of its other fields out of the
// sweep in the same commit, and a barrier that dropped a type in silence would
// report a clean tree it had stopped looking at. Measured: with this check
// removed, tagging one CalendarDay field takes all twelve of its fields out of
// the subject and the barrier passes. So the list is compared against the tags
// actually found, in both directions — an undeclared exclusion fails, and so
// does an entry the file no longer excludes.
var codecOwnedViewModelTypes = map[string]string{
	"FlashPayload": "AEAD-sealed flash cookie payload; encoding/json reads it by tag and names no field in any syntax this barrier can see",
}

// unboundViewModelTypes names a view model the binding resolver could not tie
// to a page-data key, and which is therefore judged by the weak name-matching
// tier described in the doc comment above. Empty: every view model in the file
// binds today, and one that stops binding is a finding, not a fallback.
var unboundViewModelTypes = map[string]string{}

// treeEvidence is everything the barrier learned about the shipped tree.
type treeEvidence struct {
	// packages holds the type-checked production packages, tests excluded: a
	// field only a test selects is exactly the finding, so a sweep that loaded
	// the test variants would report every dead field as live.
	packages []*packages.Package

	// fieldSelections counts every resolved FieldVal selection across those
	// packages. It is the anti-vacuity floor for the Go half: a sweep that
	// loaded files but resolved nothing would call every field dead, which is
	// loud, and its mirror image — a sweep whose selection map came back empty
	// while the subject list was also empty — would be silently green.
	fieldSelections int

	// viewModelTypes are the structs of viewModelSourceFile the field sweep
	// judges; codecOwnedTypes are the ones it stood down from, which have to
	// match codecOwnedViewModelTypes exactly. A struct with no exported field
	// at all is in neither: a template cannot read an unexported field, so that
	// exclusion can hide nothing.
	viewModelTypes  []*types.Named
	codecOwnedTypes []string

	// bindings maps a page-data key to the view model it carries, resolved from
	// the type of the value in the fiber.Map literal rather than from the
	// spelling of the key.
	bindings map[string]*types.Named

	// templateFields holds every field name the embedded templates select,
	// whatever the owning type. It is the reader set for an UNBOUND view model
	// only, and it over-approximates badly — see the doc comment.
	templateFields map[string]bool

	// scopedTemplateFields holds, per bound page-data key, the field names
	// selected inside the template block that ranges over it. This is the
	// reader set for a bound view model, and it is exact for the shape the
	// calendar renders.
	scopedTemplateFields map[string]map[string]bool

	// boundRangeBlocks counts the template blocks the scoping actually entered,
	// so "the resolver bound a key but no template ranges over it" cannot pass
	// as "the type has no dead field".
	boundRangeBlocks int

	templateFiles int
	goFiles       int
}

var (
	treeEvidenceOnce   sync.Once
	treeEvidenceCached *treeEvidence
	treeEvidenceErr    error
)

// TestEveryViewModelFieldIsReadByATemplateOrProductionGo is the field barrier.
func TestEveryViewModelFieldIsReadByATemplateOrProductionGo(t *testing.T) {
	evidence := loadTreeEvidence(t)
	refuseUndeclaredTypeExclusions(t, evidence)

	subject := viewModelFields(t, evidence)

	var unread []string
	for _, field := range subject {
		if reason := reflectivelyReadViewModelFields[field.name]; reason != "" {
			continue
		}
		if field.templateReaders[field.name] {
			continue
		}
		if evidence.fieldIsSelectedInProduction(field.object) {
			continue
		}
		unread = append(unread, fmt.Sprintf("  %s.%s\n      declared at %s\n      template readers searched: %s",
			field.owner, field.name, field.position, field.readerScope))
	}
	if len(unread) == 0 {
		return
	}

	sort.Strings(unread)
	t.Fatalf("%d view-model field(s) no embedded template interpolates and no production Go selects:\n%s\n"+
		"Subject: %s. Excluded as codec-owned: %s.\n"+
		"A field with no reader is removed together with whatever computes it — the computation is the cost, not the field. "+
		"If a reader does exist and it is reflective, add the field to reflectivelyReadViewModelFields naming that reader.",
		len(unread), strings.Join(unread, "\n"),
		describeNamedTypes(evidence.viewModelTypes), describeStrings(evidence.codecOwnedTypes))
}

// TestEveryTypeDeclaredInTheAPIPackageIsNamedByProductionGo holds the package's
// top-level type declarations to a production consumer.
//
// The class this refuses is a production file that exists only to spell a name
// the tests prefer: a compatibility alias in the transport package obscures the
// service type that owns the shape, and every dead-code pass has to re-derive
// that its single reference lives in a `_test.go`.
func TestEveryTypeDeclaredInTheAPIPackageIsNamedByProductionGo(t *testing.T) {
	evidence := loadTreeEvidence(t)

	apiPkg := evidence.packageByPath(apiPackagePath)
	if apiPkg == nil {
		t.Fatalf("the sweep did not load %s; there is nothing to judge", apiPackagePath)
	}

	declared := declaredTypes(t, apiPkg)
	if len(declared) < 20 {
		t.Fatalf("the sweep recognised only %d top-level type declaration(s) in %s; it is measuring the wrong package", len(declared), apiPackagePath)
	}

	used := typeNamesUsedInProduction(evidence)
	if len(used) < 100 {
		t.Fatalf("the sweep resolved only %d type name use(s) across the shipped packages; it would call every declaration dead", len(used))
	}

	var orphaned []string
	for _, declaration := range declared {
		if reason := typeDeclarationsOnlyTestsName[declaration.name]; reason != "" {
			continue
		}
		if used[declaration.object] {
			continue
		}
		orphaned = append(orphaned, fmt.Sprintf("  %s\n      declared at %s", declaration.name, declaration.position))
	}
	if len(orphaned) == 0 {
		return
	}

	sort.Strings(orphaned)
	t.Fatalf("%d type(s) declared in production code that no production Go names:\n%s\n"+
		"A declaration only tests reach belongs in a _test.go, or the tests belong on the type that owns the shape.",
		len(orphaned), strings.Join(orphaned, "\n"))
}

// TestDeclarationBarrierRecognisesItsOwnFixtures anchors the barrier on inputs
// it owns rather than on the tree it judges. An anchor read off the live
// evidence stops firing the day that evidence empties, which is exactly the day
// the barrier would need it.
func TestDeclarationBarrierRecognisesItsOwnFixtures(t *testing.T) {
	const fixture = `{{.SentinelDirectField}}
{{if .SentinelGuardField}}x{{end}}
{{range .SentinelRows}}{{.SentinelNestedField}}{{end}}
{{with $row := .SentinelRows}}{{$row.SentinelVariableField}}{{end}}
{{template "icon" "heart"}}`

	evidence := &treeEvidence{
		templateFields:       map[string]bool{},
		scopedTemplateFields: map[string]map[string]bool{},
	}
	if err := collectTemplateFieldNames("fixture.html", fixture, nil, evidence); err != nil {
		t.Fatalf("parsing the fixture template: %v", err)
	}

	for _, want := range []string{
		"SentinelDirectField",
		"SentinelGuardField",
		"SentinelRows",
		"SentinelNestedField",
		"SentinelVariableField",
	} {
		if !evidence.templateFields[want] {
			t.Fatalf("the template collector missed %q; every field it misses reads as dead", want)
		}
	}
	// The negative half: a name the fixture never selects must not be
	// collected, or the barrier would treat every field as read.
	if evidence.templateFields["SentinelAbsentField"] {
		t.Fatalf("the template collector invented %q; a collector that over-collects makes the barrier green about nothing", "SentinelAbsentField")
	}
	// A quoted argument is a string, not a field selection.
	if evidence.templateFields["heart"] {
		t.Fatalf("the template collector read a string argument as a field selection")
	}
}

// TestDeclarationBarrierScopesAReadToTheBlockThatRangesOverItsBinding is the
// anchor for the precise tier, and it is the one that matters: the whole reason
// this barrier once missed a dead CalendarDay.IsPeriod is that a field name
// rendered on some other type looked like a read of this one.
//
// The fixture owns both halves — one selection inside the bound block that must
// be attributed to it, and four outside it (a plain action, the root context
// reached through `$.`, a sibling range's element, and the binding key itself)
// that must not be.
func TestDeclarationBarrierScopesAReadToTheBlockThatRangesOverItsBinding(t *testing.T) {
	const fixture = `{{.OutsideTheBlock}}
{{range .SentinelRows}}
  {{.InsideTheBlock}}
  {{if eq .InsideTheBlock $.RootReachedFromInside}}x{{end}}
{{end}}
{{range .OtherRows}}{{.InsideTheSiblingBlock}}{{end}}`

	evidence := &treeEvidence{
		templateFields:       map[string]bool{},
		scopedTemplateFields: map[string]map[string]bool{},
	}
	bindings := map[string]bool{"SentinelRows": true}
	if err := collectTemplateFieldNames("fixture.html", fixture, bindings, evidence); err != nil {
		t.Fatalf("parsing the fixture template: %v", err)
	}

	scoped := evidence.scopedTemplateFields["SentinelRows"]
	if !scoped["InsideTheBlock"] {
		t.Fatalf("the scoped collector missed a field selected inside the bound block; every field it misses reads as dead")
	}
	for _, mustNotLeak := range []string{
		// Selected before the block; attributing it would be the tree-wide
		// tier wearing the precise tier's name.
		"OutsideTheBlock",
		// `$.` is the root context, not the ranged element.
		"RootReachedFromInside",
		// A different range's element, which is the exact shape of the
		// CalendarDay.IsPeriod false positive.
		"InsideTheSiblingBlock",
		// The binding key itself is selected on the root.
		"OtherRows",
	} {
		if scoped[mustNotLeak] {
			t.Fatalf("the scoped collector attributed %q to the bound block; a scope that leaks is the tree-wide sweep with a narrower name", mustNotLeak)
		}
	}
	// The tree-wide set is still the union, since an unbound view model is
	// judged against it.
	for _, want := range []string{"OutsideTheBlock", "InsideTheBlock", "InsideTheSiblingBlock", "RootReachedFromInside"} {
		if !evidence.templateFields[want] {
			t.Fatalf("the tree-wide collector missed %q, which the unbound tier depends on", want)
		}
	}
}

// viewModelField is one exported field of a view-model struct, together with
// the reader set it was judged against.
type viewModelField struct {
	owner    string
	name     string
	object   *types.Var
	position string

	// templateReaders is the scoped set when the owner is bound and the
	// tree-wide set when it is not; readerScope says which, so a failure names
	// the tier it was decided in.
	templateReaders map[string]bool
	readerScope     string
}

// viewModelFields resolves the exported fields of every view model in
// handler_types.go, each carrying the reader set its owning type earned.
func viewModelFields(t *testing.T, evidence *treeEvidence) []viewModelField {
	t.Helper()

	apiPkg := evidence.packageByPath(apiPackagePath)
	if apiPkg == nil {
		t.Fatalf("the sweep did not load %s; there is nothing to judge", apiPackagePath)
	}

	// The floor measures whether a subject was recognised AT ALL — the file
	// moved, the parser broke, the type checker returned nothing — and
	// deliberately not whether the view model is large enough. A floor set just
	// under today's field count turns the next honest removal into a message
	// about a broken collector: measured at a subject of nine, where a `< 10`
	// floor answered "it is reading the wrong file" to three correctly retired
	// cells.
	if len(evidence.viewModelTypes) == 0 {
		t.Fatalf("the sweep recognised no view-model struct in %s; it is reading the wrong file, and a barrier with no subject passes about nothing", viewModelSourceFile)
	}

	var fields []viewModelField
	for _, named := range evidence.viewModelTypes {
		readers, scope := evidence.templateReadersFor(t, named)
		structType, ok := named.Underlying().(*types.Struct)
		if !ok {
			continue
		}
		for index := range structType.NumFields() {
			field := structType.Field(index)
			if !field.Exported() {
				continue
			}
			fields = append(fields, viewModelField{
				owner:           named.Obj().Name(),
				name:            field.Name(),
				object:          field,
				position:        relativePosition(t, apiPkg.Fset.Position(field.Pos())),
				templateReaders: readers,
				readerScope:     scope,
			})
		}
	}
	if len(fields) == 0 {
		t.Fatalf("the sweep recognised %d view-model struct(s) in %s but no exported field on any of them", len(evidence.viewModelTypes), viewModelSourceFile)
	}
	return fields
}

// templateReadersFor answers the reader set for one view model, and refuses the
// two ways the precise tier can go quiet: a type that silently fell back to
// name matching, and a binding whose range block yielded nothing.
func (evidence *treeEvidence) templateReadersFor(t *testing.T, named *types.Named) (map[string]bool, string) {
	t.Helper()

	name := named.Obj().Name()
	for key, bound := range evidence.bindings {
		if bound != named {
			continue
		}
		scoped := evidence.scopedTemplateFields[key]
		if len(scoped) == 0 {
			t.Fatalf("%s is bound to the page-data key %q and no embedded template ranges over it — "+
				"the reader set is empty, so every one of its fields would report as dead for a reason that is not about the fields",
				name, key)
		}
		return scoped, fmt.Sprintf("the {{range .%s}} block(s), %d name(s)", key, len(scoped))
	}

	if reason := unboundViewModelTypes[name]; reason != "" {
		return evidence.templateFields, fmt.Sprintf("every template by bare name, %d name(s) — DECLARED UNBOUND: %s", len(evidence.templateFields), reason)
	}
	t.Fatalf("%s is a view model no page-data key carries, so it could only be judged by matching field names across every template — "+
		"which passes any field whose name is rendered on some other type, and is how a dead CalendarDay.IsPeriod survived this barrier once. "+
		"Give it a binding, or declare it in unboundViewModelTypes with the reason the weak tier is acceptable for it.", name)
	return nil, ""
}

// refuseUndeclaredTypeExclusions holds the by-type exclusion to its allowlist in
// both directions, so a struct tag can never take a view model out of the sweep
// quietly and a stale entry can never keep claiming it did.
func refuseUndeclaredTypeExclusions(t *testing.T, evidence *treeEvidence) {
	t.Helper()

	found := map[string]bool{}
	for _, name := range evidence.codecOwnedTypes {
		found[name] = true
		if codecOwnedViewModelTypes[name] == "" {
			t.Fatalf("%s carries an encoding tag, so the whole type — and every one of its fields — was dropped from the field subject, "+
				"and codecOwnedViewModelTypes does not declare it. Add it with the codec that reads it, or take the tag off a view model.", name)
		}
	}
	for name := range codecOwnedViewModelTypes {
		if !found[name] {
			t.Fatalf("codecOwnedViewModelTypes declares %s, which %s no longer excludes; the entry is stale and now says something untrue about what the sweep skipped",
				name, viewModelSourceFile)
		}
	}
}

// declaredType is one top-level type declaration.
type declaredType struct {
	name     string
	object   types.Object
	position string
}

func declaredTypes(t *testing.T, apiPkg *packages.Package) []declaredType {
	t.Helper()

	var declarations []declaredType
	for _, file := range apiPkg.Syntax {
		for _, spec := range typeSpecsIn(file) {
			object := apiPkg.TypesInfo.Defs[spec.Name]
			if object == nil {
				t.Fatalf("the type checker did not resolve %s; an unresolved declaration is the case this barrier exists to decide", spec.Name.Name)
			}
			declarations = append(declarations, declaredType{
				name:     spec.Name.Name,
				object:   object,
				position: relativePosition(t, apiPkg.Fset.Position(spec.Name.Pos())),
			})
		}
	}
	return declarations
}

// typeNamesUsedInProduction is the set of type objects some production file
// names somewhere other than at the declaration itself.
//
// Uses rather than Defs: a declaration is not its own consumer. A method
// receiver is a use, which is what keeps a type carrying only methods out of
// the findings — a type reachable that way is reachable, and whether its
// methods are is `deadcode`'s question, not this one's.
func typeNamesUsedInProduction(evidence *treeEvidence) map[types.Object]bool {
	used := map[types.Object]bool{}
	for _, pkg := range evidence.packages {
		for identifier, object := range pkg.TypesInfo.Uses {
			if _, ok := object.(*types.TypeName); !ok {
				continue
			}
			if identifier == nil {
				continue
			}
			used[object] = true
		}
	}
	return used
}

// fieldIsSelectedInProduction answers whether some production file reads the
// field through a selector.
//
// Selections, not Uses: the type checker records the KEY of a composite-literal
// entry (`BadgeClass: badgeClass`) in Uses, and that is the WRITE this barrier
// exists to find the missing reader for. Counting it would make every field
// that is assigned read as live, which is every field there is.
func (evidence *treeEvidence) fieldIsSelectedInProduction(field *types.Var) bool {
	for _, pkg := range evidence.packages {
		for _, selection := range pkg.TypesInfo.Selections {
			if selection.Kind() != types.FieldVal {
				continue
			}
			if selection.Obj() == field {
				return true
			}
		}
	}
	return false
}

// packagesMissedBySweep names every package under internal/ that contributed no
// type-checked file. It is separate from the refusal that reads it so it can be
// measured in both directions; a refusal that only ever runs on a healthy tree
// proves nothing about the case it exists for.
func (evidence *treeEvidence) packagesMissedBySweep() []string {
	root, err := moduleRootForBarrier()
	if err != nil {
		return []string{"internal (the module root is unresolvable)"}
	}
	entries, err := os.ReadDir(filepath.Join(root, "internal"))
	if err != nil {
		return []string{"internal (unreadable)"}
	}

	loaded := map[string]bool{}
	for _, pkg := range evidence.packages {
		if len(pkg.Syntax) > 0 {
			loaded[pkg.PkgPath] = true
		}
	}

	var missed []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := "internal/" + entry.Name()
		if !loaded[modulePath+"/"+path] {
			missed = append(missed, path)
		}
	}
	sort.Strings(missed)
	return missed
}

func (evidence *treeEvidence) packageByPath(path string) *packages.Package {
	for _, pkg := range evidence.packages {
		if pkg.PkgPath == path {
			return pkg
		}
	}
	return nil
}

// loadTreeEvidence type-checks the shipped packages and parses the embedded
// templates, once per test binary.
func loadTreeEvidence(t *testing.T) *treeEvidence {
	t.Helper()

	treeEvidenceOnce.Do(func() {
		treeEvidenceCached, treeEvidenceErr = buildTreeEvidence()
	})
	if treeEvidenceErr != nil {
		t.Fatalf("the barrier could not read the tree, so nothing here is a clean bill: %v", treeEvidenceErr)
	}

	evidence := treeEvidenceCached
	if evidence.templateFiles < 30 {
		t.Fatalf("parsed only %d embedded template(s); the shipped set is far larger, so this sweep missed a directory", evidence.templateFiles)
	}
	if len(evidence.templateFields) < 100 {
		t.Fatalf("collected only %d template field selection(s); a sweep that parsed templates but collected nothing would call every field dead", len(evidence.templateFields))
	}
	// The load-bearing refusal is the structural one: a sweep that silently
	// skipped a package is the failure this barrier is least able to report on
	// itself, and every declaration that package reads would read as unread.
	// The magnitude floors around it carry slack and are the weaker half.
	if missed := evidence.packagesMissedBySweep(); len(missed) > 0 {
		t.Fatalf("the sweep type-checked no file in %s; every declaration those packages read would report as unread", strings.Join(missed, ", "))
	}
	if len(evidence.packages) < 14 {
		t.Fatalf("type-checked only %d package(s); the module is far larger, so this sweep measured the wrong tree", len(evidence.packages))
	}
	if evidence.goFiles < 200 {
		t.Fatalf("type-checked only %d production Go file(s); the module is far larger", evidence.goFiles)
	}
	if evidence.fieldSelections < 1000 {
		t.Fatalf("resolved only %d field selection(s) across the tree; the type information is missing and every field would read as unread", evidence.fieldSelections)
	}
	// A binding the templates never range over would leave the precise tier
	// empty, which templateReadersFor turns into a per-type refusal; this is
	// the same refusal one level up, for the case where the scoping walk never
	// entered a single bound block.
	if len(evidence.bindings) > 0 && evidence.boundRangeBlocks == 0 {
		t.Fatalf("%d page-data key(s) bind to a view model and no embedded template ranges over any of them; the precise reader tier saw nothing", len(evidence.bindings))
	}
	return evidence
}

func buildTreeEvidence() (*treeEvidence, error) {
	root, err := moduleRootForBarrier()
	if err != nil {
		return nil, err
	}

	config := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo |
			packages.NeedImports | packages.NeedDeps,
		Dir: root,
		// Tests excluded on purpose: this measures what the APPLICATION
		// reaches, and a declaration only a test reaches is the finding.
		Tests: false,
	}
	// The same five package trees the repository's other Go steps scope
	// themselves to; ./... would sweep node_modules in a checkout that has one.
	loaded, err := packages.Load(config, "./cmd/...", "./internal/...", "./migrations/...", "./scripts/...", "./web/...")
	if err != nil {
		return nil, fmt.Errorf("type-checking the shipped packages: %w", err)
	}

	var loadErrors []string
	for _, pkg := range loaded {
		for _, packageError := range pkg.Errors {
			loadErrors = append(loadErrors, pkg.PkgPath+": "+packageError.Error())
		}
	}
	if len(loadErrors) > 0 {
		// Fail closed. A tree that does not type-check leaves every selector
		// unresolved, which is exactly the evidence this barrier reads.
		return nil, fmt.Errorf("the tree does not type-check, so no field reader could be identified:\n  %s", strings.Join(loadErrors, "\n  "))
	}

	evidence := &treeEvidence{
		packages:             loaded,
		bindings:             map[string]*types.Named{},
		templateFields:       map[string]bool{},
		scopedTemplateFields: map[string]map[string]bool{},
	}
	for _, pkg := range loaded {
		evidence.goFiles += len(pkg.Syntax)
		for _, selection := range pkg.TypesInfo.Selections {
			if selection.Kind() == types.FieldVal {
				evidence.fieldSelections++
			}
		}
	}

	if err := collectViewModelTypes(evidence); err != nil {
		return nil, err
	}
	if err := resolveViewModelBindings(evidence); err != nil {
		return nil, err
	}
	if err := collectEmbeddedTemplateFields(evidence); err != nil {
		return nil, err
	}
	return evidence, nil
}

// collectViewModelTypes splits the structs of viewModelSourceFile into the ones
// the field sweep judges and the ones a codec owns.
func collectViewModelTypes(evidence *treeEvidence) error {
	apiPkg := evidence.packageByPath(apiPackagePath)
	if apiPkg == nil {
		return fmt.Errorf("the sweep did not load %s", apiPackagePath)
	}
	file := fileNamed(apiPkg, viewModelSourceFile)
	if file == nil {
		return fmt.Errorf("%s declares no file named %s; the barrier's subject moved and it is now measuring nothing", apiPackagePath, viewModelSourceFile)
	}

	for _, spec := range typeSpecsIn(file) {
		named, structType := namedStruct(apiPkg, spec)
		if named == nil || structType == nil {
			continue
		}
		if !hasExportedField(structType) {
			// A template can only read an exported field, so excluding this
			// struct cannot hide one. Handler is the whole of this case.
			continue
		}
		if structCarriesEncodingTags(structType) {
			evidence.codecOwnedTypes = append(evidence.codecOwnedTypes, named.Obj().Name())
			continue
		}
		evidence.viewModelTypes = append(evidence.viewModelTypes, named)
	}
	sort.Strings(evidence.codecOwnedTypes)
	return nil
}

// resolveViewModelBindings ties a page-data key to the view model it carries.
//
// The evidence is the TYPE of the value in the map literal, never the spelling
// of the key: `"CalendarDays": days` binds because days is []CalendarDay, and a
// key renamed tomorrow rebinds itself. A key two literals give two different
// view models is a refusal, not a guess — an ambiguous binding would attribute
// one type's template reads to another.
func resolveViewModelBindings(evidence *treeEvidence) error {
	subject := map[*types.Named]bool{}
	for _, named := range evidence.viewModelTypes {
		subject[named] = true
	}
	if len(subject) == 0 {
		return nil
	}

	conflicts := map[string]map[string]bool{}
	for _, pkg := range evidence.packages {
		for _, file := range pkg.Syntax {
			ast.Inspect(file, func(node ast.Node) bool {
				composite, ok := node.(*ast.CompositeLit)
				if !ok || composite.Type == nil {
					return true
				}
				literalType := pkg.TypesInfo.TypeOf(composite.Type)
				if literalType == nil {
					return true
				}
				if _, isMap := literalType.Underlying().(*types.Map); !isMap {
					return true
				}
				for _, element := range composite.Elts {
					pair, ok := element.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					key, ok := stringLiteralValue(pair.Key)
					if !ok {
						continue
					}
					named := namedStructBehind(pkg.TypesInfo.TypeOf(pair.Value))
					if named == nil || !subject[named] {
						continue
					}
					if existing, seen := evidence.bindings[key]; seen && existing != named {
						if conflicts[key] == nil {
							conflicts[key] = map[string]bool{existing.Obj().Name(): true}
						}
						conflicts[key][named.Obj().Name()] = true
					}
					evidence.bindings[key] = named
				}
				return true
			})
		}
	}

	if len(conflicts) > 0 {
		var described []string
		for key, names := range conflicts {
			var sorted []string
			for name := range names {
				sorted = append(sorted, name)
			}
			sort.Strings(sorted)
			described = append(described, fmt.Sprintf("%q carries %s", key, strings.Join(sorted, " and ")))
		}
		sort.Strings(described)
		return fmt.Errorf("a page-data key binds to more than one view model (%s); the scoped reader set would attribute one type's template reads to the other", strings.Join(described, "; "))
	}
	return nil
}

// collectEmbeddedTemplateFields parses the templates the binary embeds, not the
// ones on disk: a template that exists but is not embedded never renders.
func collectEmbeddedTemplateFields(evidence *treeEvidence) error {
	bindingKeys := map[string]bool{}
	for key := range evidence.bindings {
		bindingKeys[key] = true
	}

	return fs.WalkDir(templates.Files, ".", func(path string, entry fs.DirEntry, walkErr error) error {
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
		if err := collectTemplateFieldNames(path, string(source), bindingKeys, evidence); err != nil {
			return err
		}
		evidence.templateFiles++
		return nil
	})
}

// collectTemplateFieldNames records the field names one template selects, into
// the tree-wide set and — inside a block that ranges over a bound key — into
// that key's own set.
//
// Parsing runs with SkipFuncCheck so the sweep needs no copy of the func map;
// the func map has its own barrier one layer up.
func collectTemplateFieldNames(path string, source string, bindingKeys map[string]bool, evidence *treeEvidence) error {
	trees := map[string]*parse.Tree{}
	tree := parse.New(path)
	tree.Mode = parse.SkipFuncCheck
	if _, err := tree.Parse(source, "", "", trees); err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}

	collectTemplateFieldNamesFromNode(tree.Root, "", bindingKeys, evidence)
	for _, associated := range trees {
		if associated.Root != nil {
			collectTemplateFieldNamesFromNode(associated.Root, "", bindingKeys, evidence)
		}
	}
	return nil
}

// collectTemplateFieldNamesFromNode walks one node under a scope.
//
// scope is the bound page-data key whose element is the current dot, or "" at
// the root. A nested range inside a bound block keeps the outer scope, so the
// inner element's fields are attributed to the outer type: an over-attribution,
// which is the safe direction for a finding that reads "nothing renders this",
// and the calendar grid has no nested range today.
func collectTemplateFieldNamesFromNode(node parse.Node, scope string, bindingKeys map[string]bool, evidence *treeEvidence) {
	switch typed := node.(type) {
	case nil:
		return
	case *parse.ListNode:
		if typed == nil {
			return
		}
		for _, child := range typed.Nodes {
			collectTemplateFieldNamesFromNode(child, scope, bindingKeys, evidence)
		}
	case *parse.ActionNode:
		collectTemplateFieldNamesFromNode(typed.Pipe, scope, bindingKeys, evidence)
	case *parse.PipeNode:
		if typed == nil {
			return
		}
		for _, command := range typed.Cmds {
			collectTemplateFieldNamesFromNode(command, scope, bindingKeys, evidence)
		}
	case *parse.CommandNode:
		for _, argument := range typed.Args {
			collectTemplateFieldNamesFromNode(argument, scope, bindingKeys, evidence)
		}
	case *parse.FieldNode:
		// `.Foo.Bar` — every element is a field selection, and inside a bound
		// block the dot IS the ranged element.
		evidence.recordFieldNames(typed.Ident, scope)
	case *parse.ChainNode:
		// `(pipeline).Foo` — the head is a pipeline, the tail field names.
		collectTemplateFieldNamesFromNode(typed.Node, scope, bindingKeys, evidence)
		evidence.recordFieldNames(typed.Field, scope)
	case *parse.VariableNode:
		// `$row.Foo` — Ident[0] is the variable, the rest are fields. `$.Foo`
		// reaches the ROOT context, never the ranged element, so it is recorded
		// tree-wide only: attributing it would put every page-data key on the
		// element's reader set and hand back the tree-wide tier under a
		// narrower name.
		elementScope := scope
		if len(typed.Ident) > 0 && typed.Ident[0] == "$" {
			elementScope = ""
		}
		evidence.recordFieldNames(typed.Ident[1:], elementScope)
	case *parse.IfNode:
		collectTemplateFieldNamesFromBranch(&typed.BranchNode, scope, bindingKeys, evidence)
	case *parse.WithNode:
		collectTemplateFieldNamesFromBranch(&typed.BranchNode, scope, bindingKeys, evidence)
	case *parse.RangeNode:
		key := boundRangeKey(typed.Pipe, bindingKeys)
		// The pipe itself is evaluated in the OUTER scope — `.CalendarDays` is
		// selected on the page data, not on a CalendarDay.
		collectTemplateFieldNamesFromNode(typed.Pipe, scope, bindingKeys, evidence)
		bodyScope := scope
		if key != "" {
			bodyScope = key
			evidence.boundRangeBlocks++
			if evidence.scopedTemplateFields[key] == nil {
				evidence.scopedTemplateFields[key] = map[string]bool{}
			}
		}
		collectTemplateFieldNamesFromNode(typed.List, bodyScope, bindingKeys, evidence)
		collectTemplateFieldNamesFromNode(typed.ElseList, scope, bindingKeys, evidence)
	case *parse.TemplateNode:
		collectTemplateFieldNamesFromNode(typed.Pipe, scope, bindingKeys, evidence)
	}
}

func collectTemplateFieldNamesFromBranch(branch *parse.BranchNode, scope string, bindingKeys map[string]bool, evidence *treeEvidence) {
	collectTemplateFieldNamesFromNode(branch.Pipe, scope, bindingKeys, evidence)
	collectTemplateFieldNamesFromNode(branch.List, scope, bindingKeys, evidence)
	collectTemplateFieldNamesFromNode(branch.ElseList, scope, bindingKeys, evidence)
}

func (evidence *treeEvidence) recordFieldNames(names []string, scope string) {
	for _, name := range names {
		evidence.templateFields[name] = true
		if scope == "" {
			continue
		}
		if evidence.scopedTemplateFields[scope] == nil {
			evidence.scopedTemplateFields[scope] = map[string]bool{}
		}
		evidence.scopedTemplateFields[scope][name] = true
	}
}

// boundRangeKey answers the bound page-data key a range iterates, or "".
//
// Only the two spellings the templates use are recognised — `{{range .Key}}`
// and `{{range $x := $.Key}}` — because a shape it fails to recognise leaves
// the block unscoped, and an unscoped block is judged by the weak tier rather
// than by a wrong one.
func boundRangeKey(pipe *parse.PipeNode, bindingKeys map[string]bool) string {
	if pipe == nil || len(pipe.Cmds) != 1 || len(pipe.Cmds[0].Args) != 1 {
		return ""
	}
	switch argument := pipe.Cmds[0].Args[0].(type) {
	case *parse.FieldNode:
		if len(argument.Ident) == 1 && bindingKeys[argument.Ident[0]] {
			return argument.Ident[0]
		}
	case *parse.VariableNode:
		if len(argument.Ident) == 2 && argument.Ident[0] == "$" && bindingKeys[argument.Ident[1]] {
			return argument.Ident[1]
		}
	}
	return ""
}

func typeSpecsIn(file *ast.File) []*ast.TypeSpec {
	var specs []*ast.TypeSpec
	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.TYPE {
			continue
		}
		for _, spec := range general.Specs {
			if typeSpec, ok := spec.(*ast.TypeSpec); ok {
				specs = append(specs, typeSpec)
			}
		}
	}
	return specs
}

func namedStruct(pkg *packages.Package, spec *ast.TypeSpec) (*types.Named, *types.Struct) {
	object := pkg.TypesInfo.Defs[spec.Name]
	if object == nil {
		return nil, nil
	}
	named, ok := object.Type().(*types.Named)
	if !ok {
		return nil, nil
	}
	structType, ok := named.Underlying().(*types.Struct)
	if !ok {
		return nil, nil
	}
	return named, structType
}

// namedStructBehind unwraps a slice, array or pointer to the named struct a
// page-data value carries, so `[]CalendarDay` and `*CalendarDay` both bind.
func namedStructBehind(carried types.Type) *types.Named {
	for range 4 {
		if carried == nil {
			return nil
		}
		switch shape := carried.(type) {
		case *types.Slice:
			carried = shape.Elem()
		case *types.Array:
			carried = shape.Elem()
		case *types.Pointer:
			carried = shape.Elem()
		case *types.Named:
			if _, ok := shape.Underlying().(*types.Struct); ok {
				return shape
			}
			return nil
		default:
			return nil
		}
	}
	return nil
}

func stringLiteralValue(expression ast.Expr) (string, bool) {
	literal, ok := expression.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", false
	}
	if len(literal.Value) < 2 {
		return "", false
	}
	return literal.Value[1 : len(literal.Value)-1], true
}

func hasExportedField(structType *types.Struct) bool {
	for index := range structType.NumFields() {
		if structType.Field(index).Exported() {
			return true
		}
	}
	return false
}

// structCarriesEncodingTags reports a struct whose reader is a codec rather
// than a template or a selector.
func structCarriesEncodingTags(structType *types.Struct) bool {
	for index := range structType.NumFields() {
		tag := structType.Tag(index)
		for _, key := range []string{"json:", "form:", "query:", "xml:"} {
			if strings.Contains(tag, key) {
				return true
			}
		}
	}
	return false
}

func fileNamed(pkg *packages.Package, base string) *ast.File {
	for _, file := range pkg.Syntax {
		if filepath.Base(pkg.Fset.Position(file.Pos()).Filename) == base {
			return file
		}
	}
	return nil
}

func describeNamedTypes(named []*types.Named) string {
	if len(named) == 0 {
		return "(none)"
	}
	var names []string
	for _, entry := range named {
		names = append(names, entry.Obj().Name())
	}
	return describeStrings(names)
}

func describeStrings(values []string) string {
	if len(values) == 0 {
		return "(none)"
	}
	sorted := append([]string(nil), values...)
	sort.Strings(sorted)
	return strings.Join(sorted, ", ")
}

func relativePosition(t *testing.T, position token.Position) string {
	t.Helper()

	root, err := moduleRootForBarrier()
	if err != nil {
		return position.String()
	}
	relative, err := filepath.Rel(root, position.Filename)
	if err != nil {
		return position.String()
	}
	return fmt.Sprintf("%s:%d", filepath.ToSlash(relative), position.Line)
}

// moduleRootForBarrier walks up from the package directory to the module root.
//
// A test binary runs with its own package as the working directory, so the
// sweep has to find the tree it is meant to measure. Failing to find it is an
// error rather than a fallback to ".": a sweep rooted at the wrong directory
// loads a handful of packages and reports every declaration dead.
func moduleRootForBarrier() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolving the working directory: %w", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod above %s; the sweep would measure nothing", dir)
		}
		dir = parent
	}
}
