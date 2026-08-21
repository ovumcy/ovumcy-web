package db

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
)

// Shown-once reveal marks (migration 036). users.recovery_code_revealed_at and
// users.calendar_feed_revealed_at are what make each secret's reveal single use:
// the reveal page claims the mark with a compare-and-set, and every UPDATE that
// mints a fresh secret NULLs it in the same statement so the new secret arrives
// with its reveal armed.
//
// The claim tests below pin the mechanism, and the sweep at the bottom pins the
// half a per-method test cannot: that no FUTURE mint forgets to re-arm.

func openRevealMarkRepoForTest(t *testing.T) *UserRepository {
	t.Helper()
	database := openSQLiteForMigrationBootstrapTest(t, filepath.Join(t.TempDir(), "reveal-mark.db"))
	return NewUserRepository(database)
}

func createUserForRevealMarkTest(t *testing.T, repo *UserRepository, email string) models.User {
	t.Helper()
	user := models.User{
		Email:               email,
		PasswordHash:        "hash",
		RecoveryCodeHash:    "recovery",
		Role:                models.RoleOwner,
		LocalAuthEnabled:    true,
		OnboardingCompleted: true,
		CycleLength:         28,
		PeriodLength:        5,
		CreatedAt:           time.Now().UTC(),
	}
	if err := repo.Create(context.Background(), &user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	return user
}

func reloadUserForRevealMarkTest(t *testing.T, repo *UserRepository, userID uint) models.User {
	t.Helper()
	var reloaded models.User
	if err := repo.database.First(&reloaded, userID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	return reloaded
}

// TestClaimRecoveryCodeRevealIsSingleUsePerMint walks the whole lifecycle in one
// test because the interesting property is the sequence: a fresh account arms
// exactly one reveal, the second claim loses, and re-minting the code arms
// exactly one more. Asserting only the refusal would pass against a claim that
// never succeeds.
func TestClaimRecoveryCodeRevealIsSingleUsePerMint(t *testing.T) {
	repo := openRevealMarkRepoForTest(t)
	user := createUserForRevealMarkTest(t, repo, "recovery-mark@example.com")
	ctx := context.Background()

	if mark := reloadUserForRevealMarkTest(t, repo, user.ID).RecoveryCodeRevealedAt; mark != nil {
		t.Fatalf("a fresh account must start with its recovery reveal armed, got %v", mark)
	}

	claimed, err := repo.ClaimRecoveryCodeReveal(ctx, user.ID, time.Now())
	if err != nil {
		t.Fatalf("ClaimRecoveryCodeReveal: %v", err)
	}
	if !claimed {
		t.Fatal("expected the first claim to consume the reveal")
	}
	if mark := reloadUserForRevealMarkTest(t, repo, user.ID).RecoveryCodeRevealedAt; mark == nil {
		t.Fatal("expected the consumed reveal to be recorded on the row")
	}

	replayed, err := repo.ClaimRecoveryCodeReveal(ctx, user.ID, time.Now())
	if err != nil {
		t.Fatalf("ClaimRecoveryCodeReveal replay: %v", err)
	}
	if replayed {
		t.Fatal("expected a second claim to lose against the mark already set")
	}

	if err := repo.UpdateRecoveryCodeHashAndRevokeSessions(ctx, user.ID, "rotated-hash"); err != nil {
		t.Fatalf("UpdateRecoveryCodeHashAndRevokeSessions: %v", err)
	}
	if mark := reloadUserForRevealMarkTest(t, repo, user.ID).RecoveryCodeRevealedAt; mark != nil {
		t.Fatalf("a minted recovery code must arrive with its reveal re-armed, got %v", mark)
	}
	rearmed, err := repo.ClaimRecoveryCodeReveal(ctx, user.ID, time.Now())
	if err != nil {
		t.Fatalf("ClaimRecoveryCodeReveal after mint: %v", err)
	}
	if !rearmed {
		t.Fatal("expected the reveal of a freshly minted code to be claimable")
	}
}

// TestClaimCalendarFeedRevealIsSingleUsePerMint is the feed half of the guard
// above, with SaveCalendarFeedToken (generate AND rotate both go through it) as
// the mint that re-arms.
func TestClaimCalendarFeedRevealIsSingleUsePerMint(t *testing.T) {
	repo := openRevealMarkRepoForTest(t)
	user := createUserForRevealMarkTest(t, repo, "feed-mark@example.com")
	ctx := context.Background()

	mint := func(selector string) {
		t.Helper()
		if err := repo.SaveCalendarFeedToken(ctx, user.ID, models.CalendarFeedTokenColumns{
			Selector:     selector,
			VerifierHash: "$2a$10$hash",
			VerifierMAC:  "mac",
		}); err != nil {
			t.Fatalf("SaveCalendarFeedToken: %v", err)
		}
	}

	mint("selector-one")
	if mark := reloadUserForRevealMarkTest(t, repo, user.ID).CalendarFeedRevealedAt; mark != nil {
		t.Fatalf("a minted feed token must arrive with its reveal armed, got %v", mark)
	}

	claimed, err := repo.ClaimCalendarFeedReveal(ctx, user.ID, time.Now())
	if err != nil {
		t.Fatalf("ClaimCalendarFeedReveal: %v", err)
	}
	if !claimed {
		t.Fatal("expected the first claim to consume the reveal")
	}

	replayed, err := repo.ClaimCalendarFeedReveal(ctx, user.ID, time.Now())
	if err != nil {
		t.Fatalf("ClaimCalendarFeedReveal replay: %v", err)
	}
	if replayed {
		t.Fatal("expected a second claim to lose against the mark already set")
	}

	mint("selector-two")
	rearmed, err := repo.ClaimCalendarFeedReveal(ctx, user.ID, time.Now())
	if err != nil {
		t.Fatalf("ClaimCalendarFeedReveal after rotate: %v", err)
	}
	if !rearmed {
		t.Fatal("expected a rotate to re-arm the reveal of the new subscribe URL")
	}
}

// TestRevealClaimsAreScopedToTheirOwner pins the predicate that keeps one
// owner's claim off another owner's row on a household instance: the compare-
// and-set carries `id = ?`, so claiming for one account leaves the other's mark
// armed, and a claim naming no account (the zero id) matches no row at all
// rather than consuming an arbitrary one.
func TestRevealClaimsAreScopedToTheirOwner(t *testing.T) {
	repo := openRevealMarkRepoForTest(t)
	ownerA := createUserForRevealMarkTest(t, repo, "reveal-scope-a@example.com")
	ownerB := createUserForRevealMarkTest(t, repo, "reveal-scope-b@example.com")
	ctx := context.Background()

	if _, err := repo.ClaimRecoveryCodeReveal(ctx, ownerA.ID, time.Now()); err != nil {
		t.Fatalf("ClaimRecoveryCodeReveal: %v", err)
	}
	if _, err := repo.ClaimCalendarFeedReveal(ctx, ownerA.ID, time.Now()); err != nil {
		t.Fatalf("ClaimCalendarFeedReveal: %v", err)
	}

	reloadedB := reloadUserForRevealMarkTest(t, repo, ownerB.ID)
	if reloadedB.RecoveryCodeRevealedAt != nil || reloadedB.CalendarFeedRevealedAt != nil {
		t.Fatal("one owner's reveal must never consume another owner's mark")
	}

	claimed, err := repo.ClaimRecoveryCodeReveal(ctx, 0, time.Now())
	if err != nil {
		t.Fatalf("ClaimRecoveryCodeReveal with a zero id: %v", err)
	}
	if claimed {
		t.Fatal("a claim naming no account must consume nothing")
	}
	if reloadUserForRevealMarkTest(t, repo, ownerB.ID).RecoveryCodeRevealedAt != nil {
		t.Fatal("a claim naming no account must not touch any row")
	}
}

// closeRevealMarkRepoHandle closes the *sql.DB under a reveal-mark repository so
// the next statement fails at the driver — the one way to reach the claim
// methods' error branch without a double standing in for the database.
func closeRevealMarkRepoHandle(t *testing.T, repo *UserRepository) {
	t.Helper()

	sqlDB, err := repo.database.DB()
	if err != nil {
		t.Fatalf("database.DB() unexpected error: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close sql db: %v", err)
	}
}

// TestClaimRecoveryCodeRevealFailsClosedWhenTheUpdateErrors covers the error
// branch of the compare-and-set. A claim that could not run at all must report
// the failure AND read as false: the caller shows the recovery code on true and
// treats it as "this call consumed the reveal", so an errored claim reporting
// true would hand out a shown-once secret while the mark stayed armed — the
// single-use property would be gone, and nothing downstream could tell.
func TestClaimRecoveryCodeRevealFailsClosedWhenTheUpdateErrors(t *testing.T) {
	repo := openRevealMarkRepoForTest(t)
	user := createUserForRevealMarkTest(t, repo, "recovery-mark-closed@example.com")
	closeRevealMarkRepoHandle(t, repo)

	claimed, err := repo.ClaimRecoveryCodeReveal(context.Background(), user.ID, time.Now())
	if err == nil {
		t.Fatal("expected ClaimRecoveryCodeReveal against a closed database to surface an error")
	}
	if claimed {
		t.Fatal("a claim that errored must never read as the call that consumed the reveal")
	}
}

// TestClaimCalendarFeedRevealFailsClosedWhenTheUpdateErrors is the feed half of
// the guard above, and carries the same reasoning about the subscribe URL.
func TestClaimCalendarFeedRevealFailsClosedWhenTheUpdateErrors(t *testing.T) {
	repo := openRevealMarkRepoForTest(t)
	user := createUserForRevealMarkTest(t, repo, "feed-mark-closed@example.com")
	closeRevealMarkRepoHandle(t, repo)

	claimed, err := repo.ClaimCalendarFeedReveal(context.Background(), user.ID, time.Now())
	if err == nil {
		t.Fatal("expected ClaimCalendarFeedReveal against a closed database to surface an error")
	}
	if claimed {
		t.Fatal("a claim that errored must never read as the call that consumed the reveal")
	}
}

// revealMarkPairings maps the column a MINT writes to the reveal mark that mint
// has to re-arm in the same statement.
var revealMarkPairings = map[string]string{
	"recovery_code_hash":     "recovery_code_revealed_at",
	"calendar_feed_selector": "calendar_feed_revealed_at",
}

// TestEveryRecoveryCodeMintClearsItsRevealMark sweeps THIS FILE'S source for
// every `Updates(map[string]any{...})` literal that mints a shown-once secret,
// and requires each to NULL the matching reveal mark in the same map.
//
// Per-method tests prove the three mints that exist today re-arm; they say
// nothing about the fourth. A mint that forgets leaves the owner a secret whose
// reveal is already marked consumed — a fix at N of N+1 sites, where the
// unconverted site is the one the next reader will copy. Deriving the set from
// the source rather than re-listing it means a method added later is judged too.
//
// Only a MINT re-arms: a write that NULLs the column (a revoke, a force-clear,
// the clear-data reset) leaves the mark standing on purpose, because re-arming
// the reveal of a token that no longer resolves would only make a retained
// sealed cookie presentable again. The sweep therefore looks at the VALUE, and
// its own fixtures below prove it can answer both ways.
func TestEveryRecoveryCodeMintClearsItsRevealMark(t *testing.T) {
	assertRevealMarkSweepAnswersBothWays(t)

	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, "user_repository.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse user_repository.go: %v", err)
	}

	mints := 0
	ast.Inspect(parsed, func(node ast.Node) bool {
		literal, ok := node.(*ast.CompositeLit)
		if !ok {
			return true
		}
		written, clearedToNil := writtenColumns(literal)
		for column, mark := range revealMarkPairings {
			if !written[column] || clearedToNil[column] {
				continue
			}
			mints++
			if !written[mark] {
				t.Errorf(
					"%s: an UPDATE writing %s mints a shown-once secret and must NULL %s in the same map, so the new secret arrives with its one-time reveal armed",
					fileSet.Position(literal.Pos()), column, mark,
				)
			}
		}
		return true
	})

	if mints == 0 {
		t.Fatal("the sweep found no mint at all — it is measuring nothing, so the pairing it claims to enforce is unproven")
	}
}

