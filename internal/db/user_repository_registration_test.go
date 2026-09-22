package db

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/models"
	"gorm.io/gorm"
)

func openRegistrationRepositoryForTest(t *testing.T) *UserRepository {
	t.Helper()

	databasePath := filepath.Join(t.TempDir(), "registration-repository.db")
	database, err := OpenDatabase(Config{Driver: DriverSQLite, SQLitePath: databasePath})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})

	return NewUserRepository(database)
}

func TestUserRepositoryCreateTranslatesUniqueViolation(t *testing.T) {
	repo := openRegistrationRepositoryForTest(t)

	first := &models.User{
		Email:            "unique@example.com",
		PasswordHash:     "hash",
		RecoveryCodeHash: "recovery",
		Role:             models.RoleOwner,
		CycleLength:      models.DefaultCycleLength,
		PeriodLength:     models.DefaultPeriodLength,
		AutoPeriodFill:   true,
		CreatedAt:        time.Now().UTC(),
	}
	if err := repo.Create(context.Background(), first); err != nil {
		t.Fatalf("create first user: %v", err)
	}

	second := &models.User{
		Email:            "unique@example.com",
		PasswordHash:     "hash",
		RecoveryCodeHash: "recovery",
		Role:             models.RoleOwner,
		CycleLength:      models.DefaultCycleLength,
		PeriodLength:     models.DefaultPeriodLength,
		AutoPeriodFill:   true,
		CreatedAt:        time.Now().UTC(),
	}
	err := repo.Create(context.Background(), second)
	if err == nil {
		t.Fatal("expected unique violation error")
	}

	var uniqueErr *UniqueConstraintError
	if !errors.As(err, &uniqueErr) {
		t.Fatalf("expected UniqueConstraintError, got %T %v", err, err)
	}
}

func TestUserRepositoryCreateUserWithSymptomsRollsBackOnSeedFailure(t *testing.T) {
	repo := openRegistrationRepositoryForTest(t)

	if err := repo.database.Exec("DROP TABLE symptom_types").Error; err != nil {
		t.Fatalf("drop symptom_types: %v", err)
	}

	user := &models.User{
		Email:            "rollback@example.com",
		PasswordHash:     "hash",
		RecoveryCodeHash: "recovery",
		Role:             models.RoleOwner,
		CycleLength:      models.DefaultCycleLength,
		PeriodLength:     models.DefaultPeriodLength,
		AutoPeriodFill:   true,
		CreatedAt:        time.Now().UTC(),
	}
	symptoms := []models.SymptomType{{
		Name:      "Test",
		Icon:      "✨",
		Color:     "#111111",
		IsBuiltin: true,
	}}

	err := repo.CreateUserWithSymptoms(context.Background(), user, symptoms)
	if err == nil {
		t.Fatal("expected seed write error")
	}

	var seedErr *SymptomSeedError
	if !errors.As(err, &seedErr) {
		t.Fatalf("expected SymptomSeedError, got %T %v", err, err)
	}

	exists, checkErr := repo.ExistsByNormalizedEmail(context.Background(), "rollback@example.com")
	if checkErr != nil {
		t.Fatalf("check rollback user existence: %v", checkErr)
	}
	if exists {
		t.Fatal("expected user insert rollback on symptom seed failure")
	}
}

// TestUserRepositoryCompleteOnboardingRefusesZeroOwner proves CompleteOnboarding
// refuses a zero userID rather than running its writes: Where("id = ?", 0) /
// Where("user_id = ?", 0) ordinarily matches zero rows and returns nil, a
// silent no-op indistinguishable from a completed onboarding.
func TestUserRepositoryCompleteOnboardingRefusesZeroOwner(t *testing.T) {
	repo := openRegistrationRepositoryForTest(t)

	err := repo.CompleteOnboarding(context.Background(), 0, time.Now().UTC(), 5, false)
	if !errors.Is(err, ErrUserOwnerRequired) {
		t.Fatalf("expected ErrUserOwnerRequired, got %v", err)
	}
}

