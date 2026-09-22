package db

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// scopedUserUpdateBuilders are the only two functions in user_repository.go
// allowed to build a users-table UPDATE query's bare `id = ?` clause. Both
// refuse a zero id (ErrUserOwnerRequired) before returning the query, which is
// exactly the property this guard exists to keep true of every OTHER function
// in the file: none of them may construct that same
// `Model(&models.User{}).Where("id = ?", userID)` shape on their own, because
// doing so would skip the refusal.
var scopedUserUpdateBuilders = map[string]bool{
	"scopedUserUpdate":   true,
	"scopedUserUpdateTx": true,
}

// concatenatedPredicateSites are the compound writers whose WHERE predicate is
// not a string literal at all but a concatenation. They are named here because
// a scan keyed on the literal's SHAPE skipped exactly these two while reporting
// a healthy population: removing ReleaseWebhookWatermark's refusal left the
// guard green. Counting sites cannot catch that — the count was satisfied by
// the wrong members — so the two load-bearing names are asserted directly.
var concatenatedPredicateSites = map[string]string{
	"ClaimWebhookWatermark":   "accepted because it returns the zero-row outcome to its caller",
	"ReleaseWebhookWatermark": "accepted because it calls requireUserOwnerID before building the clause",
}

// bareIDClause matches one conjunct that scopes a users-table UPDATE to a
// single owner. It is applied per conjunct rather than to the whole predicate:
// `id = ?` anywhere in the AND-chain scopes the write, so a site spelled
// `Where("totp_enabled = ? AND id = ?", ...)` is the same shape as the leading
// form and must be classified the same way.
var bareIDClause = regexp.MustCompile(`(?i)^id\s*=\s*\?$`)

// conjunctSeparator splits an AND-chain. Case-insensitive because the spelling
// of the keyword is the author's choice and carries no meaning here.
var conjunctSeparator = regexp.MustCompile(`(?i)\s+AND\s+`)

// nonLiteralPredicatePart stands in for the parts of a concatenated predicate
// that are not string literals (`columns.watermark`, a helper's return). It
// contains neither a conjunct separator nor an `id = ?`, so substituting it can
// neither invent nor hide a scoping clause.
const nonLiteralPredicatePart = "<expr>"

// userUpdateSite is what the scan concludes about one function that builds a
// users-table UPDATE scoped by an `id = ?` conjunct.
type userUpdateSite struct {
	// bare: the predicate is the scoping clause and nothing else — the shape
	// only scopedUserUpdate/scopedUserUpdateTx may build.
	bare bool
	// concatenated: the predicate was assembled from a concatenation rather
	// than written as one string literal.
	concatenated bool
	// guardBeforeClause: requireUserOwnerID is called, and its call sits before
	// the Where that builds the clause. Order is part of the property — a
	// refusal reached after the query was built refuses nothing.
	guardBeforeClause bool
	// reportsZeroRows: RowsAffected is not merely read but reaches the caller —
	// returned, or tested in a condition that returns. A read whose result is
	// dropped is the silent success this guard is about.
	reportsZeroRows bool
}

