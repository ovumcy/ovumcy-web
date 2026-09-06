package cli

import (
	"bytes"
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func TestRunUsersCommandListPrintsMinimalUserAuditTable(t *testing.T) {
	t.Parallel()

	databasePath := createCLIUsersDatabase(t)
	createCLIUsersUser(t, databasePath, "owner@example.com", "Owner", models.RoleOwner, true, time.Date(2026, time.March, 1, 10, 0, 0, 0, time.UTC))
	createCLIUsersUser(t, databasePath, "second-owner@example.com", "", models.RoleOwner, false, time.Date(2026, time.March, 2, 11, 0, 0, 0, time.UTC))

	var output bytes.Buffer
	err := runUsersCommand(db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath}, []string{"list"}, "", strings.NewReader(""), &output)
	if err != nil {
		t.Fatalf("runUsersCommand(list) returned error: %v", err)
	}

	rendered := output.String()
	if !strings.Contains(rendered, "ID") ||
		!strings.Contains(rendered, "EMAIL") ||
		!strings.Contains(rendered, "ROLE") ||
		!strings.Contains(rendered, "DISPLAY NAME") ||
		!strings.Contains(rendered, "ONBOARDED") ||
		!strings.Contains(rendered, "CREATED AT") {
		t.Fatalf("expected user table header, got %q", rendered)
	}
	if !strings.Contains(rendered, "owner@example.com") || !strings.Contains(rendered, "second-owner@example.com") {
		t.Fatalf("expected both users in output, got %q", rendered)
	}
	if !strings.Contains(rendered, "Owner") || !strings.Contains(rendered, "-") {
		t.Fatalf("expected display name and empty placeholder, got %q", rendered)
	}
	if strings.Contains(rendered, "StrongPass1") {
		t.Fatalf("did not expect password content in output: %q", rendered)
	}
}

func TestRunUsersCommandDeleteRequiresExplicitConfirmation(t *testing.T) {
	t.Parallel()

	databasePath := createCLIUsersDatabase(t)
	user := createCLIUsersUser(t, databasePath, "owner@example.com", "Owner", models.RoleOwner, true, time.Now().UTC())
	seedCLIUsersHealthData(t, databasePath, user.ID)

	var output bytes.Buffer
	err := runUsersCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		[]string{"delete", "owner@example.com"},
		"",
		strings.NewReader("no\n"),
		&output,
	)
	if err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("expected cancellation error, got %v", err)
	}

	remainingUsers := listCLIUserEmails(t, databasePath)
	if len(remainingUsers) != 1 || remainingUsers[0] != "owner@example.com" {
		t.Fatalf("expected user to remain after cancelled delete, got %#v", remainingUsers)
	}
	assertCLIUsersDataCounts(t, databasePath, user.ID, 1, 1, 1)
}

func TestRunUsersCommandDeleteRemovesAccountAndRelatedDataAfterExplicitConfirmation(t *testing.T) {
	databasePath := createCLIUsersDatabase(t)
	user := createCLIUsersUser(t, databasePath, "owner@example.com", "Owner", models.RoleOwner, true, time.Now().UTC())
	seedCLIUsersHealthData(t, databasePath, user.ID)
	fencePath := armOperatorFence(t, databasePath)

	var output bytes.Buffer
	err := runUsersCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		[]string{"delete", "owner@example.com"},
		fencePath,
		strings.NewReader("DELETE\n"),
		&output,
	)
	if err != nil {
		t.Fatalf("runUsersCommand(delete) returned error: %v", err)
	}
	if !strings.Contains(output.String(), `Deleted account "owner@example.com"`) {
		t.Fatalf("expected delete confirmation output, got %q", output.String())
	}

	assertCLIUsersDataCounts(t, databasePath, user.ID, 0, 0, 0)
}

func TestRunUsersCommandDeleteRemovesAccountWithYesFlag(t *testing.T) {
	databasePath := createCLIUsersDatabase(t)
	createCLIUsersUser(t, databasePath, "owner@example.com", "Owner", models.RoleOwner, true, time.Now().UTC())
	fencePath := armOperatorFence(t, databasePath)

	var output bytes.Buffer
	err := runUsersCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		[]string{"delete", "owner@example.com", "--yes"},
		fencePath,
		strings.NewReader(""),
		&output,
	)
	if err != nil {
		t.Fatalf("runUsersCommand(delete --yes) returned error: %v", err)
	}
	if !strings.Contains(output.String(), `Deleted account "owner@example.com"`) {
		t.Fatalf("expected delete confirmation output, got %q", output.String())
	}

	remainingUsers := listCLIUserEmails(t, databasePath)
	if len(remainingUsers) != 0 {
		t.Fatalf("expected account to be deleted, got %#v", remainingUsers)
	}
}