// writtenColumns reduces an `Updates(map[string]any{...})` literal to the set of
// column names it writes, plus the subset written as a literal `nil` — the
// distinction between minting a secret and clearing one.
func writtenColumns(literal *ast.CompositeLit) (written map[string]bool, clearedToNil map[string]bool) {
	written = make(map[string]bool)
	clearedToNil = make(map[string]bool)
	for _, element := range literal.Elts {
		pair, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := pair.Key.(*ast.BasicLit)
		if !ok || key.Kind != token.STRING {
			continue
		}
		column := strings.Trim(key.Value, `"`)
		written[column] = true
		if identifier, isIdentifier := pair.Value.(*ast.Ident); isIdentifier && identifier.Name == "nil" {
			clearedToNil[column] = true
		}
	}
	return written, clearedToNil
}

// assertRevealMarkSweepAnswersBothWays anchors the sweep on fixtures it owns:
// one map that must read as a mint and one that must read as a clear. Without
// them the sweep would report success just as loudly if writtenColumns stopped
// recognising anything at all.
func assertRevealMarkSweepAnswersBothWays(t *testing.T) {
	t.Helper()

	mint := parseUpdatesLiteralForTest(t, `map[string]any{"calendar_feed_selector": columns.Selector}`)
	if written, cleared := writtenColumns(mint); !written["calendar_feed_selector"] || cleared["calendar_feed_selector"] {
		t.Fatal("the sweep must read a non-nil column value as a mint")
	}
	revoke := parseUpdatesLiteralForTest(t, `map[string]any{"calendar_feed_selector": nil}`)
	if written, cleared := writtenColumns(revoke); !written["calendar_feed_selector"] || !cleared["calendar_feed_selector"] {
		t.Fatal("the sweep must read a nil column value as a clear")
	}
}

func parseUpdatesLiteralForTest(t *testing.T, source string) *ast.CompositeLit {
	t.Helper()

	expression, err := parser.ParseExpr(source)
	if err != nil {
		t.Fatalf("parse fixture %q: %v", source, err)
	}
	literal, ok := expression.(*ast.CompositeLit)
	if !ok {
		t.Fatalf("fixture %q is not a composite literal", source)
	}
	return literal
}