// TestUserRepositoryCompleteOnboardingMarksAnExistingDayAsPeriod covers the
// auto-fill loop's update arm: a day row that already exists on an onboarding
// date is updated rather than inserted a second time.
//
// It deliberately does NOT cover the user_id predicate on that update. The
// entry is read scoped by userID in the same transaction, so removing the
// predicate changes nothing this test can observe and it stays green either
// way — the predicate is pinned by the statement-level
// TestUserRepositoryCompleteOnboardingUpdateIsScopedByOwner below.
func TestUserRepositoryCompleteOnboardingMarksAnExistingDayAsPeriod(t *testing.T) {
	repo := openRegistrationRepositoryForTest(t)
	ctx := context.Background()
	ownerID, startDay, dayID := seedOnboardingOwnerWithExistingDay(t, repo)

	if err := repo.CompleteOnboarding(ctx, ownerID, startDay, 1, true); err != nil {
		t.Fatalf("complete onboarding: %v", err)
	}

	var reloaded models.DailyLog
	if err := repo.database.WithContext(ctx).First(&reloaded, dayID).Error; err != nil {
		t.Fatalf("reload day: %v", err)
	}
	if !reloaded.IsPeriod {
		t.Fatal("expected the onboarding auto-fill to mark the existing day as period")
	}
}

// TestUserRepositoryCompleteOnboardingUpdateIsScopedByOwner watches the SQL
// the auto-fill loop's update arm actually sends. The row it updates is read
// scoped by userID in the same transaction, so dropping the user_id predicate
// changes no outcome a row-level assertion can see; the statement itself is
// the only place the regression back to a primary-key-only Updates shows.
func TestUserRepositoryCompleteOnboardingUpdateIsScopedByOwner(t *testing.T) {
	repo := openRegistrationRepositoryForTest(t)
	ownerID, startDay, _ := seedOnboardingOwnerWithExistingDay(t, repo)

	var dailyLogUpdates []string
	if err := repo.database.Callback().Update().After("gorm:update").Register("test:record_daily_log_updates", func(tx *gorm.DB) {
		if tx.Statement.Table == "daily_logs" {
			dailyLogUpdates = append(dailyLogUpdates, tx.Statement.SQL.String())
		}
	}); err != nil {
		t.Fatalf("register update callback: %v", err)
	}

	if err := repo.CompleteOnboarding(context.Background(), ownerID, startDay, 1, true); err != nil {
		t.Fatalf("complete onboarding: %v", err)
	}

	if len(dailyLogUpdates) != 1 {
		t.Fatalf("expected exactly one daily_logs UPDATE from the auto-fill update arm, got %d: %q", len(dailyLogUpdates), dailyLogUpdates)
	}
	statement := dailyLogUpdates[0]
	where := strings.LastIndex(statement, " WHERE ")
	if where < 0 || !strings.Contains(statement[where:], "user_id") {
		t.Fatalf("expected the onboarding daily_logs UPDATE to be scoped by user_id in its WHERE clause, got %q", statement)
	}
}

// TestUserRepositoryCompleteOnboardingUpdateIsScopedByOwnerInSource pins the
// same predicate where it is written, and that the Updates call's error
// reaches the transaction: a scoped update whose failure is dropped would
// complete an onboarding over a day it never marked.
func TestUserRepositoryCompleteOnboardingUpdateIsScopedByOwnerInSource(t *testing.T) {
	function := userRepositoryMethodForTest(t, "CompleteOnboarding")

	if ifReturningErrorOf(function.Body, isOwnerScopedEntryUpdatesError) == nil {
		t.Fatal("expected CompleteOnboarding to run " +
			`tx.Model(&entry).Where("user_id = ?", userID).Updates(...) and return its error, ` +
			"mirroring DailyLogRepository.Save")
	}
}

