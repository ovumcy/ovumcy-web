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
// It measures two things over the same loaded tree:
//
//   - TestEveryViewModelFieldIsReadByATemplateOrProductionGo — every exported
//     field of a view-model struct in handler_types.go.
//   - TestEveryTypeDeclaredInTheAPIPackageIsNamedByProductionGo — every
//     top-level type declaration in internal/api.
//
// Both refuse to pass on silence: a sweep that recognised no fields, parsed no
// templates or resolved no selections fails loudly instead of reporting a clean
// tree it never looked at.

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

	// templateFields holds every field name the embedded templates select, by
	// name and not by owning type. Go templates are dynamically typed, so the
	// owner of `.CellClass` is not recoverable from the parse tree; the sweep
	// therefore over-approximates, which can only make a dead field read as
	// live (a false green on a name collision), never make a live field read as
	// dead. For a barrier whose finding is "nothing reads this", that is the
	// safe direction.
	//
	// The residual is a real one and it has already bitten: `CalendarDay` used
	// to carry an `IsPeriod` the calendar grid never interpolated, and this
	// sweep called it live because `models.DailyLog.IsPeriod` is rendered by
	// the dashboard and the log summary. So the barrier is a floor, not a
	// proof: it catches every field whose name is unique, and a name shared
	// with a live field elsewhere still has to be read for by hand.
	templateFields map[string]bool

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

	subject := viewModelFields(t, evidence)
	if len(subject) < 10 {
		t.Fatalf("the sweep recognised only %d view-model field(s) in %s; it is reading the wrong file, and a barrier with no subject passes about nothing", len(subject), viewModelSourceFile)
	}

	var unread []string
	for _, field := range subject {
		if reason := reflectivelyReadViewModelFields[field.name]; reason != "" {
			continue
		}
		if evidence.templateFields[field.name] {
			continue
		}
		if evidence.fieldIsSelectedInProduction(field.object) {
			continue
		}
		unread = append(unread, fmt.Sprintf("  %s.%s\n      declared at %s", field.owner, field.name, field.position))
	}
	if len(unread) == 0 {
		return
	}

	sort.Strings(unread)
	t.Fatalf("%d view-model field(s) no embedded template interpolates and no production Go selects:\n%s\n"+
		"A field with no reader is removed together with whatever computes it — the computation is the cost, not the field. "+
		"If a reader does exist and it is reflective, add the field to reflectivelyReadViewModelFields naming that reader.",
		len(unread), strings.Join(unread, "\n"))
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

	collected := map[string]bool{}
	if err := collectTemplateFieldNames("fixture.html", fixture, collected); err != nil {
		t.Fatalf("parsing the fixture template: %v", err)
	}

	for _, want := range []string{
		"SentinelDirectField",
		"SentinelGuardField",
		"SentinelRows",
		"SentinelNestedField",
		"SentinelVariableField",
	} {
		if !collected[want] {
			t.Fatalf("the template collector missed %q; every field it misses reads as dead", want)
		}
	}
	// The negative half: a name the fixture never selects must not be
	// collected, or the barrier would treat every field as read.
	if collected["SentinelAbsentField"] {
		t.Fatalf("the template collector invented %q; a collector that over-collects makes the barrier green about nothing", "SentinelAbsentField")
	}
	// A quoted argument is a string, not a field selection.
	if collected["heart"] {
		t.Fatalf("the template collector read a string argument as a field selection")
	}
}

// viewModelField is one exported field of a view-model struct.
type viewModelField struct {
	owner    string
	name     string
	object   *types.Var
	position string
}

