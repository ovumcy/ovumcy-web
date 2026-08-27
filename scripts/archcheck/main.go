// Command archcheck answers four architecture questions about the TREE, not
// about one edit: transport imports no persistence, the layers of
// docs/architecture.md import in one direction only, no schema is migrated
// at runtime, and no identifier names a role the product does not have.
//
// The distinction is the whole reason it exists. Two of those invariants are
// also guarded at the moment of an edit, and that guard sees one tool call and
// never the file the call produced: `db.AutoMigr` plus `ate(...)` written as two
// edits passes any pattern, and so does an import split across two. Widening the
// patterns cannot reach that — the defect is the SHAPE of an event-shaped rule,
// which asks "does this call contain X" when the invariant is "does the tree
// contain X". Asked of the tree, the answer is idempotent and indifferent to how
// the text arrived: one edit, ten, a patch script, a generator.
//
// Two passes, cheapest first:
//
//   - Syntax (go/parser), over every Go file in the module. This alone decides
//     the two import rules exactly — an import graph IS syntax — and it is what
//     makes the common case cost a parse and nothing more. Parsing the files
//     rather than listing the packages is also what makes those rules see a
//     file behind a build tag this platform does not select: internal/services
//     holds one (the `gofuzz` libFuzzer harness), and `go list` answers about
//     the build it was asked for, never about the tree.
//   - Types (golang.org/x/tools/go/packages), over only those packages the
//     syntactic pass found a candidate in. It is what makes the AutoMigrate rule
//     exact rather than name-matched: a local variable named db with an
//     AutoMigrate method of its own is not gorm's, and a rule that matched the
//     spelling would refuse it while a promoted gorm.DB embedded in a wrapper
//     walked through. The receiver decides, so the METHOD's own package is what
//     is compared — which reads promotion correctly by construction.
//
// Every failure it cannot classify is a refusal, not a pass: a file that will
// not parse, a package that will not load, a call whose receiver the type
// checker did not resolve. An instrument that answers "clean" when it could not
// look is worse than none, because the surfaces above it read exit 0 as proof.
//
// Exit codes: 0 nothing found, 1 findings printed, 2 the scan itself failed.
// Findings go to stderr, so a caller that refuses can hand git's output straight
// to the operator.
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

// Rule identifiers. They are printed with every finding and are the stable
// handle a rule file, a test or a message can name — the text of a message may
// be improved, the identifier may not.
const (
	ruleAPIImports   = "api-imports"
	ruleLayerImports = "layer-imports"
	ruleAutoMigrate  = "automigrate"
	ruleAbsentRole   = "absent-role"
	ruleUnreadable   = "unreadable"
)

// modulePath is this module's import prefix. The layer rules deny package paths
// under it, so it is spelled once here rather than once per entry.
const modulePath = "github.com/ovumcy/ovumcy-web"

// gormPkgPath is compared against the package that DECLARES the method reached
// by the call, never against the spelling of the receiver.
const gormPkgPath = "gorm.io/gorm"

// forbiddenImports is the persistence surface transport may not reach. Each
// entry matches the path itself and anything under it, so "gorm.io" covers
// gorm.io/driver/postgres. It is deliberately a list of module paths rather
// than a pattern: a driver added to the tree is a deliberate act, and the day a
// new one arrives this list is where that decision gets recorded.
var forbiddenImports = []string{
	"gorm.io",
	"database/sql",
	"github.com/ovumcy/ovumcy-web/internal/db",
	"github.com/glebarez/sqlite",
	"modernc.org/sqlite",
	"github.com/mattn/go-sqlite3",
	"github.com/lib/pq",
	"github.com/jackc/pgx",
}

// transportDirs are the package paths that are transport-only. internal/apideps
// is here because it exists to keep internal/api that way: it is the port
// surface transport reaches persistence through, so a persistence import there
// defeats the same rule one file further out.
var transportDirs = []string{
	"internal/api",
	"internal/apideps",
}