// TestUserRepositoryCreateUserWithSymptomsChecksSeedOwnersInSource pins the
// owner check on the one symptom insert in this package that does not go
// through SymptomRepository. CreateUserWithSymptoms stamps user.ID onto the
// seed rows and writes them with its own transaction handle, so the guard on
// SymptomRepository.Create/CreateBatch does not cover it; leaving it uncovered
// would fix the class at every site but this one, which is how the two halves
// drift apart. The id is assigned by the insert one statement earlier, so no
// behavioral test can drive this path to a zero owner — the check is pinned
// where it is written, down to its error reaching the caller before the
// insert runs.
func TestUserRepositoryCreateUserWithSymptomsChecksSeedOwnersInSource(t *testing.T) {
	function := userRepositoryMethodForTest(t, "CreateUserWithSymptoms")

	check := ifReturningErrorOf(function.Body, isSeedOwnerCheck)
	if check == nil {
		t.Fatal("expected CreateUserWithSymptoms to run requireSymptomOwners(prepared) and return its error, " +
			"the same owner check SymptomRepository's own inserts run")
	}

	var insert ast.Node
	ast.Inspect(function.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if ok && selectorName(call.Fun) == "Create" && len(call.Args) == 1 && isAddressOfIdent(call.Args[0], "prepared") {
			insert = call
		}
		return insert == nil
	})
	if insert == nil {
		t.Fatal("seed insert tx.Create(&prepared) not found in CreateUserWithSymptoms; update this guard")
	}
	if check.Pos() > insert.Pos() {
		t.Fatal("expected the owner check to run before the seed insert, not after it")
	}
}

// TestIfReturningErrorOfClassifiesEachShape keeps the source guards above from
// passing on a shape that runs the call but loses its error.
func TestIfReturningErrorOfClassifiesEachShape(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		returned bool
	}{
		{"returned wrapped", `if err := requireSymptomOwners(prepared); err != nil { return &SymptomSeedError{Err: err} }`, true},
		{"returned bare", `if err := requireSymptomOwners(prepared); err != nil { return err }`, true},
		{"discarded", `_ = requireSymptomOwners(prepared)`, false},
		{"called as a statement", `requireSymptomOwners(prepared)`, false},
		{"swallowed by return nil", `if err := requireSymptomOwners(prepared); err != nil { return nil }`, false},
		{"no return in the branch", `if err := requireSymptomOwners(prepared); err != nil { println(err) }`, false},
		{"inverted condition", `if err := requireSymptomOwners(prepared); err == nil { return err }`, false},
		{"other rows checked", `if err := requireSymptomOwners(other); err != nil { return err }`, false},
		{"another error returned", `if err := requireSymptomOwners(prepared); err != nil { return errOther }`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file, err := parser.ParseFile(token.NewFileSet(), "synthetic.go", "package p\nfunc f() error {\n"+tc.body+"\nreturn nil\n}\n", 0)
			if err != nil {
				t.Fatalf("parse synthetic source: %v", err)
			}
			body := file.Decls[0].(*ast.FuncDecl).Body
			if got := ifReturningErrorOf(body, isSeedOwnerCheck) != nil; got != tc.returned {
				t.Fatalf("classified returned=%v, want %v for %s", got, tc.returned, tc.body)
			}
		})
	}
}

func seedOnboardingOwnerWithExistingDay(t *testing.T, repo *UserRepository) (ownerID uint, startDay time.Time, dayID uint) {
	t.Helper()
	ctx := context.Background()

	owner := &models.User{
		Email:            "onboarding-owner@example.com",
		PasswordHash:     "hash",
		RecoveryCodeHash: "recovery",
		Role:             models.RoleOwner,
		CycleLength:      models.DefaultCycleLength,
		PeriodLength:     models.DefaultPeriodLength,
		AutoPeriodFill:   true,
		CreatedAt:        time.Now().UTC(),
	}
	if err := repo.Create(ctx, owner); err != nil {
		t.Fatalf("create owner: %v", err)
	}

	startDay = time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)
	existing := models.DailyLog{
		UserID:        owner.ID,
		Date:          startDay,
		IsPeriod:      false,
		Flow:          models.FlowNone,
		SexActivity:   models.SexActivityNone,
		CervicalMucus: models.CervicalMucusNone,
		PregnancyTest: models.PregnancyTestNone,
		SymptomIDs:    []uint{},
	}
	if err := repo.database.WithContext(ctx).Create(&existing).Error; err != nil {
		t.Fatalf("seed existing day: %v", err)
	}
	return owner.ID, startDay, existing.ID
}