// scanUserUpdateSites classifies every function in one Go source that builds a
// users-table UPDATE predicate containing an `id = ?` conjunct. It is separate
// from the test below so the classifier itself can be exercised against
// synthetic sources (TestUserUpdateScopingScanClassifiesEachShape): every
// branch here decides whether a real zero-id hole is reported, and a branch
// that only ever runs against the one healthy file is a branch nothing proves.
//
// opaque names functions whose Where predicate yields no string literal at all,
// so nothing can be concluded about it either way.
func scanUserUpdateSites(path string, source []byte) (sites map[string]userUpdateSite, opaque []string, err error) {
	fileSet := token.NewFileSet()
	parsed, parseErr := parser.ParseFile(fileSet, path, source, parser.SkipObjectResolution)
	if parseErr != nil {
		return nil, nil, parseErr
	}

	sites = map[string]userUpdateSite{}
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Body == nil {
			continue
		}

		site := userUpdateSite{}
		clauseAt := -1
		var unreadable bool
		ast.Inspect(function.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Where" || len(call.Args) == 0 {
				return true
			}
			skeleton, literalParts, concatenated := predicateSkeleton(call.Args[0])
			if literalParts == 0 {
				unreadable = true
				return true
			}
			conjuncts := conjunctSeparator.Split(skeleton, -1)
			scoped := false
			for _, conjunct := range conjuncts {
				if bareIDClause.MatchString(strings.TrimSpace(conjunct)) {
					scoped = true
					break
				}
			}
			if !scoped {
				return true
			}
			if len(conjuncts) == 1 {
				site.bare = true
			}
			if concatenated {
				site.concatenated = true
			}
			if at := int(call.Pos()); clauseAt < 0 || at < clauseAt {
				clauseAt = at
			}
			return true
		})
		if unreadable {
			opaque = append(opaque, function.Name.Name)
		}
		if clauseAt < 0 {
			continue
		}

		guardAt := -1
		ast.Inspect(function.Body, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.Ident:
				if typed.Name == "requireUserOwnerID" {
					if at := int(typed.Pos()); guardAt < 0 || at < guardAt {
						guardAt = at
					}
				}
			case *ast.ReturnStmt:
				for _, result := range typed.Results {
					if mentionsRowsAffected(result) {
						site.reportsZeroRows = true
					}
				}
			case *ast.IfStmt:
				// `if result.RowsAffected == 0 { return Err... }`: the zero-row
				// outcome reaches the caller through the branch rather than
				// through the return expression.
				if mentionsRowsAffected(typed.Cond) && containsReturn(typed.Body) {
					site.reportsZeroRows = true
				}
			}
			return true
		})
		site.guardBeforeClause = guardAt >= 0 && guardAt < clauseAt

		sites[function.Name.Name] = site
	}
	sort.Strings(opaque)
	return sites, opaque, nil
}

// predicateSkeleton flattens a Where predicate expression into the SQL text it
// will produce, with every non-literal part replaced by
// nonLiteralPredicatePart. It reports how many string literals it found (zero
// means nothing can be read off the expression) and whether the predicate was
// concatenated rather than written whole.
func predicateSkeleton(expression ast.Expr) (skeleton string, literalParts int, concatenated bool) {
	switch typed := expression.(type) {
	case *ast.BasicLit:
		if typed.Kind != token.STRING {
			return nonLiteralPredicatePart, 0, false
		}
		unquoted, err := strconv.Unquote(typed.Value)
		if err != nil {
			return nonLiteralPredicatePart, 0, false
		}
		return unquoted, 1, false
	case *ast.ParenExpr:
		return predicateSkeleton(typed.X)
	case *ast.BinaryExpr:
		if typed.Op != token.ADD {
			return nonLiteralPredicatePart, 0, false
		}
		left, leftLiterals, _ := predicateSkeleton(typed.X)
		right, rightLiterals, _ := predicateSkeleton(typed.Y)
		return left + right, leftLiterals + rightLiterals, true
	default:
		return nonLiteralPredicatePart, 0, false
	}
}

func mentionsRowsAffected(node ast.Node) bool {
	found := false
	ast.Inspect(node, func(n ast.Node) bool {
		if selector, ok := n.(*ast.SelectorExpr); ok && selector.Sel.Name == "RowsAffected" {
			found = true
		}
		return !found
	})
	return found
}

func containsReturn(node ast.Node) bool {
	found := false
	ast.Inspect(node, func(n ast.Node) bool {
		if _, ok := n.(*ast.ReturnStmt); ok {
			found = true
		}
		return !found
	})
	return found
}

// unguardedUserUpdateSites names the functions that build the scoping clause
// and satisfy none of the accepted shapes.
func unguardedUserUpdateSites(sites map[string]userUpdateSite) []string {
	var offenders []string
	for name, site := range sites {
		if scopedUserUpdateBuilders[name] {
			// The builders themselves are the one place the bare shape is
			// allowed to appear.
			continue
		}
		if site.bare {
			// A bare `Where("id = ?", userID)` outside the two builders:
			// whatever query it is chained off of (repo.database or a
			// transaction's tx), it built the scoped clause without the
			// zero-id refusal.
			offenders = append(offenders, name)
			continue
		}
		if !site.guardBeforeClause && !site.reportsZeroRows {
			offenders = append(offenders, name)
		}
	}
	sort.Strings(offenders)
	return offenders
}