// layerRule is one direction of the layering drawn in docs/architecture.md:
// the package tree it constrains, and the module-local packages that tree may
// not import. api -> services -> db -> models runs one way, so each layer's
// rule is the set of layers ABOVE it.
//
// Denials, not an allow-list of what a layer may reach. The allowed set grows
// with ordinary work — internal/services imports internal/i18n today, which it
// did not when the layers were first drawn — so a list transcribed from the
// graph as it stands would refuse the first legitimate edge someone adds, and
// the refusal would land on a contributor who did nothing wrong. What the
// contract fixes is the direction; a denial states the direction and nothing
// else.
//
// Only module-local paths, and that is the division of labour with
// ruleAPIImports above rather than an omission: transport is denied the
// persistence surface itself (gorm, the drivers, database/sql) because
// "transport never reaches the database" is about the database. "Domain logic
// never depends on HTTP" and "persisted types stay transport-free" are about
// this module's own layers — internal/services legitimately dials net/http to
// deliver a webhook, and denying it that would be a rule the contract does not
// state.
type layerRule struct {
	dir    string   // the package tree the rule constrains, relative to the module root
	denied []string // package paths it may not import; each covers its own subtree
	remedy string   // what to do instead, printed with every finding
}

var layerRules = []layerRule{
	{
		dir:    "internal/services",
		denied: []string{modulePath + "/internal/api", modulePath + "/internal/apideps"},
		remedy: "domain logic sits below transport: transport calls a service, and a service answers with domain data and sentinel errors.",
	},
	{
		dir:    "internal/db",
		denied: []string{modulePath + "/internal/api", modulePath + "/internal/apideps", modulePath + "/internal/services"},
		remedy: "persistence sits below domain logic: a repository takes what it needs as arguments and returns models, and internal/services is what calls it.",
	},
	{
		// The bottom of the layering, which is why its denial is the module
		// itself: internal/models has no outgoing edge inside this module at
		// all, and that is the property "transport-free types" names.
		dir:    "internal/models",
		denied: []string{modulePath},
		remedy: "persisted types depend on no other layer: keep the type here and put the behaviour that needs a neighbour in internal/services.",
	},
}

// absentRoleWords are account-role nouns from the multi-user vocabularies this
// product does not implement. docs/architecture.md states the role model in one
// line — owner-role-only, and "there is no viewer or partner role" — and
// internal/models declares exactly one Role constant, RoleOwner. An identifier
// naming some OTHER role describes a thing the product does not have, and is
// read as a design statement by everyone who meets it. Release 1.4.0 removed
// "the never-shipped non-owner 'viewer' sanitization path" and the day-read
// service it left behind kept the name for a year: ViewerService,
// ViewerDayReader, FetchDayLogForViewer, each reading nothing but user.ID.
//
// This is a NAMING rule, not an enforcement one, so it is asked of the whole
// module rather than of the layer the defect was last found in: a ViewerPageData
// in internal/api, a ViewerRepository in internal/db and a partnerMode in
// cmd/ovumcy are the identical claim, and a class fixed at N of N+1 sites is a
// new defect rather than a partial fix.
//
// The list is kept to nouns that can only mean "an account with a role", which
// is why "member" and "collaborator" are absent — both already carry a non-role
// meaning in this tree (a member of a set, an injected collaborator), and
// flagging them would teach the reader to suppress the rule rather than fix a
// name. "viewer" and "partner" are pinned to the role line in
// docs/architecture.md by TestTheAbsentRoleWordsKeepTheRolesTheArchitectureDeclaresAbsent;
// every word, including the rest, is held down by a realistic identifier in
// TestEveryAbsentRoleWordIsProvedByARealisticIdentifier, so a word cannot be
// removed, misspelled, or left here inert without a test going red.
var absentRoleWords = []string{
	"viewer",
	"partner",
	"admin",
	"administrator",
	"guest",
	"moderator",
	"subscriber",
	"tenant",
}

// skippedDirs never hold source this module builds. testdata is excluded by the
// go tool itself, and the other two hold trees that are not this module's even
// when they parse as Go. Any directory whose name begins with a dot is skipped
// too — the working trees under .tmp are copies of this repository, and scanning
// them would report one violation once per checkout.
var skippedDirs = map[string]bool{
	"testdata":     true,
	"node_modules": true,
	"vendor":       true,
}