func createCLIUsersDatabase(t *testing.T) string {
	t.Helper()

	databasePath := filepath.Join(t.TempDir(), "cli-users-test.db")
	database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath})
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
	return databasePath
}

func createCLIUsersUser(t *testing.T, databasePath string, email string, displayName string, role string, onboardingCompleted bool, createdAt time.Time) models.User {
	t.Helper()

	database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	passwordHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	user := models.User{
		DisplayName:         displayName,
		Email:               strings.ToLower(strings.TrimSpace(email)),
		PasswordHash:        string(passwordHash),
		LocalAuthEnabled:    true,
		Role:                role,
		OnboardingCompleted: onboardingCompleted,
		CycleLength:         models.DefaultCycleLength,
		PeriodLength:        models.DefaultPeriodLength,
		AutoPeriodFill:      true,
		CreatedAt:           createdAt,
	}
	if err := database.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	return user
}

func listCLIUserEmails(t *testing.T, databasePath string) []string {
	t.Helper()

	database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	users := make([]models.User, 0)
	if err := database.Order("email ASC").Find(&users).Error; err != nil {
		t.Fatalf("list users: %v", err)
	}

	emails := make([]string, 0, len(users))
	for _, user := range users {
		emails = append(emails, user.Email)
	}
	return emails
}

func seedCLIUsersHealthData(t *testing.T, databasePath string, userID uint) {
	t.Helper()

	database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	symptom := models.SymptomType{
		UserID:    userID,
		Name:      "Custom",
		Icon:      "A",
		Color:     "#111111",
		IsBuiltin: false,
	}
	if err := database.Create(&symptom).Error; err != nil {
		t.Fatalf("create symptom: %v", err)
	}

	logEntry := models.DailyLog{
		UserID:     userID,
		Date:       time.Date(2026, time.March, 10, 0, 0, 0, 0, time.UTC),
		IsPeriod:   true,
		Flow:       models.FlowMedium,
		SymptomIDs: []uint{symptom.ID},
		Notes:      "test note",
	}
	if err := database.Create(&logEntry).Error; err != nil {
		t.Fatalf("create daily log: %v", err)
	}
}

func assertCLIUsersDataCounts(t *testing.T, databasePath string, userID uint, wantUsers int64, wantSymptoms int64, wantLogs int64) {
	t.Helper()

	database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	assertCLIUsersCountForModel(t, database, &models.User{}, "id = ?", userID, wantUsers)
	assertCLIUsersCountForModel(t, database, &models.SymptomType{}, "user_id = ?", userID, wantSymptoms)
	assertCLIUsersCountForModel(t, database, &models.DailyLog{}, "user_id = ?", userID, wantLogs)
}

func assertCLIUsersCountForModel(t *testing.T, database *gorm.DB, model any, query string, arg any, want int64) {
	t.Helper()

	var count int64
	if err := database.Model(model).Where(query, arg).Count(&count).Error; err != nil {
		t.Fatalf("count %T: %v", model, err)
	}
	if count != want {
		t.Fatalf("expected %T count %d, got %d", model, want, count)
	}
}

func TestRunUsersCommandCreateProvisionsOwnerFromStdin(t *testing.T) {
	t.Parallel()

	databasePath := createCLIUsersDatabase(t)

	var output bytes.Buffer
	err := runUsersCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		[]string{"create", "Owner@Example.com"},
		"",
		strings.NewReader("StrongPass1\n"),
		&output,
	)
	if err != nil {
		t.Fatalf("runUsersCommand(create) returned error: %v", err)
	}

	rendered := output.String()
	if !strings.Contains(rendered, "Created owner account owner@example.com") {
		t.Fatalf("expected creation confirmation, got %q", rendered)
	}
	if strings.Contains(rendered, "StrongPass1") {
		t.Fatalf("did not expect password in output: %q", rendered)
	}
	if strings.Contains(rendered, "OVUM-") || strings.Contains(rendered, "Recovery code:") {
		t.Fatalf("did not expect a recovery code without opt-in: %q", rendered)
	}
	if !strings.Contains(rendered, "onboarding") {
		t.Fatalf("expected onboarding hint in output, got %q", rendered)
	}

	emails := listCLIUserEmails(t, databasePath)
	if len(emails) != 1 || emails[0] != "owner@example.com" {
		t.Fatalf("expected a single normalized owner, got %#v", emails)
	}
	if countCLISymptomTypes(t, databasePath) == 0 {
		t.Fatal("expected built-in symptoms to be seeded for the new owner")
	}
}