// TestUserRepositoryUpdatesGoThroughTheScopingHelper is the completeness
// guard the zero-id refusal depends on. The privacy boundary treats an
// absent or zero id as invalid input, never as a wildcard: a raw
// `Model(&models.User{}).Where("id = ?", userID)` — bare, compounded in any
// position of the AND-chain, or concatenated together from pieces — built
// outside a guarded shape would let a zero id reach the database unrefused.
// Two shapes are accepted for a function that builds such a clause:
//
//  1. It is scopedUserUpdate/scopedUserUpdateTx themselves (the bare clause's
//     only builders).
//  2. It calls requireUserOwnerID BEFORE building the query, OR it reports the
//     query's zero-row outcome to its caller (a compare-and-set write whose
//     zero-row result is legitimately ambiguous between "no owner" and
//     "predicate didn't match", and which already surfaces that ambiguity
//     honestly instead of swallowing it).
//
// A function that builds the clause and does neither is a silent-success
// hole: a zero id matches zero rows, the write reports success, and nothing
// downstream notices.
//
// The set of users-table UPDATE builders is derived from the file's own AST
// rather than re-listed: a hand-written mirror of the writers would agree
// with itself while a new writer went unguarded.
func TestUserRepositoryUpdatesGoThroughTheScopingHelper(t *testing.T) {
	const path = "user_repository.go"

	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	sites, opaque, err := scanUserUpdateSites(path, source)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	// A predicate the scan cannot read is not a predicate the scan cleared. It
	// has never occurred in this file; if one appears it has to be made
	// readable rather than silently skipped, which is how the concatenated
	// writers were skipped before.
	if len(opaque) > 0 {
		t.Fatalf("these functions pass a Where predicate the scan cannot read, so it cannot tell whether they scope by id: %v", opaque)
	}

	// Anti-vacuity, by name rather than by population: a count of sites is
	// satisfied by the wrong members, and the previous shape of this scan found
	// a healthy set while missing both concatenated writers entirely.
	for name := range scopedUserUpdateBuilders {
		site, found := sites[name]
		if !found || !site.bare {
			t.Fatalf("the scan did not find %s building the bare scoped WHERE clause: it is not measuring what it claims", name)
		}
	}
	for name, why := range concatenatedPredicateSites {
		site, found := sites[name]
		if !found {
			t.Fatalf("the scan did not find %s among the id-scoped writers (%s): the concatenated-predicate path is unexercised", name, why)
		}
		if !site.concatenated {
			t.Fatalf("%s no longer builds its predicate by concatenation (%s): move the name to a site that does, or the concatenated-predicate path is unexercised", name, why)
		}
	}
	// Each acceptance arm is claimed by a named site, so neither can rot into
	// an arm that accepts everything.
	if !sites["ReleaseWebhookWatermark"].guardBeforeClause {
		t.Fatalf("ReleaseWebhookWatermark must call requireUserOwnerID before it builds the scoped clause: it is the site that holds the guard-first arm")
	}
	if !sites["ClaimWebhookWatermark"].reportsZeroRows {
		t.Fatalf("ClaimWebhookWatermark must report its zero-row outcome to its caller: it is the site that holds the RowsAffected arm")
	}

	if offenders := unguardedUserUpdateSites(sites); len(offenders) > 0 {
		t.Fatalf("these functions build a users-table UPDATE scoped by a raw `id = ?` conjunct without going through scopedUserUpdate/scopedUserUpdateTx, calling requireUserOwnerID first, or reporting the zero-row outcome, so a zero id would reach them unrefused: %v", offenders)
	}
}

