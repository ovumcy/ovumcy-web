package i18n_test

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"text/template/parse"

	"github.com/ovumcy/ovumcy-web/internal/i18n"
	"github.com/ovumcy/ovumcy-web/internal/templates"
)

// A locale key the application can no longer reach is dead weight that reads as
// LIVE to every analyser this repository runs: `deadcode` has no opinion about a
// JSON catalogue, and the key-parity test is satisfied as long as all six
// catalogues carry it. It survives translation waves, costs six edits whenever
// its neighbours move, and hides the fact that whatever used to render it is
// gone too.
//
// This is the bottom of the cascade the templateFuncMap barrier guards the top
// of (`internal/api/handlers_template_funcmap_test.go`): a func-map entry no
// template calls keeps a services helper reachable, which keeps the keys that
// helper names reachable. Removing the top layer is what makes this layer
// measurable at all.
//
// The sweep collects EVERY string literal in shipped Go and in shipped
// templates and asks whether the catalogue contains it. It deliberately does
// not recognise keys by shape. A dotted-identifier pattern — the obvious
// spelling — called `cycle start replace required` and its sibling dead on
// 2026-08-15: they are ordinary catalogue keys that happen to contain spaces
// (`internal/api/error_mapping_days.go`), and a pattern keyed on the spelling of
// a key is not keyed on keys.

// verbPattern matches a single printf verb. A literal carrying exactly one is a
// candidate key family: the application builds the real key at runtime, so the
// members must be computed from whatever enumerates them, never guessed.
var verbPattern = regexp.MustCompile(`%[a-zA-Z]`)

// keyFamily binds a format literal found in the sources to the declaration that
// enumerates its members. Exactly one source per family must be set.
//
// The point of the registry is that it cannot go quiet: a format literal that
// matches catalogue keys and has no entry here fails the barrier by name. A
// family whose members were merely pattern-matched would accept `lang.zz` for a
// language the build does not ship, which is the false negative this whole test
// exists to refuse.
type keyFamily struct {
	format string

	// exactly one of the following names the source of truth
	fromSupportedLanguages bool
	fromStringVar          string
	fromFuncReturns        string
}

var localeKeyFamilies = []keyFamily{
	// internal/api/language_switch_view_models.go builds `lang.%s` from the
	// codes the manager reports, which are the embedded catalogue filenames.
	{format: "lang.%s", fromSupportedLanguages: true},
	// internal/templates/stats.html builds both of these with `printf`.
	{format: "stats.factor_pattern_%s", fromStringVar: "statsCycleFactorPatternOrder"},
	{format: "stats.factor_cycle_kind_%s", fromFuncReturns: "classifyStatsCycleFactorComparison"},
}

// sourceEvidence is everything the sweep learned from the shipped sources.
type sourceEvidence struct {
	// literals holds every string literal, whatever its role. Membership in the
	// catalogue is what makes one a key — not its shape.
	literals map[string]bool
	// stringVars and funcReturns are the enumerators a key family resolves
	// from, indexed by declaration name; declCounts guards against two
	// packages declaring the same name, where a resolver could not tell which
	// one it read.
	stringVars  map[string][]string
	funcReturns map[string][]string
	declCounts  map[string]int

	// goDirs records which directories actually contributed a parsed file, so
	// the sweep can be held to visiting every package rather than merely to a
	// plausible file count.
	goDirs map[string]bool

	// templateKeyCallSites records every `t`/`tn` command the shipped
	// templates contain, which is what the reverse barrier
	// (locale_template_call_sites_test.go) measures: literals answer "is this
	// catalogue key still named anywhere", call sites answer "does the key
	// this template renders exist at all".
	templateKeyCallSites []templateKeyCallSite

	// goFormatSites records every `fmt.Sprintf` in shipped Go whose format
	// string is computed rather than written as a literal, and goFormatBodies
	// the surrounding evidence each one needs. That is what the verb barrier
	// (locale_format_verb_parity_test.go) measures: a catalogue value used as a
	// printf pattern is a contract between six JSON files and a Go argument
	// list, and nothing else in the build reads both ends.
	goFormatSites  []goFormatSite
	goFormatBodies map[string]goFormatBody

	// pendingTemplateTextSpans holds, for the file currently being parsed, the
	// source regions the parser attributed to literal text rather than to a
	// template action. templateLiteralCopySites is what the literal-copy
	// barrier (locale_template_literal_copy_test.go) derives from them: those
	// spans are the only part of a template no `t` call can ever have reached,
	// so they are where untranslated copy hides. The counters beside them are
	// the barrier's proof that its reader still reads.
	pendingTemplateTextSpans      []templateTextSpan
	templateLiteralCopySites      []templateLiteralCopySite
	templateHumanReadableAttrs    int
	templateMarkupElementsScanned int
	// reviewedSymbolsSeen records which entries of
	// reviewedLanguageIndependentText a text node actually carried, so the
	// text barrier can refuse an entry that matches nothing on the same
	// grounds the attribute barrier already refuses a stale exemption.
	reviewedSymbolsSeen map[string]bool
	// goKeyOriginLiterals holds the literals found at the call arguments a
	// format contract points at when the site itself does not name its key,
	// and goFunctionDeclarations counts declarations per name so two functions
	// of one name in one file cannot merge into a single bucket unnoticed.
	goKeyOriginLiterals    []goKeyOriginLiteral
	goFunctionDeclarations map[string]int

	goFiles       int
	templateFiles int
}

