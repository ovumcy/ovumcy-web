package services

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Barrier for the absent-role naming class.
//
// The security constitution states the product's role model in one line: every
// account is the sole owner of its own data, and "there is no viewer/partner
// role". internal/models backs that with a single constant, RoleOwner. A
// production identifier in this layer that names some OTHER role therefore
// describes a thing the product does not have, and it is read as a design
// statement by everyone who meets it — reviewers, new surfaces, and threat
// models alike. Release 1.4.0 removed "the never-shipped non-owner 'viewer'
// sanitization path", and the day-read service it left behind kept the name:
// ViewerService, ViewerDayReader, FetchDayLogForViewer. Each of those reads
// user.ID and nothing else, so the name promised a boundary the code has never
// had, in the one layer where the role model is enforced.
//
// This sweep reads the shipped source rather than a list of known sites, so an
// identifier added later is covered the moment it is written.
//
// SCOPE, deliberately narrower than the package, and stated here so no reader
// concludes the broader claim from a green run:
//
//   - NON-TEST files only. A test legitimately NAMES an absent role in order to
//     prove it is refused — TestBuildSettingsPageViewDataPartnerSkipsExportSummary
//     drives a user whose Role is "legacy_viewer" — and a barrier that flagged
//     those would push the tree toward deleting the proof.
//   - DECLARED identifiers only: type, func and method names, const and var
//     names, struct fields, interface methods, parameters and results, and
//     short variable declarations. Comments and string literals are invisible
//     to it, which is why the "legacy_viewer" role VALUE fixtures and prose that
//     explains the absence of a viewer role do not trip it.
//   - Whole camel/snake TOKENS, never substrings: "reviewer" is one token and is
//     not "viewer". A substring match would flag safe code and be silenced by an
//     exemption list, which is the failure mode this file has none of.
//
// absentRoleBarrierWords is the subject of the sweep, not an allow-list: these
// are account-role nouns from the multi-user vocabularies this product
// deliberately does not implement. It is kept to nouns that can only mean "an
// account with a role", which is why "member" and "collaborator" are absent —
// both already carry a non-role meaning in this tree (a member of a set, an
// injected collaborator) and flagging them would teach the reader to suppress
// the barrier rather than fix a name.
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

// TestNoServicesIdentifierNamesARoleTheProductDoesNotHave fails when a
// production identifier in internal/services is named after a role the product
// declares absent.
func TestNoServicesIdentifierNamesARoleTheProductDoesNotHave(t *testing.T) {
	root := absentRoleBarrierRepoRoot(t)

	entries, err := os.ReadDir(filepath.Join(root, "internal", "services"))
	if err != nil {
		t.Fatalf("read internal/services: %v", err)
	}

	// The anchor counts FILES rather than findings: a layer with no offending
	// identifier is the state this barrier exists to reach, so an anchor
	// conditioned on the findings would stop firing on the day it succeeds.
	var findings []string
	parsed := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(root, "internal", "services", name)
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		parsed++
		findings = append(findings, absentRoleBarrierScanSource(t, "internal/services/"+name, string(source))...)
	}
	if parsed == 0 {
		t.Fatal("internal/services yielded no non-test Go file — the sweep read nothing and its verdict is vacuous")
	}

	if len(findings) > 0 {
		sort.Strings(findings)
		t.Fatalf("production identifiers in internal/services name a role the product does not have; the only role it declares is models.RoleOwner (\"owner\"), so rename each to the CONCERN it serves (the day-read wrapper serves the acting owner) — do not add the word to absentRoleBarrierWords:\n%s",
			strings.Join(findings, "\n"))
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
			t.Fatalf("absentRoleBarrierWords forbids %q while internal/models declares it as a role — the product's role model changed, so remove %q from absentRoleBarrierWords (and update the security constitution's role line)", word, word)
		}
	}
}

// TestAbsentRoleBarrierClassifiesItsOwnFixtures proves the sweep can report
// both verdicts, on sources the test owns rather than on the tree it judges.
func TestAbsentRoleBarrierClassifiesItsOwnFixtures(t *testing.T) {
	const offending = `package fixture

type ViewerService struct{ days int }

func (service *ViewerService) FetchDayLogForViewer() int { return service.days }
`
	// Every construct below is one the sweep must NOT flag: the declared role
	// itself, a token that merely CONTAINS a forbidden word, an absent role
	// named in a comment, and one named as a string VALUE.
	const clean = `package fixture

// There is no viewer or partner role in this product.
const RoleOwner = "owner"

func IsOwnerUser(role string) bool { return role == RoleOwner }

func reviewerCount(unsupportedRole string) int {
	if unsupportedRole == "legacy_viewer" {
		return 0
	}
	return 1
}
`

	hits := absentRoleBarrierScanSource(t, "fixture/offending.go", offending)
	if len(hits) != 2 {
		t.Fatalf("a type and a method both named for the absent viewer role must produce exactly two findings, got %d: %v", len(hits), hits)
	}
	for _, hit := range hits {
		if !strings.Contains(hit, "viewer") {
			t.Fatalf("a finding must name the offending role word so the reader knows which token to remove, got %q", hit)
		}
	}

	if hits := absentRoleBarrierScanSource(t, "fixture/clean.go", clean); len(hits) != 0 {
		t.Fatalf("the declared role, the token \"reviewer\", a comment and a role STRING VALUE must all pass — a barrier that flags them teaches suppression instead of renaming; got %v", hits)
	}
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
	report := func(ident *ast.Ident) {
		if ident == nil || ident.Name == "_" {
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
				if ident, isIdent := expr.(*ast.Ident); isIdent {
					report(ident)
				}
			}
		}
		return true
	})
	return findings
}

// absentRoleBarrierOffendingWord reports the forbidden role noun an identifier
// carries as a whole camel/snake token, if any.
func absentRoleBarrierOffendingWord(identifier string) (string, bool) {
	tokens := absentRoleBarrierTokens(identifier)
	for _, word := range absentRoleBarrierWords {
		for _, candidate := range tokens {
			if candidate == word {
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

	entries, err := os.ReadDir(filepath.Join(root, "internal", "models"))
	if err != nil {
		t.Fatalf("read internal/models: %v", err)
	}

	roles := make(map[string]bool)
	parsed := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(root, "internal", "models", name)
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		parsed++

		fileSet := token.NewFileSet()
		file, parseErr := parser.ParseFile(fileSet, name, source, 0)
		if parseErr != nil {
			t.Fatalf("parse internal/models/%s: %v", name, parseErr)
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
	if parsed == 0 {
		t.Fatal("internal/models yielded no non-test Go file — the declared role set was read from nothing")
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