// TestUserUpdateScopingScanClassifiesEachShape exercises the classifier against
// synthetic sources. Running it only over the healthy repository proves that
// the file passes, not that the scan would fail anything: every shape the scan
// is supposed to reject has to be shown rejected somewhere, and the repository
// deliberately contains none of them.
func TestUserUpdateScopingScanClassifiesEachShape(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		offender bool
	}{
		{
			name:     "bare clause outside the builders",
			body:     "\tq := repo.database.Model(&models.User{}).Where(\"id = ?\", userID)\n\t_ = q.Update(\"a\", 1).Error",
			offender: true,
		},
		{
			name:     "leading compound clause, no guard and no report",
			body:     "\t_ = repo.database.Model(&models.User{}).Where(\"id = ? AND email = ?\", userID, e).Update(\"a\", 1).Error",
			offender: true,
		},
		{
			name:     "trailing compound clause, no guard and no report",
			body:     "\t_ = repo.database.Model(&models.User{}).Where(\"totp_enabled = ? AND id = ?\", true, userID).Update(\"a\", 1).Error",
			offender: true,
		},
		{
			name:     "concatenated clause, no guard and no report",
			body:     "\t_ = repo.database.Model(&models.User{}).Where(\"id = ? AND \"+col+\" = ?\", userID, v).Update(\"a\", 1).Error",
			offender: true,
		},
		{
			name:     "RowsAffected read but dropped",
			body:     "\tresult := repo.database.Model(&models.User{}).Where(\"id = ? AND email = ?\", userID, e).Update(\"a\", 1)\n\t_ = result.RowsAffected\n\t_ = result.Error",
			offender: true,
		},
		{
			name:     "guard called after the clause is built",
			body:     "\tq := repo.database.Model(&models.User{}).Where(\"id = ? AND email = ?\", userID, e)\n\tif err := requireUserOwnerID(userID); err != nil {\n\t\t_ = err\n\t}\n\t_ = q.Update(\"a\", 1).Error",
			offender: true,
		},
		{
			name:     "guard called before the clause is built",
			body:     "\tif err := requireUserOwnerID(userID); err != nil {\n\t\treturn\n\t}\n\t_ = repo.database.Model(&models.User{}).Where(\"id = ? AND email = ?\", userID, e).Update(\"a\", 1).Error",
			offender: false,
		},
		{
			name:     "concatenated clause guarded before the clause is built",
			body:     "\tif err := requireUserOwnerID(userID); err != nil {\n\t\treturn\n\t}\n\t_ = repo.database.Model(&models.User{}).Where(\"id = ? AND \"+col+\" = ?\", userID, v).Update(\"a\", 1).Error",
			offender: false,
		},
		{
			name:     "zero-row outcome returned",
			body:     "\tresult := repo.database.Model(&models.User{}).Where(\"id = ? AND email = ?\", userID, e).Update(\"a\", 1)\n\tif true {\n\t\treturn result.RowsAffected == 1, result.Error\n\t}",
			offender: false,
		},
		{
			name:     "zero-row outcome reported through a branch",
			body:     "\tresult := repo.database.Model(&models.User{}).Where(\"id = ? AND email = ?\", userID, e).Update(\"a\", 1)\n\tif result.RowsAffected == 0 {\n\t\treturn\n\t}\n\t_ = result.Error",
			offender: false,
		},
		{
			name:     "predicate without an id conjunct is not a site at all",
			body:     "\t_ = repo.database.Model(&models.User{}).Where(\"user_id = ? AND date >= ?\", userID, d).Update(\"a\", 1).Error",
			offender: false,
		},
		{
			name:     "id inequality is not a scoping clause",
			body:     "\t_ = repo.database.Model(&models.User{}).Where(\"email = ? AND id != ?\", e, userID).Update(\"a\", 1).Error",
			offender: false,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			source := []byte("package db\n\nfunc Subject() {\n" + testCase.body + "\n}\n")
			sites, opaque, err := scanUserUpdateSites("synthetic.go", source)
			if err != nil {
				t.Fatalf("scan synthetic source: %v", err)
			}
			if len(opaque) > 0 {
				t.Fatalf("synthetic source read as opaque: %v", opaque)
			}
			offenders := unguardedUserUpdateSites(sites)
			flagged := len(offenders) == 1 && offenders[0] == "Subject"
			if flagged != testCase.offender {
				t.Fatalf("flagged as offender = %v, want %v (sites %+v)", flagged, testCase.offender, sites)
			}
		})
	}
}

// TestUserUpdateScopingScanReportsAnUnreadablePredicate pins the opaque arm: a
// predicate built from something the scan cannot flatten must be reported
// rather than skipped, because skipping is what let the concatenated writers
// through.
func TestUserUpdateScopingScanReportsAnUnreadablePredicate(t *testing.T) {
	source := []byte("package db\n\nfunc Subject() {\n\t_ = repo.database.Model(&models.User{}).Where(predicate, userID).Update(\"a\", 1).Error\n}\n")
	_, opaque, err := scanUserUpdateSites("synthetic.go", source)
	if err != nil {
		t.Fatalf("scan synthetic source: %v", err)
	}
	if len(opaque) != 1 || opaque[0] != "Subject" {
		t.Fatalf("opaque = %v, want [Subject]", opaque)
	}
}
