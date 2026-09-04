package cli

import (
	"bytes"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
)

func validLinkOIDCIdentityConfig() security.OIDCConfig {
	return security.OIDCConfig{
		Enabled:      true,
		IssuerURL:    "https://idp.example.com",
		ClientID:     "client-id",
		ClientSecret: "client-secret",
		RedirectURL:  "https://app.example.com/auth/oidc/callback",
	}
}

func createCLILinkOIDCUser(t *testing.T, databasePath string, email string) models.User {
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

	user := models.User{
		Email:               strings.ToLower(strings.TrimSpace(email)),
		LocalAuthEnabled:    true,
		Role:                models.RoleOwner,
		OnboardingCompleted: true,
		CycleLength:         28,
		PeriodLength:        5,
		AutoPeriodFill:      true,
		CreatedAt:           time.Now().UTC(),
	}
	if err := database.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	return user
}

func loadCLILinkedOIDCIdentity(t *testing.T, databasePath string, userID uint) (models.OIDCIdentity, bool) {
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

	var identity models.OIDCIdentity
	result := database.Where("user_id = ?", userID).First(&identity)
	if result.Error != nil {
		return models.OIDCIdentity{}, false
	}
	return identity, true
}

// TestRunLinkOIDCIdentityCommandLinksByID pins (d)'s positive half: the CLI
// command addresses the account by --id (the same addressing reset-password
// gained in PR #699) and persists exactly the (issuer, subject) binding the
// settings step-up and the (now closed) public route would have created.
func TestRunLinkOIDCIdentityCommandLinksByID(t *testing.T) {
	t.Parallel()

	databasePath := filepath.Join(t.TempDir(), "cli-link-oidc-id.db")
	user := createCLILinkOIDCUser(t, databasePath, "cli-link-by-id@example.com")

	var output bytes.Buffer
	err := runLinkOIDCIdentityCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		validLinkOIDCIdentityConfig(),
		[]string{"--id", strconv.FormatUint(uint64(user.ID), 10), "--issuer", "https://idp.example.com", "--subject", "cli-linked-subject"},
		&output,
	)
	if err != nil {
		t.Fatalf("runLinkOIDCIdentityCommand returned error: %v", err)
	}
	if !strings.Contains(output.String(), "Linked OIDC identity") {
		t.Fatalf("expected confirmation output, got %q", output.String())
	}

	identity, found := loadCLILinkedOIDCIdentity(t, databasePath, user.ID)
	if !found {
		t.Fatal("expected an oidc_identities row for the account")
	}
	if identity.Issuer != "https://idp.example.com" || identity.Subject != "cli-linked-subject" {
		t.Fatalf("unexpected persisted identity: %+v", identity)
	}
}

// TestRunLinkOIDCIdentityCommandRefusesUnknownID pins (d)'s negative half: an
// id that names no account is refused with a message pointing at `users list`,
// never a silent no-op.
func TestRunLinkOIDCIdentityCommandRefusesUnknownID(t *testing.T) {
	t.Parallel()

	databasePath := filepath.Join(t.TempDir(), "cli-link-oidc-bad-id.db")
	// Force migrations without creating any account.
	createCLILinkOIDCDatabase(t, databasePath)

	err := runLinkOIDCIdentityCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		validLinkOIDCIdentityConfig(),
		[]string{"--id", "999999", "--issuer", "https://idp.example.com", "--subject", "cli-linked-subject"},
		nil,
	)
	if err == nil {
		t.Fatal("expected an error for an unknown account id")
	}
	if !strings.Contains(err.Error(), "999999") {
		t.Fatalf("expected the error to name the unresolved id, got %v", err)
	}
}

func createCLILinkOIDCDatabase(t *testing.T, databasePath string) {
	t.Helper()
	database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	_ = sqlDB.Close()
}

// TestRunLinkOIDCIdentityCommandRefusesWhenOIDCDisabled pins that the command
// cannot mint a binding a real sign-in could never reach: with OIDC disabled
// on the resolved config, it refuses before ever touching the database.
func TestRunLinkOIDCIdentityCommandRefusesWhenOIDCDisabled(t *testing.T) {
	t.Parallel()

	databasePath := filepath.Join(t.TempDir(), "cli-link-oidc-disabled.db")
	user := createCLILinkOIDCUser(t, databasePath, "cli-link-disabled@example.com")

	err := runLinkOIDCIdentityCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		security.OIDCConfig{Enabled: false},
		[]string{"--id", strconv.FormatUint(uint64(user.ID), 10), "--issuer", "https://idp.example.com", "--subject", "cli-linked-subject"},
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "OIDC is not enabled") {
		t.Fatalf("expected an OIDC-disabled refusal, got %v", err)
	}
	if _, found := loadCLILinkedOIDCIdentity(t, databasePath, user.ID); found {
		t.Fatal("did not expect any identity to be persisted while OIDC is disabled")
	}
}

// TestRunLinkOIDCIdentityCommandRefusesAnIssuerMismatch guards the likeliest
// operator typo: an issuer that does not match OIDC_ISSUER_URL would create a
// row no future sign-in could ever match (the login path resolves by the
// EXACT issuer string the ID token carries).
func TestRunLinkOIDCIdentityCommandRefusesAnIssuerMismatch(t *testing.T) {
	t.Parallel()

	databasePath := filepath.Join(t.TempDir(), "cli-link-oidc-issuer-mismatch.db")
	user := createCLILinkOIDCUser(t, databasePath, "cli-link-mismatch@example.com")

	err := runLinkOIDCIdentityCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		validLinkOIDCIdentityConfig(),
		[]string{"--id", strconv.FormatUint(uint64(user.ID), 10), "--issuer", "https://not-the-configured-issuer.example", "--subject", "cli-linked-subject"},
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "does not match the configured OIDC_ISSUER_URL") {
		t.Fatalf("expected an issuer-mismatch refusal, got %v", err)
	}
	if _, found := loadCLILinkedOIDCIdentity(t, databasePath, user.ID); found {
		t.Fatal("did not expect any identity to be persisted on an issuer mismatch")
	}
}

func TestParseLinkOIDCIdentityArgsUsageErrors(t *testing.T) {
	t.Parallel()

	cases := [][]string{
		nil,                   // no address at all
		{"owner@example.com"}, // missing --issuer/--subject
		{"owner@example.com", "--issuer", "https://idp.example.com"},                                  // missing --subject
		{"--id", "1", "owner@example.com", "--issuer", "https://idp.example.com", "--subject", "sub"}, // both address forms
		{"--issuer", "https://idp.example.com", "--subject", "sub"},                                   // no address at all
	}
	for _, args := range cases {
		if _, err := parseLinkOIDCIdentityArgs(args); err == nil {
			t.Fatalf("expected a usage error for args %#v", args)
		}
	}
}
