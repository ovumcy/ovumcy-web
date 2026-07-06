package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
	"golang.org/x/crypto/bcrypt"
)

// testWebhookSecretKey is a syntactically valid SECRET_KEY (>= 32 chars) used to
// encrypt/decrypt the webhook URL in the CLI tests. It is a fake key for a fake
// endpoint; no real token is ever involved.
const testWebhookSecretKey = "0123456789abcdef0123456789abcdef"

// testWebhookURLWithToken is a fake ntfy-style endpoint whose PATH embeds a
// token-shaped secret. The headline security assertion is that this substring
// NEVER appears in any command output — only the host may be printed.
const testWebhookURLWithToken = "https://ntfy.example/topic-abc?token=tk_SECRETVALUE123"

// testWebhookTokenSubstring is the secret fragment that must never leak.
const testWebhookTokenSubstring = "tk_SECRETVALUE123"

func createCLIWebhookDatabase(t *testing.T) string {
	t.Helper()

	databasePath := filepath.Join(t.TempDir(), "cli-webhook-test.db")
	database, err := db.OpenSQLite(databasePath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return databasePath
}

func createCLIWebhookOwner(t *testing.T, databasePath string, email string) models.User {
	t.Helper()

	database, err := db.OpenSQLite(databasePath)
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
		Email:                  strings.ToLower(strings.TrimSpace(email)),
		PasswordHash:           string(passwordHash),
		Role:                   models.RoleOwner,
		CycleLength:            models.DefaultCycleLength,
		PeriodLength:           models.DefaultPeriodLength,
		AutoPeriodFill:         true,
		WebhookNotifyPeriod:    true,
		WebhookNotifyOvulation: true,
		ReminderLeadDays:       models.DefaultReminderLeadDays,
		CreatedAt:              time.Now().UTC(),
	}
	if err := database.Create(&user).Error; err != nil {
		t.Fatalf("create owner: %v", err)
	}
	return user
}

// loadWebhookRow reads the raw persisted webhook columns for the owner so tests
// can assert ciphertext-at-rest (never plaintext) and the toggle/lead values.
func loadWebhookRow(t *testing.T, databasePath string, userID uint) models.User {
	t.Helper()

	database, err := db.OpenSQLite(databasePath)
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
		t.Fatalf("load webhook row: %v", err)
	}
	return user
}

func sqliteConfig(databasePath string) db.Config {
	return db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath}
}

// TestRunWebhookCommandSetPersistsSettingsAndEncryptsURL is the core happy path:
// `webhook set` with the URL piped over stdin persists the toggles and lead days,
// stores the URL as CIPHERTEXT (never plaintext, never the token), and the stored
// ciphertext round-trips through DecryptWebhookURL back to the exact plaintext.
func TestRunWebhookCommandSetPersistsSettingsAndEncryptsURL(t *testing.T) {
	t.Parallel()

	databasePath := createCLIWebhookDatabase(t)
	owner := createCLIWebhookOwner(t, databasePath, "owner@example.com")

	var output bytes.Buffer
	err := runWebhookCommand(
		sqliteConfig(databasePath),
		testWebhookSecretKey,
		[]string{"set", "owner@example.com", "--enabled=true", "--notify-period=false", "--reminder-lead-days=5", "--url-stdin"},
		strings.NewReader(testWebhookURLWithToken+"\n"),
		&output,
	)
	if err != nil {
		t.Fatalf("runWebhookCommand(set) returned error: %v", err)
	}

	rendered := output.String()
	if !strings.Contains(rendered, "webhook configured for owner@example.com") {
		t.Fatalf("expected success confirmation, got %q", rendered)
	}
	// Host-only in the confirmation; NEVER the token/scheme/path.
	if !strings.Contains(rendered, "ntfy.example") {
		t.Fatalf("expected host in confirmation, got %q", rendered)
	}
	assertNoSecretLeak(t, rendered)

	row := loadWebhookRow(t, databasePath, owner.ID)
	if !row.WebhookEnabled {
		t.Fatalf("expected webhook enabled persisted, got %+v", row)
	}
	if row.WebhookNotifyPeriod {
		t.Fatalf("expected notify-period=false persisted, got %+v", row)
	}
	if !row.WebhookNotifyOvulation {
		t.Fatalf("expected notify-ovulation to remain true (unspecified), got %+v", row)
	}
	if row.ReminderLeadDays != 5 {
		t.Fatalf("expected reminder_lead_days=5, got %d", row.ReminderLeadDays)
	}
	// The stored URL must be ciphertext: not the plaintext, not the token.
	if row.WebhookURL == "" {
		t.Fatal("expected an encrypted webhook_url to be stored")
	}
	if strings.Contains(row.WebhookURL, testWebhookTokenSubstring) || strings.Contains(row.WebhookURL, "ntfy.example") {
		t.Fatalf("stored webhook_url is not ciphertext (leaks plaintext): %q", row.WebhookURL)
	}
	// Round-trip: the ciphertext decrypts back to the exact plaintext endpoint.
	svc := services.NewWebhookSettingsService(nil, []byte(testWebhookSecretKey))
	plaintext, err := svc.DecryptWebhookURL(owner.ID, row.WebhookURL)
	if err != nil {
		t.Fatalf("DecryptWebhookURL: %v", err)
	}
	if plaintext != testWebhookURLWithToken {
		t.Fatalf("round-trip mismatch: got %q want %q", plaintext, testWebhookURLWithToken)
	}
}

