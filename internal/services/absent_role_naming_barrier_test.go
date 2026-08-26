package services

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
)

// Barrier for the absent-role naming class.
//
// docs/architecture.md states the product's role model in one line: it is
// owner-role-only, each account the sole owner of its own data, and "there is no
// viewer or partner role". internal/models backs that with a single constant,
// RoleOwner. A production identifier that names some OTHER role therefore
// describes a thing the product does not have, and it is read as a design
// statement by everyone who meets it — reviewers, new surfaces, and threat
// models alike. Release 1.4.0 removed "the never-shipped non-owner 'viewer'
// sanitization path", and the day-read service it left behind kept the name:
// ViewerService, ViewerDayReader, FetchDayLogForViewer. Each of those reads
// user.ID and nothing else, so the name promised a boundary the code has never
// had.
//
// The sweep covers ALL of internal/, not the one layer the defect was found in.
// This is a NAMING defect, not an enforcement one — a ViewerPageData in
// internal/api or a ViewerRepository in internal/db is the identical claim about
// the product, and a class fixed at N of N+1 sites is a new defect rather than a
// partial fix. It reads the shipped source rather than a list of known sites, so
// a package added later is covered the moment it is written.
//
// SCOPE, deliberately narrower than "every name in the tree", and stated here so
// no reader concludes the broader claim from a green run:
//
//   - NON-TEST files only, and testdata/ is skipped. A test legitimately NAMES
//     an absent role in order to prove it is refused — the settings view-model
//     suite drives a user whose Role is "legacy_viewer" — and a barrier that
//     flagged those would push the tree toward deleting the proof.
//   - DECLARED identifiers only: type, func and method names, const and var
//     names, struct fields, interface methods, parameters and results, short
//     variable declarations and range clauses. Comments and string literals are
//     invisible to it, which is why the "legacy_viewer" role VALUE fixtures and
//     prose that explains the absence of a viewer role do not trip it.
//   - Whole camel/snake TOKENS with a folded plural, never substrings.
//     "reviewer" is one token and is not "viewer"; mapDashboardViewError
//     contains the letters of "viewer" across its View|Error boundary and is not
//     one either. A substring match would flag both, and would then be silenced
//     by an exemption list — which is the failure mode this file has none of.
//
// absentRoleBarrierWords is the subject of the sweep, not an allow-list: these
// are account-role nouns from the multi-user vocabularies this product
// deliberately does not implement. It is kept to nouns that can only mean "an
// account with a role", which is why "member" and "collaborator" are absent —
// both already carry a non-role meaning in this tree (a member of a set, an
// injected collaborator) and flagging them would teach the reader to suppress
// the barrier rather than fix a name.
//
// "viewer" and "partner" carry a LOWER bound derived from docs/architecture.md
// (TestAbsentRoleBarrierKeepsTheRolesTheArchitectureDeclaresAbsent): removing
// either fails. The remaining five have no source outside this file to derive
// them from — they are a judgement about role vocabulary the product does not
// use — so removing one is a deliberate narrowing of the barrier and needs a
// stated reason. What every word does carry is a fixture proving it bites
// (TestAbsentRoleBarrierFlagsEveryWordItLists), so none of them can sit here
// inert.
var absentRoleBarrierWords = []string{
	"viewer",
	"partner",
	"admin",
	"administrator",
	"guest",
	"moderator",
	"subscriber",
	"tenant",
}

// absentRoleBarrierTree is the subtree swept, relative to the module root.
const absentRoleBarrierTree = "internal"