// finding is one violation, positioned where an editor can open it.
type finding struct {
	rule string
	pos  token.Position
	msg  string
}

func (f finding) String() string {
	return fmt.Sprintf("%s: %s: %s", f.pos, f.rule, f.msg)
}

func main() {
	root := flag.String("root", ".", "module root to scan")
	flag.Parse()

	findings, err := run(*root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "archcheck: the scan did not complete, so nothing here is a clean bill: %v\n", err)
		os.Exit(2)
	}
	if len(findings) == 0 {
		return
	}
	for _, f := range findings {
		fmt.Fprintln(os.Stderr, f)
	}
	// The pointer has to name something a clone actually has. Each finding
	// already carries its own remedy, so this trailer only has to say where the
	// contract behind them is written down: docs/architecture.md draws the
	// layers, CONTRIBUTING.md says how to run this command.
	fmt.Fprintf(os.Stderr,
		"archcheck: %d finding(s). The layer contract is docs/architecture.md; running this check is in CONTRIBUTING.md.\n",
		len(findings))
	os.Exit(1)
}

// run scans the module rooted at root and returns its findings, sorted.
func run(root string) ([]finding, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolving %q: %w", root, err)
	}
	// The walk and the type checker must agree on what a file is CALLED, and
	// they reach the tree by different routes: this walk descends from the path
	// it was handed, while `go list` answers in the canonical one. On Windows a
	// junction, or a temporary directory reached through its 8.3 short name,
	// makes those two spellings differ for the same file — and the typed pass
	// would then match none of its own candidates and report nothing.
	if resolved, resolveErr := filepath.EvalSymlinks(abs); resolveErr == nil {
		abs = resolved
	} else {
		return nil, fmt.Errorf("resolving %q: %w", root, resolveErr)
	}

	fset := token.NewFileSet()
	var findings []finding
	var candidates []string

	walkErr := filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != abs && skipDir(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		file, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			// Refused, not skipped: a file this pass cannot read is one whose
			// imports and calls it did not see, and reporting nothing about it
			// would be the fail-open this command exists to close.
			findings = append(findings, finding{
				rule: ruleUnreadable,
				pos:  token.Position{Filename: path},
				msg:  "this file does not parse, so nothing in it was checked: " + parseErr.Error(),
			})
			return nil
		}
		findings = append(findings, importFindings(fset, abs, path, file)...)
		findings = append(findings, layerFindings(fset, abs, path, file)...)
		findings = append(findings, absentRoleFindings(fset, abs, path, file)...)
		if hasAutoMigrateCall(file) {
			candidates = append(candidates, path)
		}
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("walking %s: %w", abs, walkErr)
	}

	if len(candidates) > 0 {
		typed, typeErr := autoMigrateFindings(abs, candidates)
		if typeErr != nil {
			return nil, typeErr
		}
		findings = append(findings, typed...)
	}

	// Positions are reported relative to the scan root. Both refusing surfaces
	// run at the top of the repository, and one of them scans a throwaway copy
	// of the index: an absolute path there names a directory that no longer
	// exists by the time the operator reads the message.
	for i := range findings {
		findings[i].pos.Filename = relativeTo(abs, findings[i].pos.Filename)
	}

	sort.Slice(findings, func(i, j int) bool {
		if findings[i].pos.Filename != findings[j].pos.Filename {
			return findings[i].pos.Filename < findings[j].pos.Filename
		}
		return findings[i].pos.Offset < findings[j].pos.Offset
	})
	return findings, nil
}

func skipDir(name string) bool {
	return strings.HasPrefix(name, ".") || skippedDirs[name]
}

// relativeTo names a file the way a reader at the scan root would. It falls
// back to the absolute path rather than to an error: a position that cannot be
// made relative is still a position, and dropping the finding to keep the
// formatting tidy would be the wrong trade.
func relativeTo(root, file string) string {
	rel, err := filepath.Rel(root, file)
	if err != nil {
		return file
	}
	return filepath.ToSlash(rel)
}

