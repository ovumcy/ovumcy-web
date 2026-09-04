package cli

import (
	"bytes"
	"errors"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/security"
	"github.com/ovumcy/ovumcy-web/internal/services"
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
		{"--id", "1", "--id", "2", "--issuer", "https://idp.example.com", "--subject", "sub"},         // duplicate --id
		{"--id", "abc", "--issuer", "https://idp.example.com", "--subject", "sub"},                    // non-numeric --id
		{"owner@example.com", "--issuer", "a", "--issuer", "b", "--subject", "sub"},                   // duplicate --issuer
		{"owner@example.com", "--issuer", "a", "--subject", "b", "--subject", "c"},                    // duplicate --subject
		{"owner@example.com", "--issuer", "https://idp.example.com", "--subject"},                     // --subject missing its value
		{"owner@example.com", "--issuer", "https://idp.example.com", "--subject", ""},                 // --subject blank value
		{"owner@example.com", "--issuer="},                                                            // --issuer= with nothing after it
		{"owner@example.com", "--bogus-flag"},                                                         // unknown flag
		{"owner@example.com", "second@example.com", "--issuer", "a", "--subject", "b"},                // two positional addresses
	}
	for _, args := range cases {
		if _, err := parseLinkOIDCIdentityArgs(args); err == nil {
			t.Fatalf("expected a usage error for args %#v", args)
		}
	}
}

// TestParseLinkOIDCIdentityArgsSkipsBlankArgsAndAcceptsBothFlagForms pins the
// two things TestParseLinkOIDCIdentityArgsUsageErrors cannot: that a stray
// blank argument (a shell-quoting artifact) is skipped rather than treated as
// an empty positional address, and that `--name=value` and `--name value` are
// both accepted for --issuer and --subject.
func TestParseLinkOIDCIdentityArgsSkipsBlankArgsAndAcceptsBothFlagForms(t *testing.T) {
	t.Parallel()

	opts, err := parseLinkOIDCIdentityArgs([]string{"", "owner@example.com", "--issuer=https://idp.example.com", "", "--subject", "sub-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if opts.email != "owner@example.com" || opts.issuer != "https://idp.example.com" || opts.subject != "sub-1" {
		t.Fatalf("unexpected parsed options: %+v", opts)
	}
}

func TestParseLinkOIDCIdentityStringFlagBranches(t *testing.T) {
	t.Parallel()

	if _, _, err := parseLinkOIDCIdentityStringFlag([]string{"--issuer"}, 0, "--issuer", linkOIDCIdentityUsage); err == nil {
		t.Fatal("expected an error when the space-separated form has no following value")
	}
	if _, _, err := parseLinkOIDCIdentityStringFlag([]string{"--issuer", ""}, 0, "--issuer", linkOIDCIdentityUsage); err == nil {
		t.Fatal("expected an error when the space-separated value is blank")
	}
	if _, _, err := parseLinkOIDCIdentityStringFlag([]string{"--issuer="}, 0, "--issuer", linkOIDCIdentityUsage); err == nil {
		t.Fatal("expected an error when the --name= form carries nothing after the equals sign")
	}
	value, consumed, err := parseLinkOIDCIdentityStringFlag([]string{"--issuer=https://idp.example.com"}, 0, "--issuer", linkOIDCIdentityUsage)
	if err != nil || value != "https://idp.example.com" || consumed != 0 {
		t.Fatalf("unexpected --name=value result: value=%q consumed=%d err=%v", value, consumed, err)
	}
}

// TestMapLinkOIDCIdentityLookupErrorBranches unit-tests the pure mapper
// directly with constructed sentinels, the same style
// TestMapResetPasswordErrorFormatsAmbiguousEmail already uses for the sibling
// command — cheaper and more precise than engineering a live DB fixture for
// each outcome.
func TestMapLinkOIDCIdentityLookupErrorBranches(t *testing.T) {
	t.Parallel()

	ambiguous := &services.AmbiguousEmailError{Email: "owner@example.com", IDs: []uint{5, 18}}
	if err := mapLinkOIDCIdentityLookupError(ambiguous, linkOIDCIdentityOptions{}, "owner@example.com"); err == nil || !strings.Contains(err.Error(), "retry with --id") {
		t.Fatalf("expected an ambiguous-email refusal, got %v", err)
	}

	if err := mapLinkOIDCIdentityLookupError(services.ErrOperatorUserNotFound, linkOIDCIdentityOptions{userID: 42}, ""); err == nil || !strings.Contains(err.Error(), "id 42") {
		t.Fatalf("expected an id-not-found refusal naming the id, got %v", err)
	}
	if err := mapLinkOIDCIdentityLookupError(services.ErrOperatorUserNotFound, linkOIDCIdentityOptions{}, "owner@example.com"); err == nil || !strings.Contains(err.Error(), "owner@example.com") {
		t.Fatalf("expected an email-not-found refusal naming the email, got %v", err)
	}
	if err := mapLinkOIDCIdentityLookupError(services.ErrOperatorUserIDRequired, linkOIDCIdentityOptions{}, ""); err == nil || !strings.Contains(err.Error(), "an account id is required") {
		t.Fatalf("expected the id-required refusal, got %v", err)
	}
	if err := mapLinkOIDCIdentityLookupError(errors.New("boom"), linkOIDCIdentityOptions{}, ""); err == nil || !strings.Contains(err.Error(), "look up account") {
		t.Fatalf("expected the generic lookup-failure fallback, got %v", err)
	}
}

// TestMapLinkOIDCIdentityLinkErrorBranches is the ConfirmAndLinkIdentity-side
// counterpart of the lookup-error test above.
func TestMapLinkOIDCIdentityLinkErrorBranches(t *testing.T) {
	t.Parallel()

	if err := mapLinkOIDCIdentityLinkError(services.ErrOIDCDisabled); err == nil || !strings.Contains(err.Error(), "OIDC is not enabled") {
		t.Fatalf("expected an OIDC-disabled refusal, got %v", err)
	}
	if err := mapLinkOIDCIdentityLinkError(services.ErrOIDCLinkFailed); err == nil || !strings.Contains(err.Error(), "already linked to a different account") {
		t.Fatalf("expected a cross-user-claim refusal, got %v", err)
	}
	if err := mapLinkOIDCIdentityLinkError(services.ErrOIDCIdentityResolveFailed); err == nil || !strings.Contains(err.Error(), "link oidc identity") {
		t.Fatalf("expected the generic fallback for a storage failure, got %v", err)
	}
}

// TestRunLinkOIDCIdentityCommandLinksByEmail is the email-addressed sibling of
// TestRunLinkOIDCIdentityCommandLinksByID: both addressing forms reach
// ConfirmAndLinkIdentity through resolveLinkOIDCIdentityTarget.
func TestRunLinkOIDCIdentityCommandLinksByEmail(t *testing.T) {
	t.Parallel()

	databasePath := filepath.Join(t.TempDir(), "cli-link-oidc-email.db")
	createCLILinkOIDCUser(t, databasePath, "cli-link-by-email@example.com")

	var output bytes.Buffer
	err := runLinkOIDCIdentityCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		validLinkOIDCIdentityConfig(),
		[]string{"cli-link-by-email@example.com", "--issuer", "https://idp.example.com", "--subject", "cli-linked-by-email"},
		&output,
	)
	if err != nil {
		t.Fatalf("runLinkOIDCIdentityCommand returned error: %v", err)
	}
	if !strings.Contains(output.String(), "cli-link-by-email@example.com") {
		t.Fatalf("expected confirmation output naming the account, got %q", output.String())
	}
}