// TestRunWebhookCommandShowPrintsHostOnlyNeverToken is the headline security
// assertion: `webhook show` prints the configured status and the destination
// HOST, and NEVER the full URL, path, query, or token.
func TestRunWebhookCommandShowPrintsHostOnlyNeverToken(t *testing.T) {
	t.Parallel()

	databasePath := createCLIWebhookDatabase(t)
	owner := createCLIWebhookOwner(t, databasePath, "owner@example.com")

	// Arm the webhook first (piped URL).
	var armOut bytes.Buffer
	if err := runWebhookCommand(
		sqliteConfig(databasePath),
		testWebhookSecretKey,
		[]string{"set", "owner@example.com", "--enabled=true", "--url-stdin"},
		strings.NewReader(testWebhookURLWithToken+"\n"),
		&armOut,
	); err != nil {
		t.Fatalf("arm webhook: %v", err)
	}
	_ = owner

	var output bytes.Buffer
	if err := runWebhookCommand(
		sqliteConfig(databasePath),
		testWebhookSecretKey,
		[]string{"show", "owner@example.com"},
		strings.NewReader(""),
		&output,
	); err != nil {
		t.Fatalf("runWebhookCommand(show) returned error: %v", err)
	}

	rendered := output.String()
	if !strings.Contains(rendered, "webhook status for owner@example.com") {
		t.Fatalf("expected status header, got %q", rendered)
	}
	if !strings.Contains(rendered, "configured (host ntfy.example)") {
		t.Fatalf("expected host-only endpoint status, got %q", rendered)
	}
	if !strings.Contains(rendered, "enabled:") {
		t.Fatalf("expected enabled line, got %q", rendered)
	}
	// Headline: the token/path/query/scheme must be absent from show output.
	assertNoSecretLeak(t, rendered)
}

// TestRunWebhookCommandShowNotConfigured proves the status line for an owner with
// no endpoint reads "not configured" and leaks nothing.
func TestRunWebhookCommandShowNotConfigured(t *testing.T) {
	t.Parallel()

	databasePath := createCLIWebhookDatabase(t)
	createCLIWebhookOwner(t, databasePath, "owner@example.com")

	var output bytes.Buffer
	if err := runWebhookCommand(
		sqliteConfig(databasePath),
		testWebhookSecretKey,
		[]string{"show", "owner@example.com"},
		strings.NewReader(""),
		&output,
	); err != nil {
		t.Fatalf("runWebhookCommand(show) returned error: %v", err)
	}
	if !strings.Contains(output.String(), "not configured") {
		t.Fatalf("expected 'not configured', got %q", output.String())
	}
}