// TestNoProductionIdentifierNamesARoleTheProductDoesNotHave fails when a
// production identifier anywhere under internal/ is named after a role the
// product declares absent.
func TestNoProductionIdentifierNamesARoleTheProductDoesNotHave(t *testing.T) {
	root := absentRoleBarrierRepoRoot(t)
	files := absentRoleBarrierProductionFiles(t, root, absentRoleBarrierTree)

	// The anchor counts FILES rather than findings: a tree with no offending
	// identifier is the state this barrier exists to reach, so an anchor
	// conditioned on the findings would stop firing on the day it succeeds. It
	// doubles as the recursion anchor — internal/ holds no .go file at its top
	// level, so a walk that stopped descending would read nothing and fail here
	// rather than report a clean verdict about the packages it never opened.
	if len(files) == 0 {
		t.Fatalf("%s yielded no production Go file — the sweep read nothing and its verdict is vacuous", absentRoleBarrierTree)
	}

	var findings []string
	for _, path := range files {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		display := filepath.ToSlash(absentRoleBarrierRelative(t, root, path))
		findings = append(findings, absentRoleBarrierScanSource(t, display, string(source))...)
	}

	if len(findings) > 0 {
		sort.Strings(findings)
		t.Fatalf("production identifiers name a role the product does not have; the only role it declares is models.RoleOwner (\"owner\"), so rename each to the CONCERN it serves — do not add the word to absentRoleBarrierWords:\n%s",
			strings.Join(findings, "\n"))
	}
}

// TestAbsentRoleBarrierKeepsTheRolesTheArchitectureDeclaresAbsent derives the
// roles the product documents as absent from docs/architecture.md and asserts
// the barrier still forbids them. It is the lower bound the word list would
// otherwise lack: dropping "viewer" or "partner" silently narrows the barrier to
// nothing, and no other test in this file would notice.
func TestAbsentRoleBarrierKeepsTheRolesTheArchitectureDeclaresAbsent(t *testing.T) {
	root := absentRoleBarrierRepoRoot(t)
	path := filepath.Join(root, "docs", "architecture.md")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read docs/architecture.md: %v", err)
	}

	// The doc's own sentence, read as the source of truth rather than copied
	// into this file. A rewrite that drops the sentence fails here instead of
	// quietly removing the lower bound.
	sentence := regexp.MustCompile(`There is no ([a-z]+) or ([a-z]+)\s+role`)
	match := sentence.FindStringSubmatch(strings.ReplaceAll(string(source), "\n", " "))
	if match == nil {
		t.Fatal("docs/architecture.md no longer states the role model as \"There is no <x> or <y> role\" — the lower bound on absentRoleBarrierWords was derived from that sentence and must be re-derived, not dropped")
	}

	listed := make(map[string]bool, len(absentRoleBarrierWords))
	for _, word := range absentRoleBarrierWords {
		listed[word] = true
	}
	for _, declaredAbsent := range match[1:] {
		if !listed[declaredAbsent] {
			t.Fatalf("docs/architecture.md declares %q a role the product does not have, but absentRoleBarrierWords no longer forbids it — restore %q to the list; the doc is the source, this list is the enforcement", declaredAbsent, declaredAbsent)
		}
	}
}

// TestAbsentRoleBarrierWordsStayDisjointFromTheDeclaredRoles derives the roles
// the product actually declares from internal/models and asserts the barrier
// can never contradict them. It is the one direction in which editing
// absentRoleBarrierWords is the correct answer: a role this list forbids that
// models has since declared is a product change, and the word must leave the
// list rather than the constant leave the product.
func TestAbsentRoleBarrierWordsStayDisjointFromTheDeclaredRoles(t *testing.T) {
	declared := absentRoleBarrierDeclaredRoles(t, absentRoleBarrierRepoRoot(t))

	// Positive anchor on the production predicate: if internal/models stops
	// declaring any role at all the disjointness below is trivially true, and
	// this barrier would report success about a role model nobody looked at.
	if !declared["owner"] {
		t.Fatalf("internal/models declares no Role* constant with the value \"owner\" — the role model this barrier is derived from is gone, got %v", absentRoleBarrierSorted(declared))
	}

	for _, word := range absentRoleBarrierWords {
		if declared[word] {
			t.Fatalf("absentRoleBarrierWords forbids %q while internal/models declares it as a role — the product's role model changed, so remove %q from absentRoleBarrierWords (and update the role line in docs/architecture.md)", word, word)
		}
	}
}