func TestRunUsersCommandCreatePrintsRecoveryCodeOnOptIn(t *testing.T) {
	t.Parallel()

	databasePath := createCLIUsersDatabase(t)

	var output bytes.Buffer
	err := runUsersCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		[]string{"create", "owner@example.com", "--show-recovery-code"},
		"",
		strings.NewReader("StrongPass1\n"),
		&output,
	)
	if err != nil {
		t.Fatalf("runUsersCommand(create --show-recovery-code) returned error: %v", err)
	}

	rendered := output.String()
	if !strings.Contains(rendered, "Recovery code:") || !strings.Contains(rendered, "OVUM-") {
		t.Fatalf("expected recovery code to be printed on opt-in, got %q", rendered)
	}
}

func TestRunUsersCommandCreateAddsSecondHouseholdOwner(t *testing.T) {
	t.Parallel()

	databasePath := createCLIUsersDatabase(t)
	createCLIUsersUser(t, databasePath, "mom@example.com", "Mom", models.RoleOwner, true, time.Now().UTC())

	var output bytes.Buffer
	err := runUsersCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		[]string{"create", "daughter@example.com"},
		"",
		strings.NewReader("StrongPass1\n"),
		&output,
	)
	if err != nil {
		t.Fatalf("runUsersCommand(create second owner) returned error: %v", err)
	}
	if !strings.Contains(output.String(), "Created owner account daughter@example.com") {
		t.Fatalf("expected creation confirmation for the second owner, got %q", output.String())
	}

	emails := listCLIUserEmails(t, databasePath)
	if len(emails) != 2 {
		t.Fatalf("expected two independent owners on the instance, got %#v", emails)
	}
}

func TestRunUsersCommandCreateRejectsDuplicateEmail(t *testing.T) {
	t.Parallel()

	databasePath := createCLIUsersDatabase(t)
	createCLIUsersUser(t, databasePath, "owner@example.com", "Owner", models.RoleOwner, true, time.Now().UTC())

	var output bytes.Buffer
	err := runUsersCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		[]string{"create", "owner@example.com"},
		"",
		strings.NewReader("StrongPass1\n"),
		&output,
	)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("expected duplicate-email error, got %v", err)
	}

	emails := listCLIUserEmails(t, databasePath)
	if len(emails) != 1 {
		t.Fatalf("expected the duplicate not to create a second row, got %#v", emails)
	}
}

func TestRunUsersCommandCreateSkipIfExistsIsIdempotent(t *testing.T) {
	t.Parallel()

	databasePath := createCLIUsersDatabase(t)
	createCLIUsersUser(t, databasePath, "mom@example.com", "Mom", models.RoleOwner, true, time.Now().UTC())

	var output bytes.Buffer
	err := runUsersCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		[]string{"create", "mom@example.com", "--skip-if-exists"},
		"",
		strings.NewReader("StrongPass1\n"),
		&output,
	)
	if err != nil {
		t.Fatalf("runUsersCommand(create --skip-if-exists) must not error on an existing email, got: %v", err)
	}
	if !strings.Contains(output.String(), "already exists — skipping") {
		t.Fatalf("expected skip message, got %q", output.String())
	}

	emails := listCLIUserEmails(t, databasePath)
	if len(emails) != 1 {
		t.Fatalf("expected no duplicate row after skip, got %#v", emails)
	}
}

func TestRunUsersCommandCreateRejectsWeakPassword(t *testing.T) {
	t.Parallel()

	databasePath := createCLIUsersDatabase(t)

	var output bytes.Buffer
	err := runUsersCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		[]string{"create", "owner@example.com"},
		"",
		strings.NewReader("weak\n"),
		&output,
	)
	if err == nil || !strings.Contains(err.Error(), "strength") {
		t.Fatalf("expected password strength error, got %v", err)
	}

	if emails := listCLIUserEmails(t, databasePath); len(emails) != 0 {
		t.Fatalf("expected no account created on weak password, got %#v", emails)
	}
}