// TestRunWebhookCommandSetRejectsInvalidScheme proves a non-http(s) URL is
// rejected by the reused service validator and NOTHING is persisted.
func TestRunWebhookCommandSetRejectsInvalidScheme(t *testing.T) {
	t.Parallel()

	databasePath := createCLIWebhookDatabase(t)
	owner := createCLIWebhookOwner(t, databasePath, "owner@example.com")

	var output bytes.Buffer
	err := runWebhookCommand(
		sqliteConfig(databasePath),
		testWebhookSecretKey,
		[]string{"set", "owner@example.com", "--enabled=true", "--url-stdin"},
		strings.NewReader("ftp://evil.example/hook\n"),
		&output,
	)
	if err == nil || !strings.Contains(err.Error(), "webhook url invalid") {
		t.Fatalf("expected invalid-url error, got %v", err)
	}

	row := loadWebhookRow(t, databasePath, owner.ID)
	if row.WebhookEnabled || row.WebhookURL != "" {
		t.Fatalf("expected nothing persisted on invalid URL, got %+v", row)
	}
}

// TestRunWebhookCommandUnknownEmail proves an unmatched owner returns an error
// (and never a panic).
func TestRunWebhookCommandUnknownEmail(t *testing.T) {
	t.Parallel()

	databasePath := createCLIWebhookDatabase(t)

	var output bytes.Buffer
	err := runWebhookCommand(
		sqliteConfig(databasePath),
		testWebhookSecretKey,
		[]string{"show", "ghost@example.com"},
		strings.NewReader(""),
		&output,
	)
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not-found error, got %v", err)
	}
}

// TestRunWebhookCommandDryRunWritesNothing proves --dry-run validates and prints
// the would-be change but persists nothing.
func TestRunWebhookCommandDryRunWritesNothing(t *testing.T) {
	t.Parallel()

	databasePath := createCLIWebhookDatabase(t)
	owner := createCLIWebhookOwner(t, databasePath, "owner@example.com")

	var output bytes.Buffer
	err := runWebhookCommand(
		sqliteConfig(databasePath),
		testWebhookSecretKey,
		[]string{"set", "owner@example.com", "--enabled=true", "--reminder-lead-days=9", "--url-stdin", "--dry-run"},
		strings.NewReader(testWebhookURLWithToken+"\n"),
		&output,
	)
	if err != nil {
		t.Fatalf("runWebhookCommand(set --dry-run) returned error: %v", err)
	}

	rendered := output.String()
	if !strings.Contains(rendered, "[dry-run] no changes written") {
		t.Fatalf("expected dry-run notice, got %q", rendered)
	}
	// The dry-run preview must still be host-only.
	if !strings.Contains(rendered, "ntfy.example") {
		t.Fatalf("expected host in dry-run preview, got %q", rendered)
	}
	assertNoSecretLeak(t, rendered)

	row := loadWebhookRow(t, databasePath, owner.ID)
	if row.WebhookEnabled || row.WebhookURL != "" || row.ReminderLeadDays != models.DefaultReminderLeadDays {
		t.Fatalf("dry-run must not persist anything, got %+v", row)
	}
}

// TestRunWebhookCommandLeadDaysClampedAboveMax proves an out-of-range lead-days
// value is CLAMPED (not rejected) by the reused service.
func TestRunWebhookCommandLeadDaysClampedAboveMax(t *testing.T) {
	t.Parallel()

	databasePath := createCLIWebhookDatabase(t)
	owner := createCLIWebhookOwner(t, databasePath, "owner@example.com")

	var output bytes.Buffer
	err := runWebhookCommand(
		sqliteConfig(databasePath),
		testWebhookSecretKey,
		[]string{"set", "owner@example.com", "--reminder-lead-days=99"},
		strings.NewReader(""),
		&output,
	)
	if err != nil {
		t.Fatalf("runWebhookCommand(set lead-days) returned error: %v", err)
	}

	row := loadWebhookRow(t, databasePath, owner.ID)
	if row.ReminderLeadDays != services.MaxReminderLeadDays {
		t.Fatalf("expected lead days clamped to %d, got %d", services.MaxReminderLeadDays, row.ReminderLeadDays)
	}
}