func newSourceEvidence() *sourceEvidence {
	return &sourceEvidence{
		literals:               map[string]bool{},
		stringVars:             map[string][]string{},
		funcReturns:            map[string][]string{},
		declCounts:             map[string]int{},
		goDirs:                 map[string]bool{},
		goFormatBodies:         map[string]goFormatBody{},
		goFunctionDeclarations: map[string]int{},
		reviewedSymbolsSeen:    map[string]bool{},
	}
}

// TestEveryLocaleKeyIsReachableFromTheApplication is the barrier.
func TestEveryLocaleKeyIsReachableFromTheApplication(t *testing.T) {
	root := moduleRoot(t)

	evidence := newSourceEvidence()
	if err := collectFromGoTree(root, evidence); err != nil {
		t.Fatalf("sweeping the Go sources: %v", err)
	}
	if err := collectFromShippedTemplates(evidence); err != nil {
		t.Fatalf("sweeping the shipped templates: %v", err)
	}
	refuseAnUnderfedSweep(t, root, evidence)

	manager, err := i18n.NewManager(i18n.LangEN)
	if err != nil {
		t.Fatalf("building the locale manager: %v", err)
	}
	catalogue := everyShippedKey(manager)
	if len(catalogue) < 700 {
		t.Fatalf("the catalogue reports only %d keys; the sweep is measuring the wrong files", len(catalogue))
	}

	reachable := resolveReachableKeys(t, manager, evidence, catalogue)

	var unreachable []string
	for key := range catalogue {
		if !reachable[key] {
			unreachable = append(unreachable, key)
		}
	}
	sort.Strings(unreachable)
	if len(unreachable) == 0 {
		return
	}

	english := manager.Messages(i18n.LangEN)
	t.Fatalf("%d locale key(s) no shipped Go file or template can reach:\n%s\n"+
		"A key nothing reaches is removed from all six catalogues at once. Before removing one, "+
		"read the test-side references listed above it: a spec that re-typed the English sentence "+
		"pins the string without ever naming the key, and deleting it turns a green suite red for "+
		"a reason that reads as a product regression.",
		len(unreachable), describeUnreachable(root, unreachable, english))
}

// moduleRoot walks up from the package directory to the module root.
//
// A test binary runs with its own package as the working directory, so the
// sweep has to find the tree it is meant to measure. Failing to find it is
// fatal rather than a fallback to ".": a sweep rooted at the wrong directory
// reads a handful of files, finds no keys, and reports the entire catalogue
// dead — loud, but for the wrong reason — while the mirror-image mistake of
// rooting it too high reads another checkout entirely.
func moduleRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("resolving the working directory: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod above the package directory; the sweep would measure nothing")
		}
		dir = parent
	}
}

