package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func TestRunUsersCommandListPrintsMinimalUserAuditTable(t *testing.T) {
	t.Parallel()

	databasePath := createCLIUsersDatabase(t)
	createCLIUsersUser(t, databasePath, "owner@example.com", "Owner", models.RoleOwner, true, time.Date(2026, time.March, 1, 10, 0, 0, 0, time.UTC))
	createCLIUsersUser(t, databasePath, "second-owner@example.com", "", models.RoleOwner, false, time.Date(2026, time.March, 2, 11, 0, 0, 0, time.UTC))

	var output bytes.Buffer
	err := runUsersCommand(db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath}, []string{"list"}, strings.NewReader(""), &output)
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
	t.Parallel()

	databasePath := createCLIUsersDatabase(t)
	user := createCLIUsersUser(t, databasePath, "owner@example.com", "Owner", models.RoleOwner, true, time.Now().UTC())
	seedCLIUsersHealthData(t, databasePath, user.ID)

	var output bytes.Buffer
	err := runUsersCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		[]string{"delete", "owner@example.com"},
		strings.NewReader("DELETE\n"),
		&output,
	)
	if err != nil {
		t.Fatalf("runUsersCommand(delete) returned error: %v", err)
	}
	if !strings.Contains(output.String(), "Deleted account owner@example.com") {
		t.Fatalf("expected delete confirmation output, got %q", output.String())
	}

	assertCLIUsersDataCounts(t, databasePath, user.ID, 0, 0, 0)
}

func TestRunUsersCommandDeleteRemovesAccountWithYesFlag(t *testing.T) {
	t.Parallel()

	databasePath := createCLIUsersDatabase(t)
	createCLIUsersUser(t, databasePath, "owner@example.com", "Owner", models.RoleOwner, true, time.Now().UTC())

	var output bytes.Buffer
	err := runUsersCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		[]string{"delete", "owner@example.com", "--yes"},
		strings.NewReader(""),
		&output,
	)
	if err != nil {
		t.Fatalf("runUsersCommand(delete --yes) returned error: %v", err)
	}
	if !strings.Contains(output.String(), "Deleted account owner@example.com") {
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
	database, err := db.OpenSQLite(databasePath)
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

	database, err := db.OpenSQLite(databasePath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	defer sqlDB.Close()

	passwordHash, err := bcrypt.GenerateFromPassword([]byte("StrongPass1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	user := models.User{
		DisplayName:         displayName,
		Email:               strings.ToLower(strings.TrimSpace(email)),
		PasswordHash:        string(passwordHash),
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

	database, err := db.OpenSQLite(databasePath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	defer sqlDB.Close()

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

	database, err := db.OpenSQLite(databasePath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	defer sqlDB.Close()

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

	database, err := db.OpenSQLite(databasePath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	defer sqlDB.Close()

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