func userRepositoryMethodForTest(t *testing.T, name string) *ast.FuncDecl {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), "user_repository.go", nil, 0)
	if err != nil {
		t.Fatalf("parse the user repository source: %v", err)
	}
	for _, decl := range file.Decls {
		function, ok := decl.(*ast.FuncDecl)
		if ok && function.Recv != nil && function.Name.Name == name {
			return function
		}
	}
	t.Fatalf("method not found in the user repository source: %s", name)
	return nil
}

// ifReturningErrorOf finds `if err := <call>; err != nil { return ...err... }`
// where matches accepts <call>: the call runs and the branch hands its error,
// not some other value, back to the caller.
func ifReturningErrorOf(root ast.Node, matches func(ast.Expr) bool) *ast.IfStmt {
	var found *ast.IfStmt
	ast.Inspect(root, func(node ast.Node) bool {
		if found != nil {
			return false
		}
		stmt, ok := node.(*ast.IfStmt)
		if !ok {
			return true
		}
		assign, ok := stmt.Init.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 || !matches(assign.Rhs[0]) {
			return true
		}
		errIdent, ok := assign.Lhs[0].(*ast.Ident)
		if !ok || errIdent.Name == "_" {
			return true
		}
		cond, ok := stmt.Cond.(*ast.BinaryExpr)
		if !ok || cond.Op != token.NEQ || !isIdent(cond.X, errIdent.Name) || !isIdent(cond.Y, "nil") {
			return true
		}
		for _, inner := range stmt.Body.List {
			if ret, ok := inner.(*ast.ReturnStmt); ok && returnMentions(ret, errIdent.Name) {
				found = stmt
				return false
			}
		}
		return true
	})
	return found
}

func returnMentions(ret *ast.ReturnStmt, name string) bool {
	mentioned := false
	for _, result := range ret.Results {
		ast.Inspect(result, func(node ast.Node) bool {
			if isIdent(node, name) {
				mentioned = true
			}
			return !mentioned
		})
	}
	return mentioned
}

func isSeedOwnerCheck(expr ast.Expr) bool {
	call, ok := expr.(*ast.CallExpr)
	return ok && isIdent(call.Fun, "requireSymptomOwners") && len(call.Args) == 1 && isIdent(call.Args[0], "prepared")
}

// isOwnerScopedEntryUpdatesError matches
// tx.Model(&entry).Where("user_id = ?", userID).Updates(...).Error.
func isOwnerScopedEntryUpdatesError(expr ast.Expr) bool {
	errorField, ok := expr.(*ast.SelectorExpr)
	if !ok || errorField.Sel.Name != "Error" {
		return false
	}
	updates, ok := errorField.X.(*ast.CallExpr)
	if !ok || selectorName(updates.Fun) != "Updates" {
		return false
	}
	where, ok := updates.Fun.(*ast.SelectorExpr).X.(*ast.CallExpr)
	if !ok || selectorName(where.Fun) != "Where" || len(where.Args) != 2 || !isIdent(where.Args[1], "userID") {
		return false
	}
	predicate, ok := where.Args[0].(*ast.BasicLit)
	if !ok || predicate.Kind != token.STRING {
		return false
	}
	unquoted, err := strconv.Unquote(predicate.Value)
	if err != nil || strings.Join(strings.Fields(unquoted), "") != "user_id=?" {
		return false
	}
	model, ok := where.Fun.(*ast.SelectorExpr).X.(*ast.CallExpr)
	return ok && selectorName(model.Fun) == "Model" && len(model.Args) == 1 && isAddressOfIdent(model.Args[0], "entry")
}

func selectorName(expr ast.Expr) string {
	if selector, ok := expr.(*ast.SelectorExpr); ok {
		return selector.Sel.Name
	}
	return ""
}

func isAddressOfIdent(expr ast.Expr, name string) bool {
	unary, ok := expr.(*ast.UnaryExpr)
	return ok && unary.Op == token.AND && isIdent(unary.X, name)
}

func isIdent(node ast.Node, name string) bool {
	ident, ok := node.(*ast.Ident)
	return ok && ident.Name == name
}