// refuseAnUnderfedSweep fails when the sweep read too little to have measured
// anything. Every assertion in this file passes trivially on an empty evidence
// set, which is the exact shape of a check that is green about nothing.
//
// Measured 2026-08-15 on the tree this barrier ships with (`find . -name '*.go'
// ! -name '*_test.go'` outside the dot-directories, `find internal/templates
// -name '*.html'`): 282 non-test Go files and 38 shipped templates.
//
// The magnitude floors below carry deliberate slack, which is also their weakness
// — losing one mid-sized package would still clear 200. So the load-bearing
// refusal is the structural one: every package under internal/ must have
// contributed a file. A sweep that silently skips a directory is the failure
// this barrier is least able to report on itself, and it has happened here
// before: a template sweep that missed components/ called 15 of 29 live
// functions dead on 2026-08-15.
func refuseAnUnderfedSweep(t *testing.T, root string, evidence *sourceEvidence) {
	t.Helper()

	missed, err := packagesMissedBySweep(root, evidence)
	if err != nil {
		t.Fatalf("listing internal/: %v", err)
	}
	if len(missed) > 0 {
		t.Fatalf("the sweep parsed no Go file in %s; every key those packages name would read as unreachable", strings.Join(missed, ", "))
	}

	if evidence.goFiles < 200 {
		t.Fatalf("parsed only %d non-test Go files; the module is far larger, so this sweep measured the wrong tree", evidence.goFiles)
	}
	if evidence.templateFiles < 30 {
		t.Fatalf("parsed only %d templates; the embedded set is far larger, so this sweep missed a directory", evidence.templateFiles)
	}
	if len(evidence.literals) < 1000 {
		t.Fatalf("collected only %d string literals; a sweep that parsed files but collected nothing would call every key dead", len(evidence.literals))
	}
}

// packagesMissedBySweep names every package under internal/ that contributed no
// parsed file. It is separate from the refusal above so it can be measured in
// both directions; a refusal that only ever runs on a healthy tree proves
// nothing about the case it exists for.
func packagesMissedBySweep(root string, evidence *sourceEvidence) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, "internal"))
	if err != nil {
		return nil, err
	}

	var missed []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if !evidence.goDirs["internal/"+entry.Name()] {
			missed = append(missed, "internal/"+entry.Name())
		}
	}
	sort.Strings(missed)
	return missed, nil
}

// everyShippedKey returns the union of the keys across every shipped catalogue.
//
// The union rather than English alone: the parity test projects plural groups
// onto each language's own CLDR categories, so `.few` and `.many` exist only in
// Russian and English alone would never see them. Messages(lang) already
// returns the English overlay merged with the target, so the union over the
// supported set is the full key space.
func everyShippedKey(manager *i18n.Manager) map[string]bool {
	keys := map[string]bool{}
	for _, language := range manager.SupportedLanguages() {
		for key := range manager.Messages(language) {
			keys[key] = true
		}
	}
	return keys
}

// resolveReachableKeys turns the collected evidence into the set of keys the
// application can actually name.
func resolveReachableKeys(t *testing.T, manager *i18n.Manager, evidence *sourceEvidence, catalogue map[string]bool) map[string]bool {
	t.Helper()

	reachable := map[string]bool{}

	// 1. A literal that the catalogue contains is a key the code names outright.
	for literal := range evidence.literals {
		if catalogue[literal] {
			reachable[literal] = true
		}
	}

	// 2. A plural base names its CLDR variants. TranslatePlural resolves
	//    key+"."+category, then key+".other", then the bare key, so a base the
	//    code names reaches every category any supported language can return.
	//
	//    Every literal is treated as a possible base, rather than only the ones
	//    passed directly to TranslatePlural or `tn`. Measured 2026-08-15: the
	//    precise reading reported 36 keys across nine live families dead —
	//    dashboard.reminder_banner_*, dashboard.late_cycle.* and
	//    stats.statement_cycle_trend_* all name their base through a const or a
	//    struct field and reach the translator through a variable, so the call
	//    site carries no literal to see. Chasing that indirection is dataflow
	//    analysis; treating any literal as a base is sound for this catalogue
	//    instead, because a base's variants exist only because the base is
	//    plural, and the parity test already governs which variants each
	//    language must carry. Residual: a dead "x.one" hides behind a live
	//    plain key "x" — accepted, and the parity test would still see it.
	categories := map[string]bool{"other": true}
	for _, language := range manager.SupportedLanguages() {
		for _, category := range i18n.PluralCategories(language) {
			categories[category] = true
		}
	}
	for base := range evidence.literals {
		for category := range categories {
			reachable[base+"."+category] = true
		}
	}

	// 3. A key built at runtime is reachable only for the members its own
	//    enumerator produces.
	for _, family := range familiesInPlay(t, evidence, catalogue) {
		for _, member := range resolveFamilyMembers(t, manager, evidence, family) {
			reachable[fmt.Sprintf(family.format, member)] = true
		}
	}

	return reachable
}