func countCLISymptomTypes(t *testing.T, databasePath string) int64 {
	t.Helper()

	database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	var count int64
	if err := database.Model(&models.SymptomType{}).Count(&count).Error; err != nil {
		t.Fatalf("count symptoms: %v", err)
	}
	return count
}

// TestRunUsersCommandSetEmailRestoresTheAccountsTheBootRepairLeavesLockedOut is
// the regression for the two rows AuthEmailRenormalizer counts and leaves
// standing: a duplicate mailbox (SkippedConflicts) and a value it cannot reduce
// to an addr-spec (SkippedUnrenormalizable). Neither can sign in, and neither
// can be addressed by an email-taking command — the strict rule refuses the
// stored string outright, and its bare form resolves the OTHER account. The
// repair therefore runs by id, and it has to leave the health record where it
// is: deleting the row was the only path the runbook could name before, and it
// takes the account's whole history with it.
func TestRunUsersCommandSetEmailRestoresTheAccountsTheBootRepairLeavesLockedOut(t *testing.T) {
	t.Parallel()

	databasePath := createCLIUsersDatabase(t)
	winner := createCLIUsersUser(t, databasePath, "dup@example.com", "Winner", models.RoleOwner, true, time.Date(2026, time.March, 1, 10, 0, 0, 0, time.UTC))
	collided := createCLIUsersUser(t, databasePath, "collided-placeholder@example.com", "Collided", models.RoleOwner, true, time.Date(2026, time.March, 2, 10, 0, 0, 0, time.UTC))
	unparseable := createCLIUsersUser(t, databasePath, "unparseable-placeholder@example.com", "Unparseable", models.RoleOwner, true, time.Date(2026, time.March, 3, 10, 0, 0, 0, time.UTC))
	seedCLIUsersHealthData(t, databasePath, winner.ID)
	seedCLIUsersHealthData(t, databasePath, collided.ID)
	seedCLIUsersHealthData(t, databasePath, unparseable.ID)

	// The two shapes the pre-strict normalizer persisted and the boot repair
	// then had to skip. Written raw: no current code path can produce them.
	const collidedStored = "second account <dup@example.com>"
	const unparseableStored = `"jane doe"@example.com`
	setCLIUsersStoredEmail(t, databasePath, collided.ID, collidedStored)
	setCLIUsersStoredEmail(t, databasePath, unparseable.ID, unparseableStored)

	versionsBefore := map[uint]int{
		collided.ID:    loadCLIUsersRow(t, databasePath, collided.ID).AuthSessionVersion,
		unparseable.ID: loadCLIUsersRow(t, databasePath, unparseable.ID).AuthSessionVersion,
	}

	// The precondition the id form exists for: the stored string cannot be
	// handed to an email-taking command at all. Deliberately without --yes: if
	// the strict rule were ever relaxed, this string would resolve the WINNER
	// account, and a skipped confirmation would erase it before the assertion
	// below could fail. Empty stdin reads as "not DELETE", so the probe stays
	// non-destructive under every outcome.
	if err := runUsersCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		[]string{"delete", collidedStored},
		"",
		strings.NewReader(""),
		&bytes.Buffer{},
	); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("expected the legacy stored form to be refused by the email path, got %v", err)
	}

	repairs := []struct {
		userID   uint
		stored   string
		newEmail string
	}{
		{userID: collided.ID, stored: collidedStored, newEmail: "second@example.com"},
		{userID: unparseable.ID, stored: unparseableStored, newEmail: "jane.doe@example.com"},
	}
	for _, repair := range repairs {
		var output bytes.Buffer
		if err := runUsersCommand(
			db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
			[]string{"set-email", "--id", strconv.FormatUint(uint64(repair.userID), 10), repair.newEmail},
			"",
			strings.NewReader(""),
			&output,
		); err != nil {
			t.Fatalf("set-email for id=%d returned error: %v", repair.userID, err)
		}
		rendered := output.String()
		if !strings.Contains(rendered, strconv.Quote(repair.stored)) || !strings.Contains(rendered, repair.newEmail) {
			t.Fatalf("expected the repair to name both addresses, got %q", rendered)
		}

		// The point of the whole command: the account signs in again, and its
		// health record is still there.
		user, err := authenticateCLIUser(t, databasePath, repair.newEmail, "StrongPass1")
		if err != nil {
			t.Fatalf("expected id=%d to sign in as %s, got %v", repair.userID, repair.newEmail, err)
		}
		if user.ID != repair.userID {
			t.Fatalf("expected %s to resolve id=%d, got id=%d", repair.newEmail, repair.userID, user.ID)
		}
		assertCLIUsersDataCounts(t, databasePath, repair.userID, 1, 1, 1)

		if user.AuthSessionVersion <= versionsBefore[repair.userID] {
			t.Fatalf("expected the re-homing to revoke sessions for id=%d: before=%d after=%d", repair.userID, versionsBefore[repair.userID], user.AuthSessionVersion)
		}
	}

	// The account that won the repair is untouched by either repair — neither
	// its address nor its history moved.
	winnerUser, err := authenticateCLIUser(t, databasePath, "dup@example.com", "StrongPass1")
	if err != nil || winnerUser.ID != winner.ID {
		t.Fatalf("expected dup@example.com to still resolve id=%d, got id=%d (err=%v)", winner.ID, winnerUser.ID, err)
	}
	assertCLIUsersDataCounts(t, databasePath, winner.ID, 1, 1, 1)
}