// pathKey is how two spellings of one file are compared — cleaned, and case
// folded exactly where the filesystem folds it. Windows answers the same file
// under more than one casing, and a map keyed on the raw string would then miss
// its own entry; POSIX does not, and folding there would merge two files that
// differ only in case.
func pathKey(p string) string {
	p = filepath.Clean(p)
	if runtime.GOOS == "windows" {
		return strings.ToLower(p)
	}
	return p
}

// importFindings applies the transport-only rule to one parsed file.
//
// Test files are exempt, and that is a scope decision rather than an oversight:
// fixtures under internal/api need a real database by construction, and a rule
// covering them would refuse ordinary test work on its first day.
func importFindings(fset *token.FileSet, root, path string, file *ast.File) []finding {
	if !inTransportLayer(root, path) {
		return nil
	}
	var out []finding
	for _, spec := range file.Imports {
		imported, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		if !isForbiddenImport(imported) {
			continue
		}
		out = append(out, finding{
			rule: ruleAPIImports,
			pos:  fset.Position(spec.Pos()),
			msg: "this file is transport-only and must not import " + imported +
				". Persistence lives in internal/db behind a repository, reached from internal/services; transport calls the service.",
		})
	}
	return out
}

// layerFindings applies the one-directional layering to one parsed file.
//
// Test files are exempt, on the same reading that exempts them from
// ruleAPIImports, and here the exemption is load-bearing rather than a courtesy:
// internal/db's tests import internal/services and internal/services' tests
// import internal/db, both by construction — a repository is proved against the
// service that owns it and vice versa. A rule covering them would refuse
// ordinary test work on its first day, and the way out of it would be to route
// tests around the database, which costs more than the rule is worth.
func layerFindings(fset *token.FileSet, root, path string, file *ast.File) []finding {
	rel, ok := productionFile(root, path)
	if !ok {
		return nil
	}
	var out []finding
	for _, rule := range layerRules {
		if !strings.HasPrefix(rel, rule.dir+"/") {
			continue
		}
		for _, spec := range file.Imports {
			imported, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				continue
			}
			if !matchesAny(imported, rule.denied) {
				continue
			}
			out = append(out, finding{
				rule: ruleLayerImports,
				pos:  fset.Position(spec.Pos()),
				msg:  rule.dir + " must not import " + imported + ". " + rule.remedy,
			})
		}
	}
	return out
}

// absentRoleFindings applies the absent-role naming rule to one parsed file.
//
// Test files are exempt, on the same reading that exempts them from the import
// rules, and here the exemption is the point rather than a courtesy: a test
// legitimately NAMES an absent role in order to prove it is refused — fixtures
// across internal/services and internal/api drive accounts whose Role is
// "viewer" or "legacy_viewer" precisely to make the owner predicate false — and
// a rule covering them would push the tree toward deleting the proof.
//
// It reads DECLARED identifiers only: type, func and method names, const and var
// names, struct fields, interface methods, parameters and results, short
// variable declarations and range clauses. Comments and string literals are
// invisible to it, which is why a role VALUE like "legacy_viewer" and prose
// explaining the absence of a viewer role do not trip it.
func absentRoleFindings(fset *token.FileSet, root, path string, file *ast.File) []finding {
	if _, ok := productionFile(root, path); !ok {
		return nil
	}

	var out []finding
	report := func(node ast.Node) {
		ident, ok := node.(*ast.Ident)
		if !ok || ident == nil || ident.Name == "_" {
			return
		}
		word, found := absentRoleWordIn(ident.Name)
		if !found {
			return
		}
		out = append(out, finding{
			rule: ruleAbsentRole,
			pos:  fset.Position(ident.Pos()),
			msg: fmt.Sprintf("%s names %q, a role this product does not have. The only role it declares is models.RoleOwner; "+
				"docs/architecture.md states there is no viewer or partner role. Rename it for the CONCERN it serves — "+
				"the day-read seam that used to be ViewerService is OwnerDayReadService. Do not add the word to absentRoleWords.",
				ident.Name, word),
		})
	}

	ast.Inspect(file, func(n ast.Node) bool {
		switch typed := n.(type) {
		case *ast.TypeSpec:
			report(typed.Name)
		case *ast.FuncDecl:
			report(typed.Name)
		case *ast.ValueSpec:
			for _, name := range typed.Names {
				report(name)
			}
		case *ast.Field:
			// Struct fields, interface methods, parameters and results.
			for _, name := range typed.Names {
				report(name)
			}
		case *ast.AssignStmt:
			if typed.Tok != token.DEFINE {
				return true
			}
			for _, expr := range typed.Lhs {
				report(expr)
			}
		case *ast.RangeStmt:
			// `for k, v := range …` declares k and v with :=, but carries them
			// on RangeStmt rather than on an AssignStmt.
			if typed.Tok != token.DEFINE {
				return true
			}
			report(typed.Key)
			report(typed.Value)
		}
		return true
	})
	return out
}