// familiesInPlay finds every format literal in the sources that could name
// catalogue keys, and fails on one this file does not know how to resolve.
//
// Discovery is deliberately the sources' job rather than this file's: a family
// added tomorrow announces itself here instead of surfacing as a handful of
// keys the barrier calls dead.
func familiesInPlay(t *testing.T, evidence *sourceEvidence, catalogue map[string]bool) []keyFamily {
	t.Helper()

	registered := map[string]keyFamily{}
	for _, family := range localeKeyFamilies {
		registered[family.format] = family
	}

	var found []keyFamily
	var unregistered []string
	for literal := range evidence.literals {
		if !looksLikeAKeyFormat(literal) || !formatMatchesAnyKey(literal, catalogue) {
			continue
		}
		family, ok := registered[literal]
		if !ok {
			unregistered = append(unregistered, literal)
			continue
		}
		found = append(found, family)
	}

	if len(unregistered) > 0 {
		sort.Strings(unregistered)
		t.Fatalf("the sources build locale keys from %d format literal(s) with no registered member source: %s\n"+
			"Add each to localeKeyFamilies naming the declaration that enumerates its members. "+
			"Do not widen a pattern instead: a pattern accepts members the build does not ship.",
			len(unregistered), strings.Join(unregistered, ", "))
	}
	return found
}

// looksLikeAKeyFormat keeps the family search to formats that could name a key
// at all. A bare "%s" would otherwise match every dot-free key in the
// catalogue, and an error format like "read locale %s: %w" carries two verbs.
//
// The heuristic can only over-report: a family it fails to recognise leaves its
// keys unreachable, and the barrier says so by name. It can never make the
// barrier quiet.
func looksLikeAKeyFormat(literal string) bool {
	if len(verbPattern.FindAllString(literal, -1)) != 1 {
		return false
	}
	prefix := literal[:verbPattern.FindStringIndex(literal)[0]]
	return strings.Contains(prefix, ".") && len(prefix) >= 4
}

func formatMatchesAnyKey(literal string, catalogue map[string]bool) bool {
	location := verbPattern.FindStringIndex(literal)
	expression := "^" + regexp.QuoteMeta(literal[:location[0]]) + `[^.]+` + regexp.QuoteMeta(literal[location[1]:]) + "$"
	matcher, err := regexp.Compile(expression)
	if err != nil {
		return false
	}
	for key := range catalogue {
		if matcher.MatchString(key) {
			return true
		}
	}
	return false
}

// resolveFamilyMembers reads a family's members from the declaration that
// produces them, so the answer changes when the product changes.
func resolveFamilyMembers(t *testing.T, manager *i18n.Manager, evidence *sourceEvidence, family keyFamily) []string {
	t.Helper()

	switch {
	case family.fromSupportedLanguages:
		return manager.SupportedLanguages()
	case family.fromStringVar != "":
		return declaredMembers(t, evidence, family, family.fromStringVar, evidence.stringVars)
	case family.fromFuncReturns != "":
		return declaredMembers(t, evidence, family, family.fromFuncReturns, evidence.funcReturns)
	default:
		t.Fatalf("key family %q names no member source", family.format)
		return nil
	}
}

func declaredMembers(t *testing.T, evidence *sourceEvidence, family keyFamily, name string, index map[string][]string) []string {
	t.Helper()

	members, ok := index[name]
	if !ok {
		t.Fatalf("key family %q resolves from %q, which the sweep did not find; it was renamed or removed, and the family's members are now unknown", family.format, name)
	}
	if count := evidence.declCounts[name]; count != 1 {
		t.Fatalf("key family %q resolves from %q, which is declared %d times; the sweep cannot tell which declaration enumerates the family", family.format, name, count)
	}
	return members
}