// TestRunUsersCommandSetEmailReRunWithTheSameAddressRevokesNoSession pins the
// idempotent retry: the second run of a line the operator already ran must not
// write, because the write bumps auth_session_version and would sign the owner
// out of the session they just signed into.
func TestRunUsersCommandSetEmailReRunWithTheSameAddressRevokesNoSession(t *testing.T) {
	t.Parallel()

	databasePath := createCLIUsersDatabase(t)
	user := createCLIUsersUser(t, databasePath, "collided-placeholder@example.com", "Collided", models.RoleOwner, true, time.Now().UTC())
	setCLIUsersStoredEmail(t, databasePath, user.ID, "second account <dup@example.com>")
	idArgument := strconv.FormatUint(uint64(user.ID), 10)

	var first bytes.Buffer
	if err := runUsersCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		[]string{"set-email", "--id", idArgument, "second@example.com"},
		"",
		strings.NewReader(""),
		&first,
	); err != nil {
		t.Fatalf("first set-email returned error: %v", err)
	}
	repaired := loadCLIUsersRow(t, databasePath, user.ID)
	// Anchor: the first run really did revoke, so the second run's stability
	// below is the early return and not a bump that never happens at all.
	if repaired.AuthSessionVersion <= 1 {
		t.Fatalf("expected the repair itself to revoke sessions, version=%d", repaired.AuthSessionVersion)
	}

	var second bytes.Buffer
	if err := runUsersCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		[]string{"set-email", "--id", idArgument, "second@example.com"},
		"",
		strings.NewReader(""),
		&second,
	); err != nil {
		t.Fatalf("re-run returned error: %v", err)
	}
	if !strings.Contains(second.String(), "already answers to second@example.com") {
		t.Fatalf("expected the re-run to report a no-op, got %q", second.String())
	}
	if again := loadCLIUsersRow(t, databasePath, user.ID); again.AuthSessionVersion != repaired.AuthSessionVersion {
		t.Fatalf("a re-run must revoke nothing: before=%d after=%d", repaired.AuthSessionVersion, again.AuthSessionVersion)
	}
}

func TestRunUsersCommandSetEmailRefusesAnAddressAnotherAccountAnswersTo(t *testing.T) {
	t.Parallel()

	databasePath := createCLIUsersDatabase(t)
	winner := createCLIUsersUser(t, databasePath, "dup@example.com", "Winner", models.RoleOwner, true, time.Date(2026, time.March, 1, 10, 0, 0, 0, time.UTC))
	collided := createCLIUsersUser(t, databasePath, "collided-placeholder@example.com", "Collided", models.RoleOwner, true, time.Date(2026, time.March, 2, 10, 0, 0, 0, time.UTC))
	setCLIUsersStoredEmail(t, databasePath, collided.ID, "second account <dup@example.com>")

	err := runUsersCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		[]string{"set-email", "--id", strconv.FormatUint(uint64(collided.ID), 10), "dup@example.com"},
		"",
		strings.NewReader(""),
		&bytes.Buffer{},
	)
	if err == nil || !strings.Contains(err.Error(), "another account") {
		t.Fatalf("expected the taken address to be refused, got %v", err)
	}

	if got := loadCLIUsersRow(t, databasePath, collided.ID).Email; got != "second account <dup@example.com>" {
		t.Fatalf("expected the refused repair to leave the row alone, got %q", got)
	}
	if got := loadCLIUsersRow(t, databasePath, winner.ID).Email; got != "dup@example.com" {
		t.Fatalf("expected the other account untouched, got %q", got)
	}
}