// TestRunWebhookCommandClearURLDisablesEndpoint proves --clear-url removes the
// stored ciphertext.
func TestRunWebhookCommandClearURLDisablesEndpoint(t *testing.T) {
	t.Parallel()

	databasePath := createCLIWebhookDatabase(t)
	owner := createCLIWebhookOwner(t, databasePath, "owner@example.com")

	// Arm, then clear.
	if err := runWebhookCommand(
		sqliteConfig(databasePath),
		testWebhookSecretKey,
		[]string{"set", "owner@example.com", "--enabled=true", "--url-stdin"},
		strings.NewReader(testWebhookURLWithToken+"\n"),
		&bytes.Buffer{},
	); err != nil {
		t.Fatalf("arm webhook: %v", err)
	}

	var output bytes.Buffer
	if err := runWebhookCommand(
		sqliteConfig(databasePath),
		testWebhookSecretKey,
		[]string{"set", "owner@example.com", "--enabled=false", "--clear-url"},
		strings.NewReader(""),
		&output,
	); err != nil {
		t.Fatalf("runWebhookCommand(clear) returned error: %v", err)
	}

	row := loadWebhookRow(t, databasePath, owner.ID)
	if row.WebhookURL != "" {
		t.Fatalf("expected endpoint cleared, got %q", row.WebhookURL)
	}
	if row.WebhookEnabled {
		t.Fatalf("expected webhook disabled after clear, got enabled")
	}
}

// TestRunWebhookCommandURLFromEnv proves the endpoint can be supplied via the
// OVUMCY_WEBHOOK_URL env var (never argv) and is stored as ciphertext.
func TestRunWebhookCommandURLFromEnv(t *testing.T) {
	// Not parallel: mutates process env.
	databasePath := createCLIWebhookDatabase(t)
	owner := createCLIWebhookOwner(t, databasePath, "owner@example.com")

	t.Setenv(webhookURLEnv, testWebhookURLWithToken)

	var output bytes.Buffer
	if err := runWebhookCommand(
		sqliteConfig(databasePath),
		testWebhookSecretKey,
		[]string{"set", "owner@example.com", "--enabled=true"},
		strings.NewReader(""),
		&output,
	); err != nil {
		t.Fatalf("runWebhookCommand(set env url) returned error: %v", err)
	}
	assertNoSecretLeak(t, output.String())

	row := loadWebhookRow(t, databasePath, owner.ID)
	svc := services.NewWebhookSettingsService(nil, []byte(testWebhookSecretKey))
	plaintext, err := svc.DecryptWebhookURL(owner.ID, row.WebhookURL)
	if err != nil {
		t.Fatalf("DecryptWebhookURL: %v", err)
	}
	if plaintext != testWebhookURLWithToken {
		t.Fatalf("env URL round-trip mismatch: got %q", plaintext)
	}
}

// TestRunWebhookCommandUsageErrors proves argument validation returns the usage
// string and never touches the DB (bad subcommand, missing email, unknown flag,
// malformed bool). These run before any DB open.
func TestRunWebhookCommandUsageErrors(t *testing.T) {
	t.Parallel()

	config := sqliteConfig(filepath.Join(t.TempDir(), "unused.db"))
	cases := [][]string{
		nil,
		{"bogus", "owner@example.com"},
		{"set"},
		{"show"},
		{"set", "owner@example.com", "--nope"},
		{"set", "owner@example.com", "--enabled=maybe"},
		{"set", "owner@example.com", "--reminder-lead-days=abc"},
		{"show", "owner@example.com", "--flag"},
		{"set", "a@example.com", "b@example.com"},
	}
	for _, args := range cases {
		err := runWebhookCommand(config, testWebhookSecretKey, args, strings.NewReader(""), &bytes.Buffer{})
		if err == nil {
			t.Fatalf("expected an error for args %v", args)
		}
	}
}