// collectFromGoTree parses every shipped Go file under root.
//
// Test files are excluded on purpose: this measures what the APPLICATION can
// reach, and a key only a test names is exactly the finding. Dot-directories go
// with them, which is also what keeps agent worktrees under .claude out of a
// sweep that would otherwise read several copies of the repository.
func collectFromGoTree(root string, evidence *sourceEvidence) error {
	skipped := map[string]bool{"node_modules": true, "testdata": true, "e2e": true, "web": true}
	fileSet := token.NewFileSet()

	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		name := entry.Name()
		if entry.IsDir() {
			if path != root && (strings.HasPrefix(name, ".") || skipped[name]) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}

		file, parseErr := parser.ParseFile(fileSet, path, nil, 0)
		if parseErr != nil {
			return fmt.Errorf("parsing %s: %w", path, parseErr)
		}
		relativeFile := path
		if relative, relErr := filepath.Rel(root, path); relErr == nil {
			relativeFile = filepath.ToSlash(relative)
		}
		collectFromGoFile(goOrigin{file: relativeFile, fileSet: fileSet}, file, evidence)
		evidence.goFiles++
		if relative, relErr := filepath.Rel(root, filepath.Dir(path)); relErr == nil {
			evidence.goDirs[filepath.ToSlash(relative)] = true
		}
		return nil
	})
}

// goOrigin carries the repository-relative path a parsed file came from and the
// file set its positions resolve against, so a finding can name a place a
// reader can open. It mirrors templateOrigin on the template side.
type goOrigin struct {
	file    string
	fileSet *token.FileSet
}

func (origin goOrigin) lineOf(position token.Pos) int {
	if origin.fileSet == nil || !position.IsValid() {
		return 0
	}
	return origin.fileSet.Position(position).Line
}

func collectFromGoFile(origin goOrigin, file *ast.File, evidence *sourceEvidence) {
	recordGoFormatSites(origin, file, evidence)

	ast.Inspect(file, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.BasicLit:
			if value, ok := goStringLiteral(typed); ok {
				evidence.literals[value] = true
			}
		}
		return true
	})

	for _, declaration := range file.Decls {
		switch typed := declaration.(type) {
		case *ast.GenDecl:
			if typed.Tok != token.VAR {
				continue
			}
			for _, spec := range typed.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for index, identifier := range value.Names {
					if index >= len(value.Values) {
						continue
					}
					composite, ok := value.Values[index].(*ast.CompositeLit)
					if !ok {
						continue
					}
					var members []string
					for _, element := range composite.Elts {
						literal, ok := element.(*ast.BasicLit)
						if !ok {
							continue
						}
						if text, ok := goStringLiteral(literal); ok {
							members = append(members, text)
						}
					}
					if len(members) > 0 {
						evidence.stringVars[identifier.Name] = members
						evidence.declCounts[identifier.Name]++
					}
				}
			}
		case *ast.FuncDecl:
			var returned []string
			ast.Inspect(typed, func(node ast.Node) bool {
				statement, ok := node.(*ast.ReturnStmt)
				if !ok {
					return true
				}
				for _, result := range statement.Results {
					literal, ok := result.(*ast.BasicLit)
					if !ok {
						continue
					}
					if text, ok := goStringLiteral(literal); ok {
						returned = append(returned, text)
					}
				}
				return true
			})
			if len(returned) > 0 {
				evidence.funcReturns[typed.Name.Name] = returned
				evidence.declCounts[typed.Name.Name]++
			}
		}
	}
}

func goStringLiteral(literal *ast.BasicLit) (string, bool) {
	if literal.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(literal.Value)
	if err != nil {
		return "", false
	}
	return value, true
}

// collectFromShippedTemplates parses the templates the binary embeds, not the
// ones on disk: a template that exists but is not embedded never ships.
//
// Parsing runs with SkipFuncCheck so the sweep needs no copy of the func map —
// a copy would be one more list to keep in step, and the func map already has
// its own barrier one layer up.
func collectFromShippedTemplates(evidence *sourceEvidence) error {
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
		return collectFromTemplateSource(path, string(source), evidence)
	})
}