// absentRoleWordIn reports the forbidden role noun an identifier carries as a
// whole camel/snake token, if any.
//
// A trailing "s" is folded before comparing: a list-of-role accessor names its
// subject in the plural, which is the likeliest shape of this defect and the one
// a singular-only match misses.
//
// Whole tokens are the unit, never substrings. "reviewer" is one token and is
// not "viewer"; mapDashboardViewError spells the letters of "viewer" across its
// View|Error boundary and is not one either. A substring match would flag both,
// and would then be silenced by an exemption list — which is the failure mode
// this rule has none of.
func absentRoleWordIn(identifier string) (string, bool) {
	tokens := identifierTokens(identifier)
	for _, word := range absentRoleWords {
		for _, candidate := range tokens {
			if candidate == word || strings.TrimSuffix(candidate, "s") == word {
				return word, true
			}
		}
	}
	return "", false
}

// identifierTokens splits an identifier into lower-cased camelCase and
// snake_case tokens.
func identifierTokens(identifier string) []string {
	var tokens []string
	var current strings.Builder
	flush := func() {
		if current.Len() > 0 {
			tokens = append(tokens, strings.ToLower(current.String()))
			current.Reset()
		}
	}

	runes := []rune(identifier)
	for i, r := range runes {
		switch {
		case r == '_':
			flush()
			continue
		case i > 0 && isUpper(r) && !isUpper(runes[i-1]):
			// lower→UPPER: "dayLog" → "day", "Log".
			flush()
		case i > 0 && isUpper(r) && i+1 < len(runes) && isLower(runes[i+1]):
			// UPPER→Upperlower: "HTTPViewer" → "HTTP", "Viewer".
			flush()
		}
		current.WriteRune(r)
	}
	flush()
	return tokens
}

func isUpper(r rune) bool { return r >= 'A' && r <= 'Z' }

func isLower(r rune) bool { return r >= 'a' && r <= 'z' }

// inTransportLayer reports whether path is production source under a
// transport-only package. The comparison runs on the path relative to the module
// root and in slash form, so the same file answers the same way whichever
// platform and whichever checkout it is read from.
func inTransportLayer(root, path string) bool {
	rel, ok := productionFile(root, path)
	if !ok {
		return false
	}
	for _, dir := range transportDirs {
		if strings.HasPrefix(rel, dir+"/") {
			return true
		}
	}
	return false
}

// productionFile names path the way a reader at the module root would — relative
// and in slash form — and reports false for a file no import rule covers: one
// outside the root, or a test file.
//
// Both import rules ask this single question, and they have to answer it the
// same way. Two copies of the test-file check is how one of them ends up
// covering _test.go by accident on the day the other is edited.
func productionFile(root, path string) (string, bool) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	if strings.HasSuffix(rel, "_test.go") {
		return "", false
	}
	return rel, true
}

func isForbiddenImport(imported string) bool {
	return matchesAny(imported, forbiddenImports)
}