// TestRunLinkOIDCIdentityCommandRefusesAMalformedEmail exercises the
// mail.ParseAddress guard directly: a positional argument that is not a valid
// address is refused before anything touches the database.
func TestRunLinkOIDCIdentityCommandRefusesAMalformedEmail(t *testing.T) {
	t.Parallel()

	err := runLinkOIDCIdentityCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: filepath.Join(t.TempDir(), "unused.db")},
		validLinkOIDCIdentityConfig(),
		[]string{"not-an-email-address", "--issuer", "https://idp.example.com", "--subject", "sub"},
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "invalid email address") {
		t.Fatalf("expected an invalid-email refusal, got %v", err)
	}
}

// TestRunLinkOIDCIdentityCommandRefusesInvalidDatabaseConfig covers the first
// db.OpenDatabase failure arm.
func TestRunLinkOIDCIdentityCommandRefusesInvalidDatabaseConfig(t *testing.T) {
	t.Parallel()

	err := runLinkOIDCIdentityCommand(
		db.Config{Driver: "bogus"},
		validLinkOIDCIdentityConfig(),
		[]string{"--id", "1", "--issuer", "https://idp.example.com", "--subject", "sub"},
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "database init failed") {
		t.Fatalf("expected a database-init refusal, got %v", err)
	}
}

// TestRunLinkOIDCIdentityCommandReportsAGenericLookupFailure drives a REAL
// storage error (rather than a constructed sentinel) through
// resolveLinkOIDCIdentityTarget's default arm: with the users table gone, the
// id lookup fails for reasons that are not "not found", and the command must
// say so rather than misreport it as an unknown id.
func TestRunLinkOIDCIdentityCommandReportsAGenericLookupFailure(t *testing.T) {
	t.Parallel()

	databasePath := filepath.Join(t.TempDir(), "cli-link-oidc-lookup-failure.db")
	user := createCLILinkOIDCUser(t, databasePath, "cli-link-lookup-failure@example.com")
	dropCLILinkOIDCTable(t, databasePath, "users")

	err := runLinkOIDCIdentityCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		validLinkOIDCIdentityConfig(),
		[]string{"--id", strconv.FormatUint(uint64(user.ID), 10), "--issuer", "https://idp.example.com", "--subject", "sub"},
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "look up account") {
		t.Fatalf("expected the generic lookup-failure wording, got %v", err)
	}
}

func dropCLILinkOIDCTable(t *testing.T, databasePath string, table string) {
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
	if err := database.Exec("DROP TABLE " + table).Error; err != nil {
		t.Fatalf("drop table %s: %v", table, err)
	}
}