// collectFromTemplateSource parses one template and feeds the evidence set.
//
// Split out of the walk above so a fixture template can be run through exactly
// the collector the barriers use, rather than through a second copy of it that
// would drift.
func collectFromTemplateSource(path string, source string, evidence *sourceEvidence) error {
	// A fresh tree set per file: two files may legitimately define the
	// same name, and a shared set turns that into a redefinition error.
	trees := map[string]*parse.Tree{}
	tree := parse.New(path)
	tree.Mode = parse.SkipFuncCheck
	if _, parseErr := tree.Parse(source, "", "", trees); parseErr != nil {
		return fmt.Errorf("parsing %s: %w", path, parseErr)
	}

	origin := templateOrigin{file: path, source: source}
	collectFromTemplateNode(tree.Root, origin, evidence)
	for _, associated := range trees {
		if associated.Root != nil {
			collectFromTemplateNode(associated.Root, origin, evidence)
		}
	}
	// Runs after the whole file has been walked, because the literal-copy
	// reader needs every text span of the file at once: it reads the source
	// with the action spans masked out, and a span it has not been told about
	// would read as an action.
	recordTemplateLiteralCopy(origin, evidence)
	evidence.templateFiles++
	return nil
}

// templateOrigin carries the file a node came from and the text it was parsed
// from, so a finding can name a place a reader can open. Node positions are
// byte offsets into that same text, including inside `{{define}}` bodies.
type templateOrigin struct {
	file   string
	source string
}

func (origin templateOrigin) lineOf(position parse.Pos) int {
	offset := int(position)
	if offset < 0 || offset > len(origin.source) {
		return 0
	}
	return 1 + strings.Count(origin.source[:offset], "\n")
}

func collectFromTemplateNode(node parse.Node, origin templateOrigin, evidence *sourceEvidence) {
	switch typed := node.(type) {
	case nil:
		return
	case *parse.ListNode:
		if typed == nil {
			return
		}
		for _, child := range typed.Nodes {
			collectFromTemplateNode(child, origin, evidence)
		}
	case *parse.ActionNode:
		collectFromTemplateNode(typed.Pipe, origin, evidence)
	case *parse.PipeNode:
		if typed == nil {
			return
		}
		for _, command := range typed.Cmds {
			collectFromTemplateNode(command, origin, evidence)
		}
	case *parse.CommandNode:
		recordTemplateKeyCallSite(typed, origin, evidence)
		for _, argument := range typed.Args {
			collectFromTemplateNode(argument, origin, evidence)
		}
	case *parse.StringNode:
		evidence.literals[typed.Text] = true
	case *parse.TextNode:
		recordTemplateTextSpan(typed, evidence)
	case *parse.IfNode:
		collectFromTemplateBranch(&typed.BranchNode, origin, evidence)
	case *parse.RangeNode:
		collectFromTemplateBranch(&typed.BranchNode, origin, evidence)
	case *parse.WithNode:
		collectFromTemplateBranch(&typed.BranchNode, origin, evidence)
	case *parse.TemplateNode:
		collectFromTemplateNode(typed.Pipe, origin, evidence)
	}
}

func collectFromTemplateBranch(branch *parse.BranchNode, origin templateOrigin, evidence *sourceEvidence) {
	collectFromTemplateNode(branch.Pipe, origin, evidence)
	collectFromTemplateNode(branch.List, origin, evidence)
	collectFromTemplateNode(branch.ElseList, origin, evidence)
}

// describeUnreachable lists each unreachable key with whatever the test layer
// still holds against it — the key itself, and the English sentence, each
// searched as a QUOTED literal.
//
// Quoted rather than bare: at catalogue scale a substring search for a value
// like "On" or "Stats" returns most of the repository, so it gets abandoned and
// the check silently does not happen. The quoted form is reviewable by eye.
func describeUnreachable(root string, keys []string, english map[string]string) string {
	pinned := testSideReferences(root, keys, english)

	var report strings.Builder
	for _, key := range keys {
		fmt.Fprintf(&report, "  %s\n", key)
		if value := english[key]; value != "" {
			fmt.Fprintf(&report, "      EN: %q\n", value)
		}
		if hits := pinned[key]; len(hits) > 0 {
			fmt.Fprintf(&report, "      pinned by: %s\n", strings.Join(hits, ", "))
		} else {
			fmt.Fprintf(&report, "      pinned by: nothing in e2e/ or *_test.go\n")
		}
	}
	return report.String()
}