func TestRunUsersCommandSetEmailRejectsADecoratedAddressAndAnUnknownID(t *testing.T) {
	t.Parallel()

	databasePath := createCLIUsersDatabase(t)
	user := createCLIUsersUser(t, databasePath, "owner@example.com", "Owner", models.RoleOwner, true, time.Now().UTC())

	// A decorated address is exactly what the strict login rule refuses, so
	// storing one would only re-create the state this command repairs.
	err := runUsersCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		[]string{"set-email", "--id", strconv.FormatUint(uint64(user.ID), 10), "jane doe <jane@example.com>"},
		"",
		strings.NewReader(""),
		&bytes.Buffer{},
	)
	if err == nil || !strings.Contains(err.Error(), "invalid email address") {
		t.Fatalf("expected a decorated address to be refused, got %v", err)
	}
	if got := loadCLIUsersRow(t, databasePath, user.ID).Email; got != "owner@example.com" {
		t.Fatalf("expected the row untouched after a refused address, got %q", got)
	}

	err = runUsersCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		[]string{"set-email", "--id", "4242", "someone@example.com"},
		"",
		strings.NewReader(""),
		&bytes.Buffer{},
	)
	// The shared wording, naming the id the operator typed: `set-email` used
	// to answer with a third phrasing of its own for the same failure.
	if err == nil || !strings.Contains(err.Error(), "no account carries id 4242") {
		t.Fatalf("expected an unknown id to be refused, got %v", err)
	}
}

func TestRunUsersCommandDeleteByIDConfirmsAgainstTheStoredIdentity(t *testing.T) {
	databasePath := createCLIUsersDatabase(t)
	winner := createCLIUsersUser(t, databasePath, "dup@example.com", "Winner", models.RoleOwner, true, time.Date(2026, time.March, 1, 10, 0, 0, 0, time.UTC))
	collided := createCLIUsersUser(t, databasePath, "collided-placeholder@example.com", "Collided", models.RoleOwner, true, time.Date(2026, time.March, 2, 10, 0, 0, 0, time.UTC))
	seedCLIUsersHealthData(t, databasePath, winner.ID)
	seedCLIUsersHealthData(t, databasePath, collided.ID)
	setCLIUsersStoredEmail(t, databasePath, collided.ID, "second account <dup@example.com>")
	fencePath := armOperatorFence(t, databasePath)

	var output bytes.Buffer
	if err := runUsersCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		[]string{"delete", "--id", strconv.FormatUint(uint64(collided.ID), 10)},
		fencePath,
		strings.NewReader("DELETE\n"),
		&output,
	); err != nil {
		t.Fatalf("delete --id returned error: %v", err)
	}
	if !strings.Contains(output.String(), strconv.Quote("second account <dup@example.com>")) {
		t.Fatalf("expected the confirmation to quote the stored identity, got %q", output.String())
	}

	assertCLIUsersDataCounts(t, databasePath, collided.ID, 0, 0, 0)
	assertCLIUsersDataCounts(t, databasePath, winner.ID, 1, 1, 1)
}

func setCLIUsersStoredEmail(t *testing.T, databasePath string, userID uint, email string) {
	t.Helper()

	database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	if err := database.Model(&models.User{}).Where("id = ?", userID).Update("email", email).Error; err != nil {
		t.Fatalf("store legacy email: %v", err)
	}
}

func loadCLIUsersRow(t *testing.T, databasePath string, userID uint) models.User {
	t.Helper()

	database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	var user models.User
	if err := database.First(&user, userID).Error; err != nil {
		t.Fatalf("load user %d: %v", userID, err)
	}
	return user
}

// authenticateCLIUser drives the real credential path — the same
// NormalizeAuthEmail rule a browser login normalizes under — so "signs in
// again" is measured rather than inferred from the stored bytes.
func authenticateCLIUser(t *testing.T, databasePath string, email string, password string) (models.User, error) {
	t.Helper()

	database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	normalized := services.NormalizeAuthEmail(email)
	if normalized == "" {
		t.Fatalf("test fixture email %q is not a valid sign-in input", email)
	}
	repositories, _ := buildRepositories(database, "")
	return services.NewAuthService(repositories.Users).AuthenticateCredentials(context.Background(), normalized, password)
}