// TestAbsentRoleBarrierFlagsEveryWordItLists gives each listed word a fixture,
// in both the singular and the plural, so no word can sit in the list inert —
// misspelled, or shadowed by a matcher that only ever worked for "viewer".
func TestAbsentRoleBarrierFlagsEveryWordItLists(t *testing.T) {
	for _, word := range absentRoleBarrierWords {
		t.Run(word, func(t *testing.T) {
			capitalized := strings.ToUpper(word[:1]) + word[1:]
			source := fmt.Sprintf("package fixture\n\nvar %sHandle int\n\nvar %ssHandle int\n", capitalized, capitalized)

			hits := absentRoleBarrierScanSource(t, "fixture/"+word+".go", source)
			if len(hits) != 2 {
				t.Fatalf("%q must be flagged in the singular AND the plural, got %d findings: %v", word, len(hits), hits)
			}
			for _, hit := range hits {
				if !strings.Contains(hit, word) {
					t.Fatalf("a finding must name the offending role word so the reader knows which token to remove, got %q", hit)
				}
			}
		})
	}
}

// TestAbsentRoleBarrierClassifiesItsOwnFixtures proves the sweep can report
// both verdicts, on sources the test owns rather than on the tree it judges.
func TestAbsentRoleBarrierClassifiesItsOwnFixtures(t *testing.T) {
	// Six findings: a type, a method, a short variable declaration, both halves
	// of a range clause, and a plural. The range clause and the plural are the
	// shapes a list-of-role accessor would actually take.
	const offending = `package fixture

type ViewerService struct{ days int }

func (service *ViewerService) FetchDayLogForViewer() int { return service.days }

func listAccounts(accounts []int) int {
	partner := accounts[0]
	for guestIndex, admins := range accounts {
		partner += guestIndex + admins
	}
	return partner
}

func FetchViewersForOwner() []int { return nil }
`
	// Every construct below is one the sweep must NOT flag: the declared role
	// itself, a token that merely CONTAINS a forbidden word, an identifier whose
	// camel boundary spells one across two tokens, an absent role named in a
	// comment, and one named as a string VALUE.
	const clean = `package fixture

// There is no viewer or partner role in this product.
const RoleOwner = "owner"

func IsOwnerUser(role string) bool { return role == RoleOwner }

func mapDashboardViewError(err error) error { return err }

func reviewerCount(unsupportedRole string) int {
	if unsupportedRole == "legacy_viewer" {
		return 0
	}
	return 1
}
`

	hits := absentRoleBarrierScanSource(t, "fixture/offending.go", offending)
	if len(hits) != 6 {
		t.Fatalf("the offending fixture declares six identifiers named for an absent role (type, method, short var, both range halves, plural func) and must produce exactly six findings, got %d: %v", len(hits), hits)
	}

	if hits := absentRoleBarrierScanSource(t, "fixture/clean.go", clean); len(hits) != 0 {
		t.Fatalf("the declared role, the token \"reviewer\", the View|Error camel boundary, a comment and a role STRING VALUE must all pass — a barrier that flags them teaches suppression instead of renaming; got %v", hits)
	}
}

// absentRoleBarrierProductionFiles walks one subtree of the module and returns
// every non-test .go file in it, testdata excluded. It is the single walk both
// the sweep and the declared-role reader use: two copies of this filter would
// drift the first time one of them learned to descend and the other did not.
func absentRoleBarrierProductionFiles(t *testing.T, root string, subtree string) []string {
	t.Helper()

	var files []string
	walkErr := filepath.WalkDir(filepath.Join(root, filepath.FromSlash(subtree)), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk %s: %v", subtree, walkErr)
	}
	sort.Strings(files)
	return files
}