func testSideReferences(root string, keys []string, english map[string]string) map[string][]string {
	needles := map[string][]string{}
	for _, key := range keys {
		candidates := []string{key}
		if value := english[key]; value != "" {
			candidates = append(candidates, value)
		}
		var quoted []string
		for _, candidate := range candidates {
			quoted = append(quoted, `"`+candidate+`"`, `'`+candidate+`'`, "`"+candidate+"`")
		}
		needles[key] = quoted
	}

	hits := map[string][]string{}
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		name := entry.Name()
		if entry.IsDir() {
			if path != root && (strings.HasPrefix(name, ".") || name == "node_modules") {
				return filepath.SkipDir
			}
			return nil
		}

		relative, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}
		underE2E := strings.HasPrefix(filepath.ToSlash(relative), "e2e/")
		if !underE2E && !strings.HasSuffix(name, "_test.go") {
			return nil
		}

		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		text := string(content)
		for key, quoted := range needles {
			for _, needle := range quoted {
				if strings.Contains(text, needle) {
					hits[key] = append(hits[key], filepath.ToSlash(relative))
					break
				}
			}
		}
		return nil
	})

	for key := range hits {
		sort.Strings(hits[key])
	}
	return hits
}

// The collector must see a key in every position the application names one in,
// or the barrier above would pass by simply failing to look.
//
// The tree is SYNTHESISED rather than the repository's own, so it keeps testing
// the collector on the day the real sources change shape — a fixture that
// measures ovumcy-web itself only ever proves that ovumcy-web agrees with
// itself.
func TestLocaleLiteralCollectorSeesEveryKeyPosition(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "go.mod", "module fixture\n")
	writeFixture(t, root, "app/handlers.go", `package app

const noticeKey = "probe.const"

var factorOrder = []string{"alpha", "omega"}

func classify() string {
	if true {
		return "wide"
	}
	return "narrow"
}

func render(messages map[string]string, language string) []string {
	return []string{
		translate(messages, "probe.direct"),
		translate(messages, noticeKey),
		translate(messages, fmt.Sprintf("probe.family.%s", "alpha")),
		i18n.TranslatePlural(messages, language, "probe.counted", 3),
		globalErrorSpec(409, "probe key with spaces"),
	}
}
`)

	evidence := newSourceEvidence()
	if err := collectFromGoTree(root, evidence); err != nil {
		t.Fatalf("sweeping the fixture: %v", err)
	}

	for _, want := range []string{"probe.direct", "probe.const", "probe.family.%s", "probe.counted", "probe key with spaces"} {
		if !evidence.literals[want] {
			t.Errorf("the collector missed %q; a key it cannot see reads as an unreachable key", want)
		}
	}
	if got := evidence.stringVars["factorOrder"]; len(got) != 2 || got[0] != "alpha" || got[1] != "omega" {
		t.Errorf("the collector read factorOrder as %v; a family resolving from it would produce the wrong members", got)
	}
	if got := evidence.funcReturns["classify"]; len(got) != 2 || got[0] != "wide" || got[1] != "narrow" {
		t.Errorf("the collector read classify's returns as %v; a family resolving from it would produce the wrong members", got)
	}
}

// The other direction: what must NOT count as the application naming a key.
// Every entry here would, if counted, keep a genuinely dead key alive — which
// is the failure mode a reachability barrier cannot report on itself.
func TestLocaleLiteralCollectorIgnoresWhatTheApplicationCannotReach(t *testing.T) {
	root := t.TempDir()
	writeFixture(t, root, "go.mod", "module fixture\n")
	writeFixture(t, root, "app/live.go", `package app

// probe.in.comment is named only here.
func live(messages map[string]string) string {
	return translate(messages, "probe.live.suffix")
}
`)
	writeFixture(t, root, "app/live_test.go", `package app

func TestSomething(t *testing.T) {
	_ = translate(nil, "probe.only.in.test")
}
`)
	writeFixture(t, root, ".claude/worktrees/copy/app/live.go", `package app

func stale(messages map[string]string) string {
	return translate(messages, "probe.only.in.worktree")
}
`)

	evidence := newSourceEvidence()
	if err := collectFromGoTree(root, evidence); err != nil {
		t.Fatalf("sweeping the fixture: %v", err)
	}

	for _, name := range []string{"probe.in.comment", "probe.only.in.test", "probe.only.in.worktree"} {
		if evidence.literals[name] {
			t.Errorf("%q was counted as reachable, but the application cannot reach it", name)
		}
	}
	// A key the application names is not the same as a key that merely appears
	// inside one: membership is exact, never substring.
	if evidence.literals["probe.live"] {
		t.Errorf("%q was counted from a longer literal that contains it; a substring match would keep most dead keys alive", "probe.live")
	}
	if !evidence.literals["probe.live.suffix"] {
		t.Fatalf("the fixture itself is broken: the live key was not collected, so the negatives above prove nothing")
	}
	if evidence.goFiles != 1 {
		t.Errorf("parsed %d files; the fixture ships exactly one non-test Go file outside a dot-directory", evidence.goFiles)
	}
}