// matchesAny reports whether imported is one of paths or lives under one. The
// prefix is taken on a path SEGMENT and never on the raw string, so
// "database/sqlx" is not "database/sql".
func matchesAny(imported string, paths []string) bool {
	for _, p := range paths {
		if imported == p || strings.HasPrefix(imported, p+"/") {
			return true
		}
	}
	return false
}

// hasAutoMigrateCall is the prefilter, and it is deliberately generous: it says
// "a call spelled AutoMigrate happens here", which is a question about syntax,
// and leaves "whose AutoMigrate" to the pass that can answer it. Generous in
// this direction is free — a false candidate costs one package load — while the
// reverse would cost the rule.
func hasAutoMigrateCall(file *ast.File) bool {
	found := false
	ast.Inspect(file, func(n ast.Node) bool {
		if found {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, isSel := call.Fun.(*ast.SelectorExpr); isSel && sel.Sel.Name == "AutoMigrate" {
			found = true
			return false
		}
		return true
	})
	return found
}

// autoMigrateFindings type-checks the packages holding the candidate files and
// keeps the calls whose method gorm declares.
//
// Only those packages are loaded. The whole module would answer the same
// question and cost seconds on every run, including the overwhelmingly common
// run where the prefilter found nothing at all — which is why this function is
// not reached in that case.
func autoMigrateFindings(root string, candidates []string) ([]finding, error) {
	wanted := make(map[string]bool, len(candidates))
	patterns := make([]string, 0, len(candidates))
	for _, c := range candidates {
		wanted[pathKey(c)] = true
		patterns = append(patterns, "file="+c)
	}

	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo |
			packages.NeedImports | packages.NeedDeps,
		Dir:   root,
		Tests: true,
	}
	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, fmt.Errorf("type-checking the packages that call AutoMigrate: %w", err)
	}

	var loadErrs []string
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		for _, e := range p.Errors {
			loadErrs = append(loadErrs, p.PkgPath+": "+e.Error())
		}
	})
	if len(loadErrs) > 0 {
		// Fail closed. A type error means the receiver was not resolved, and an
		// unresolved receiver is precisely the case this pass exists to decide.
		return nil, fmt.Errorf("the tree does not type-check, so an AutoMigrate call's receiver could not be identified:\n  %s",
			strings.Join(loadErrs, "\n  "))
	}

	seen := make(map[token.Position]bool)
	var out []finding
	for _, p := range pkgs {
		for _, file := range p.Syntax {
			name := pathKey(p.Fset.Position(file.Pos()).Filename)
			if !wanted[name] {
				continue
			}
			out = append(out, autoMigrateInFile(p, file, seen)...)
		}
	}
	return out, nil
}

// autoMigrateInFile collects gorm's AutoMigrate calls in one type-checked file.
//
// seen spans the whole load rather than one file because Tests: true hands back
// the same file inside more than one package variant, and the same call reported
// twice reads as two violations of one rule.
func autoMigrateInFile(p *packages.Package, file *ast.File, seen map[token.Position]bool) []finding {
	var out []finding
	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "AutoMigrate" {
			return true
		}
		pos := p.Fset.Position(sel.Sel.Pos())
		if seen[pos] {
			return true
		}
		selection := p.TypesInfo.Selections[sel]
		if selection == nil || selection.Kind() != types.MethodVal {
			seen[pos] = true
			out = append(out, finding{
				rule: ruleAutoMigrate,
				pos:  pos,
				msg: "the receiver of this AutoMigrate call did not resolve to a type, so whether it is gorm's could not be decided. " +
					"Name the receiver's type, or state why this call is not gorm's.",
			})
			return true
		}
		obj := selection.Obj()
		if obj.Pkg() == nil || obj.Pkg().Path() != gormPkgPath {
			return true
		}
		seen[pos] = true
		out = append(out, finding{
			rule: ruleAutoMigrate,
			pos:  pos,
			msg: "gorm AutoMigrate is forbidden. Schema changes are forward-only SQL in migrations/, " +
				"applied by the runner in internal/db/migrations.go, which is their single source of truth.",
		})
		return true
	})
	return out
}