// TestRunWebhookCommandEnableWithoutEndpointRejected proves enabling delivery
// with no stored endpoint and no supplied URL is rejected by the reused
// validator (a webhook cannot be armed without a deliverable target).
func TestRunWebhookCommandEnableWithoutEndpointRejected(t *testing.T) {
	t.Parallel()

	databasePath := createCLIWebhookDatabase(t)
	createCLIWebhookOwner(t, databasePath, "owner@example.com")

	err := runWebhookCommand(
		sqliteConfig(databasePath),
		testWebhookSecretKey,
		[]string{"set", "owner@example.com", "--enabled=true"},
		strings.NewReader(""),
		&bytes.Buffer{},
	)
	if err == nil || !strings.Contains(err.Error(), "webhook url invalid") {
		t.Fatalf("expected invalid-url error when enabling without an endpoint, got %v", err)
	}
}

// TestRunWebhookCommandClearURLAndStdinMutuallyExclusive proves the guard against
// combining --clear-url with --url-stdin.
func TestRunWebhookCommandClearURLAndStdinMutuallyExclusive(t *testing.T) {
	t.Parallel()

	config := sqliteConfig(filepath.Join(t.TempDir(), "unused.db"))
	err := runWebhookCommand(
		config,
		testWebhookSecretKey,
		[]string{"set", "owner@example.com", "--clear-url", "--url-stdin"},
		strings.NewReader("https://ntfy.example/topic\n"),
		&bytes.Buffer{},
	)
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected mutual-exclusion error, got %v", err)
	}
}

// TestRunWebhookCommandStdinRequiresURL proves --url-stdin with empty stdin is a
// clear error, not a silent empty-URL save.
func TestRunWebhookCommandStdinRequiresURL(t *testing.T) {
	t.Parallel()

	databasePath := createCLIWebhookDatabase(t)
	createCLIWebhookOwner(t, databasePath, "owner@example.com")

	err := runWebhookCommand(
		sqliteConfig(databasePath),
		testWebhookSecretKey,
		[]string{"set", "owner@example.com", "--enabled=true", "--url-stdin"},
		strings.NewReader(""),
		&bytes.Buffer{},
	)
	if err == nil || !strings.Contains(err.Error(), "webhook url is required") {
		t.Fatalf("expected 'webhook url is required', got %v", err)
	}
}

// TestRunWebhookCommandExportedEntryPoint drives the exported RunWebhookCommand
// against a real migrated SQLite DB to cover the os.Stdin/os.Stdout wiring and
// the DB-open path. An unknown owner returns an error without a panic.
func TestRunWebhookCommandExportedEntryPoint(t *testing.T) {
	t.Parallel()

	databasePath := createCLIWebhookDatabase(t)
	if err := RunWebhookCommand(sqliteConfig(databasePath), testWebhookSecretKey, []string{"show", "ghost@example.com"}); err == nil {
		t.Fatal("expected a not-found error from the exported entry point")
	}
}

// TestRunWebhookCommandDatabaseInitFailure proves a bad DB config surfaces as an
// error (the "database init failed" branch), not a panic.
func TestRunWebhookCommandDatabaseInitFailure(t *testing.T) {
	t.Parallel()

	err := runWebhookCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: ""},
		testWebhookSecretKey,
		[]string{"show", "owner@example.com"},
		strings.NewReader(""),
		&bytes.Buffer{},
	)
	if err == nil {
		t.Fatal("expected a database init failure, got nil")
	}
}

// assertNoSecretLeak fails if the rendered output contains any secret-shaped
// substring: the token, the URL scheme, the path, or the query. Only the host
// may appear.
func assertNoSecretLeak(t *testing.T, rendered string) {
	t.Helper()
	for _, banned := range []string{testWebhookTokenSubstring, "https://", "http://", "token=", "/topic"} {
		if strings.Contains(rendered, banned) {
			t.Fatalf("output leaked secret-shaped substring %q: %q", banned, rendered)
		}
	}
}