// A format literal the application uses to build keys must resolve from a named
// enumerator. This pins the refusal itself: without it, an unregistered family
// would quietly contribute nothing and its keys would be reported dead, which
// reads as a product finding rather than a gap in this file.
func TestUnregisteredKeyFamilyIsRefusedRatherThanIgnored(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		literal string
		want    bool
	}{
		{name: "dotted prefix and one verb", literal: "lang.%s", want: true},
		{name: "underscored family", literal: "stats.factor_pattern_%s", want: true},
		{name: "bare verb matches every dot-free key", literal: "%s", want: false},
		{name: "error format carries two verbs", literal: "read locale %s: %w", want: false},
		{name: "no namespace before the verb", literal: "abc%s", want: false},
	} {
		if got := looksLikeAKeyFormat(testCase.literal); got != testCase.want {
			t.Errorf("%s: looksLikeAKeyFormat(%q) = %v, want %v", testCase.name, testCase.literal, got, testCase.want)
		}
	}

	catalogue := map[string]bool{"lang.en": true, "lang.ru": true, "app.name": true}
	if !formatMatchesAnyKey("lang.%s", catalogue) {
		t.Errorf("lang.%%s was not recognised as naming catalogue keys, so its family would go unresolved")
	}
	if formatMatchesAnyKey("lang.%s", map[string]bool{"app.name": true}) {
		t.Errorf("lang.%%s matched a catalogue holding no lang key; the family search would demand a resolver for a format that names nothing")
	}
	// A verb must not swallow a plural suffix, or a family would silently
	// absorb the variants of every key beneath it.
	if formatMatchesAnyKey("cycle.%s", map[string]bool{"cycle.day.one": true}) {
		t.Errorf("a single verb matched across a dot boundary; plural variants would be absorbed by an unrelated family")
	}
}

// The structural refusal is the one that matters, so it is measured rather than
// assumed: a sweep that skipped a package must be named, and a sweep that
// visited every package must not be.
func TestASweepThatSkippedAPackageIsNamed(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"api", "services", "i18n"} {
		writeFixture(t, root, "internal/"+name+"/doc.go", "package "+name+"\n")
	}

	visitedEverything := newSourceEvidence()
	if err := collectFromGoTree(root, visitedEverything); err != nil {
		t.Fatalf("sweeping the fixture: %v", err)
	}
	missed, err := packagesMissedBySweep(root, visitedEverything)
	if err != nil {
		t.Fatalf("listing the fixture's internal/: %v", err)
	}
	if len(missed) > 0 {
		t.Errorf("a sweep that read every package reported %v as missed; the refusal would fail a healthy tree", missed)
	}

	// The other direction: drop one package from what the sweep saw.
	skippedOne := newSourceEvidence()
	for dir := range visitedEverything.goDirs {
		if dir != "internal/services" {
			skippedOne.goDirs[dir] = true
		}
	}
	missed, err = packagesMissedBySweep(root, skippedOne)
	if err != nil {
		t.Fatalf("listing the fixture's internal/: %v", err)
	}
	if len(missed) != 1 || missed[0] != "internal/services" {
		t.Errorf("a sweep that skipped internal/services reported %v; a skipped package that goes unnamed is the failure this barrier cannot see in itself", missed)
	}
}

func writeFixture(t *testing.T, root string, relative string, content string) {
	t.Helper()

	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("creating the fixture directory for %s: %v", relative, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("writing the fixture %s: %v", relative, err)
	}
}