// viewModelFields resolves the exported fields of every struct declared in
// handler_types.go that is a view model.
//
// A struct whose fields carry `json` tags is not one: its reader is the
// marshaller, which names no field in any syntax this barrier can see. That is
// the reflection case the escape hatch exists for, applied by construction to a
// whole type rather than field by field.
func viewModelFields(t *testing.T, evidence *treeEvidence) []viewModelField {
	t.Helper()

	apiPkg := evidence.packageByPath(apiPackagePath)
	if apiPkg == nil {
		t.Fatalf("the sweep did not load %s; there is nothing to judge", apiPackagePath)
	}

	file := fileNamed(apiPkg, viewModelSourceFile)
	if file == nil {
		t.Fatalf("%s declares no file named %s; the barrier's subject moved and it is now measuring nothing", apiPackagePath, viewModelSourceFile)
	}

	var fields []viewModelField
	for _, spec := range typeSpecsIn(file) {
		named, structType := namedStruct(apiPkg, spec)
		if named == nil || structType == nil {
			continue
		}
		if structCarriesEncodingTags(structType) {
			continue
		}
		for index := range structType.NumFields() {
			field := structType.Field(index)
			if !field.Exported() {
				continue
			}
			fields = append(fields, viewModelField{
				owner:    named.Obj().Name(),
				name:     field.Name(),
				object:   field,
				position: relativePosition(t, apiPkg.Fset.Position(field.Pos())),
			})
		}
	}
	return fields
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
		packages:       loaded,
		templateFields: map[string]bool{},
	}
	for _, pkg := range loaded {
		evidence.goFiles += len(pkg.Syntax)
		for _, selection := range pkg.TypesInfo.Selections {
			if selection.Kind() == types.FieldVal {
				evidence.fieldSelections++
			}
		}
	}

	if err := collectEmbeddedTemplateFields(evidence); err != nil {
		return nil, err
	}
	return evidence, nil
}

// collectEmbeddedTemplateFields parses the templates the binary embeds, not the
// ones on disk: a template that exists but is not embedded never renders.
func collectEmbeddedTemplateFields(evidence *treeEvidence) error {
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
		if err := collectTemplateFieldNames(path, string(source), evidence.templateFields); err != nil {
			return err
		}
		evidence.templateFiles++
		return nil
	})
}

// collectTemplateFieldNames records every field name one template selects.
//
// Parsing runs with SkipFuncCheck so the sweep needs no copy of the func map;
// the func map has its own barrier one layer up.
func collectTemplateFieldNames(path string, source string, into map[string]bool) error {
	trees := map[string]*parse.Tree{}
	tree := parse.New(path)
	tree.Mode = parse.SkipFuncCheck
	if _, err := tree.Parse(source, "", "", trees); err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}

	collectTemplateFieldNamesFromNode(tree.Root, into)
	for _, associated := range trees {
		if associated.Root != nil {
			collectTemplateFieldNamesFromNode(associated.Root, into)
		}
	}
	return nil
}

func collectTemplateFieldNamesFromNode(node parse.Node, into map[string]bool) {
	switch typed := node.(type) {
	case nil:
		return
	case *parse.ListNode:
		if typed == nil {
			return
		}
		for _, child := range typed.Nodes {
			collectTemplateFieldNamesFromNode(child, into)
		}
	case *parse.ActionNode:
		collectTemplateFieldNamesFromNode(typed.Pipe, into)
	case *parse.PipeNode:
		if typed == nil {
			return
		}
		for _, command := range typed.Cmds {
			collectTemplateFieldNamesFromNode(command, into)
		}
	case *parse.CommandNode:
		for _, argument := range typed.Args {
			collectTemplateFieldNamesFromNode(argument, into)
		}
	case *parse.FieldNode:
		// `.Foo.Bar` — every element is a field selection.
		for _, identifier := range typed.Ident {
			into[identifier] = true
		}
	case *parse.ChainNode:
		// `(pipeline).Foo` — the head is a pipeline, the tail field names.
		collectTemplateFieldNamesFromNode(typed.Node, into)
		for _, identifier := range typed.Field {
			into[identifier] = true
		}
	case *parse.VariableNode:
		// `$row.Foo` — Ident[0] is the variable, the rest are fields.
		for _, identifier := range typed.Ident[1:] {
			into[identifier] = true
		}
	case *parse.IfNode:
		collectTemplateFieldNamesFromBranch(&typed.BranchNode, into)
	case *parse.RangeNode:
		collectTemplateFieldNamesFromBranch(&typed.BranchNode, into)
	case *parse.WithNode:
		collectTemplateFieldNamesFromBranch(&typed.BranchNode, into)
	case *parse.TemplateNode:
		collectTemplateFieldNamesFromNode(typed.Pipe, into)
	}
}

func collectTemplateFieldNamesFromBranch(branch *parse.BranchNode, into map[string]bool) {
	collectTemplateFieldNamesFromNode(branch.Pipe, into)
	collectTemplateFieldNamesFromNode(branch.List, into)
	collectTemplateFieldNamesFromNode(branch.ElseList, into)
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