// absentRoleBarrierScanSource returns one finding per declared identifier in
// one file that carries a forbidden role token.
func absentRoleBarrierScanSource(t *testing.T, display string, source string) []string {
	t.Helper()

	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, display, source, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", display, err)
	}

	var findings []string
	report := func(node ast.Node) {
		ident, isIdent := node.(*ast.Ident)
		if !isIdent || ident == nil || ident.Name == "_" {
			return
		}
		word, found := absentRoleBarrierOffendingWord(ident.Name)
		if !found {
			return
		}
		findings = append(findings, fmt.Sprintf("  %s:%d %s (names the absent role %q)",
			display, fileSet.Position(ident.Pos()).Line, ident.Name, word))
	}

	ast.Inspect(file, func(node ast.Node) bool {
		switch typed := node.(type) {
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
	return findings
}

// absentRoleBarrierOffendingWord reports the forbidden role noun an identifier
// carries as a whole camel/snake token, if any. A trailing "s" is folded before
// comparing: a list-of-role accessor names its subject in the plural, which is
// the likeliest shape of this defect and the one a singular-only match misses.
func absentRoleBarrierOffendingWord(identifier string) (string, bool) {
	tokens := absentRoleBarrierTokens(identifier)
	for _, word := range absentRoleBarrierWords {
		for _, candidate := range tokens {
			if candidate == word || strings.TrimSuffix(candidate, "s") == word {
				return word, true
			}
		}
	}
	return "", false
}

// absentRoleBarrierTokens splits an identifier into lower-cased camelCase and
// snake_case tokens. Whole tokens are the unit on purpose: a substring match
// reads "reviewer" as "viewer" and a digit-or-acronym run as a word boundary
// that is not there.
func absentRoleBarrierTokens(identifier string) []string {
	var tokens []string
	var current strings.Builder
	flush := func() {
		if current.Len() > 0 {
			tokens = append(tokens, strings.ToLower(current.String()))
			current.Reset()
		}
	}

	runes := []rune(identifier)
	for index, letter := range runes {
		switch {
		case letter == '_':
			flush()
			continue
		case index > 0 && isUpperRune(letter) && !isUpperRune(runes[index-1]):
			// lower→UPPER: "dayLog" → "day", "Log".
			flush()
		case index > 0 && isUpperRune(letter) && index+1 < len(runes) && isLowerRune(runes[index+1]):
			// UPPER→Upperlower: "HTTPViewer" → "HTTP", "Viewer".
			flush()
		}
		current.WriteRune(letter)
	}
	flush()
	return tokens
}

func isUpperRune(letter rune) bool { return letter >= 'A' && letter <= 'Z' }

func isLowerRune(letter rune) bool { return letter >= 'a' && letter <= 'z' }

// absentRoleBarrierDeclaredRoles reads the role VALUES internal/models
// declares — every `Role…` constant bound to a string literal — so the barrier's
// notion of "a role the product has" is the product's own, not a copy of it.
func absentRoleBarrierDeclaredRoles(t *testing.T, root string) map[string]bool {
	t.Helper()

	files := absentRoleBarrierProductionFiles(t, root, "internal/models")
	if len(files) == 0 {
		t.Fatal("internal/models yielded no production Go file — the declared role set was read from nothing")
	}

	roles := make(map[string]bool)
	for _, path := range files {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}

		fileSet := token.NewFileSet()
		file, parseErr := parser.ParseFile(fileSet, filepath.Base(path), source, 0)
		if parseErr != nil {
			t.Fatalf("parse %s: %v", path, parseErr)
		}
		for _, decl := range file.Decls {
			general, isGeneral := decl.(*ast.GenDecl)
			if !isGeneral || general.Tok != token.CONST {
				continue
			}
			for _, spec := range general.Specs {
				value, isValue := spec.(*ast.ValueSpec)
				if !isValue {
					continue
				}
				for index, constName := range value.Names {
					if !strings.HasPrefix(constName.Name, "Role") || index >= len(value.Values) {
						continue
					}
					literal, isLiteral := value.Values[index].(*ast.BasicLit)
					if !isLiteral || literal.Kind != token.STRING {
						continue
					}
					text, unquoteErr := strconv.Unquote(literal.Value)
					if unquoteErr != nil {
						continue
					}
					roles[strings.ToLower(text)] = true
				}
			}
		}
	}
	return roles
}

func absentRoleBarrierSorted(set map[string]bool) []string {
	values := make([]string, 0, len(set))
	for value := range set {
		values = append(values, value)
	}
	sort.Strings(values)
	return values
}

func absentRoleBarrierRelative(t *testing.T, root string, path string) string {
	t.Helper()

	relative, err := filepath.Rel(root, path)
	if err != nil {
		t.Fatalf("relativize %s against %s: %v", path, root, err)
	}
	return relative
}

func absentRoleBarrierRepoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod above the working directory — the sweep cannot find the module root")
		}
		dir = parent
	}
}
