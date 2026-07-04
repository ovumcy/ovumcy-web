package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ovumcy/ovumcy-web/internal/db"
	"github.com/ovumcy/ovumcy-web/internal/models"
	"github.com/ovumcy/ovumcy-web/internal/services"
)

func TestRunUsersCommandUsageErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing subcommand", args: nil, want: "usage: ovumcy users <list|delete|create>"},
		{name: "unknown subcommand", args: []string{"export"}, want: "usage: ovumcy users <list|delete|create>"},
		{name: "list with extra arg", args: []string{"list", "extra"}, want: "usage: ovumcy users list"},
		{name: "delete without email", args: []string{"delete"}, want: "usage: ovumcy users delete <email> [--yes]"},
		{name: "create without email", args: []string{"create"}, want: "usage: ovumcy users create <email> [--show-recovery-code] [--skip-if-exists]"},
		{name: "create with unknown flag", args: []string{"create", "owner@example.com", "--oops"}, want: "usage: ovumcy users create <email> [--show-recovery-code] [--skip-if-exists]"},
	}

	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := runUsersCommand(db.Config{}, testCase.args, strings.NewReader(""), &bytes.Buffer{})
			if err == nil || err.Error() != testCase.want {
				t.Fatalf("expected error %q, got %v", testCase.want, err)
			}
		})
	}
}

func TestRunUsersCommandListShowsEmptyState(t *testing.T) {
	t.Parallel()

	databasePath := createCLIUsersDatabase(t)
	var output bytes.Buffer

	err := runUsersCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		[]string{"list"},
		strings.NewReader(""),
		&output,
	)
	if err != nil {
		t.Fatalf("runUsersCommand(list) returned error: %v", err)
	}
	if output.String() != "No users found.\n" {
		t.Fatalf("expected empty-state output, got %q", output.String())
	}
}

func TestParseUsersDeleteArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		args         []string
		wantEmail    string
		wantSkip     bool
		wantErrorMsg string
	}{
		{name: "email only", args: []string{"owner@example.com"}, wantEmail: "owner@example.com"},
		{name: "email and yes", args: []string{"owner@example.com", "--yes"}, wantEmail: "owner@example.com", wantSkip: true},
		{name: "yes before email", args: []string{"--yes", "owner@example.com"}, wantEmail: "owner@example.com", wantSkip: true},
		{name: "missing email", args: []string{"--yes"}, wantErrorMsg: "usage: ovumcy users delete <email> [--yes]"},
		{name: "multiple emails", args: []string{"one@example.com", "two@example.com"}, wantErrorMsg: "usage: ovumcy users delete <email> [--yes]"},
	}

	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			gotEmail, gotSkip, err := parseUsersDeleteArgs(testCase.args)
			if testCase.wantErrorMsg != "" {
				if err == nil || err.Error() != testCase.wantErrorMsg {
					t.Fatalf("expected error %q, got %v", testCase.wantErrorMsg, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseUsersDeleteArgs returned error: %v", err)
			}
			if gotEmail != testCase.wantEmail || gotSkip != testCase.wantSkip {
				t.Fatalf("unexpected parsed args: email=%q skip=%t", gotEmail, gotSkip)
			}
		})
	}
}

func TestReadDeleteConfirmation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		input       io.Reader
		wantConfirm bool
		wantError   string
	}{
		{name: "explicit delete", input: strings.NewReader("DELETE\n"), wantConfirm: true},
		{name: "delete without newline", input: strings.NewReader("DELETE"), wantConfirm: true},
		{name: "case insensitive", input: strings.NewReader("delete\n"), wantConfirm: true},
		{name: "other text", input: strings.NewReader("nope\n"), wantConfirm: false},
		{name: "nil input", input: nil, wantError: "confirmation input is required"},
	}

	for _, testCase := range tests {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			confirmed, err := readDeleteConfirmation(testCase.input)
			if testCase.wantError != "" {
				if err == nil || err.Error() != testCase.wantError {
					t.Fatalf("expected error %q, got %v", testCase.wantError, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("readDeleteConfirmation returned error: %v", err)
			}
			if confirmed != testCase.wantConfirm {
				t.Fatalf("expected confirmation=%t, got %t", testCase.wantConfirm, confirmed)
			}
		})
	}
}

func TestRunUsersDeleteRequiresConfirmationInputWhenYesFlagIsAbsent(t *testing.T) {
	t.Parallel()

	databasePath := createCLIUsersDatabase(t)
	user := createCLIUsersUser(t, databasePath, "needs-confirmation@example.com", "Owner", models.RoleOwner, true, nowUTC())
	seedCLIUsersHealthData(t, databasePath, user.ID)

	err := runUsersDelete(
		mustCLIUsersService(t, databasePath),
		[]string{"needs-confirmation@example.com"},
		nil,
		&bytes.Buffer{},
	)
	if err == nil || err.Error() != "confirmation input is required" {
		t.Fatalf("expected missing confirmation input error, got %v", err)
	}
}

func mustCLIUsersService(t *testing.T, databasePath string) *services.OperatorUserService {
	t.Helper()

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

	repositories := db.NewRepositories(database)
	return services.NewOperatorUserService(repositories.Users, services.NewAuthService(repositories.Users))
}

func nowUTC() time.Time {
	return time.Now().UTC()
}

func TestReadPasswordLineTrimsSurroundingWhitespace(t *testing.T) {
	t.Parallel()

	got, err := readPasswordLine(strings.NewReader("  StrongPass1  \r\n"))
	if err != nil {
		t.Fatalf("readPasswordLine returned error: %v", err)
	}
	if string(got) != "StrongPass1" {
		t.Fatalf("expected surrounding whitespace trimmed to match web auth, got %q", string(got))
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("read boom") }

func TestReadPasswordLineErrors(t *testing.T) {
	t.Parallel()

	if _, err := readPasswordLine(nil); err == nil {
		t.Fatal("expected error for nil input")
	}
	if _, err := readPasswordLine(strings.NewReader("")); err == nil {
		t.Fatal("expected error for empty input")
	}
	if _, err := readPasswordLine(strings.NewReader("   \n")); err == nil {
		t.Fatal("expected error for a blank password")
	}
	if _, err := readPasswordLine(failingReader{}); err == nil {
		t.Fatal("expected a non-EOF read error to propagate")
	}
}

func TestStdinIsTerminalReturnsFalseForRegularFile(t *testing.T) {
	t.Parallel()

	file, err := os.CreateTemp(t.TempDir(), "notty")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer func() { _ = file.Close() }()

	if stdinIsTerminal(file) {
		t.Fatal("expected a regular file not to be reported as a terminal")
	}

	closed, err := os.CreateTemp(t.TempDir(), "closed")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	_ = closed.Close()
	if stdinIsTerminal(closed) {
		t.Fatal("expected a closed file (failed Stat) not to be reported as a terminal")
	}
}

func TestReadCreatePasswordReadsFromNonTTYFile(t *testing.T) {
	t.Parallel()

	file, err := os.CreateTemp(t.TempDir(), "pw")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer func() { _ = file.Close() }()
	if _, err := file.WriteString("StrongPass1\n"); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	if _, err := file.Seek(0, 0); err != nil {
		t.Fatalf("seek temp file: %v", err)
	}

	got, err := readCreatePassword(file)
	if err != nil {
		t.Fatalf("readCreatePassword returned error: %v", err)
	}
	if string(got) != "StrongPass1" {
		t.Fatalf("expected password read from a non-TTY file, got %q", string(got))
	}
}

func TestMapUsersCreateError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want string
	}{
		{"email required", services.ErrOperatorUserEmailRequired, "email is required"},
		{"invalid email", services.ErrOperatorUserEmailInvalid, "invalid email address"},
		{"weak password", services.ErrOperatorUserPasswordWeak, "strength"},
		{"duplicate", services.ErrOperatorUserEmailExists, "already exists"},
		{"other", errors.New("boom"), "create owner"},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			got := mapUsersCreateError(testCase.err)
			if got == nil || !strings.Contains(got.Error(), testCase.want) {
				t.Fatalf("mapUsersCreateError(%v) = %v, want contains %q", testCase.err, got, testCase.want)
			}
		})
	}
}

func TestParseUsersCreateArgsAcceptsFlags(t *testing.T) {
	t.Parallel()

	opts, err := parseUsersCreateArgs([]string{"", "owner@example.com", "--show-recovery-code", "--skip-if-exists"})
	if err != nil {
		t.Fatalf("parseUsersCreateArgs returned error: %v", err)
	}
	if opts.email != "owner@example.com" || !opts.showRecoveryCode || !opts.skipIfExists {
		t.Fatalf("unexpected parsed options: %#v", opts)
	}

	if _, err := parseUsersCreateArgs([]string{"a@example.com", "b@example.com"}); err == nil {
		t.Fatal("expected error for a second positional email")
	}
}

func TestRunUsersCreateReturnsParseError(t *testing.T) {
	t.Parallel()

	if err := runUsersCreate(nil, []string{"--nope"}, strings.NewReader(""), &bytes.Buffer{}); err == nil {
		t.Fatal("expected a parse error for an unknown flag")
	}
}

func TestRunUsersCreateReturnsPasswordError(t *testing.T) {
	t.Parallel()

	if err := runUsersCreate(nil, []string{"owner@example.com"}, strings.NewReader("\n"), &bytes.Buffer{}); err == nil {
		t.Fatal("expected an empty-password error")
	}
}

func TestRunUsersCreateDefaultsNilOutputOnSuccess(t *testing.T) {
	databasePath := createCLIUsersDatabase(t)
	service := mustCLIUsersService(t, databasePath)

	if err := runUsersCreate(service, []string{"owner@example.com"}, strings.NewReader("StrongPass1\n"), nil); err != nil {
		t.Fatalf("runUsersCreate with nil output returned error: %v", err)
	}
}

func TestRunUsersCreateDefaultsNilOutputOnSkip(t *testing.T) {
	databasePath := createCLIUsersDatabase(t)
	createCLIUsersUser(t, databasePath, "owner@example.com", "Owner", models.RoleOwner, true, time.Now().UTC())
	service := mustCLIUsersService(t, databasePath)

	if err := runUsersCreate(service, []string{"owner@example.com", "--skip-if-exists"}, strings.NewReader("StrongPass1\n"), nil); err != nil {
		t.Fatalf("runUsersCreate skip with nil output returned error: %v", err)
	}
}

// TestRunUsersCommandReportsDatabaseInitFailure mirrors the reset command's
// operator UX: when the configured database cannot be opened, the users
// command surfaces a wrapped "database init failed" error. A directory path
// is an unopenable SQLite target on every platform.
func TestRunUsersCommandReportsDatabaseInitFailure(t *testing.T) {
	err := runUsersCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: t.TempDir()},
		[]string{"list"},
		bytes.NewReader(nil),
		io.Discard,
	)
	if err == nil {
		t.Fatal("expected an error when the database cannot be opened")
	}
	if !strings.Contains(err.Error(), "database init failed") {
		t.Fatalf("expected a wrapped database-init error, got %v", err)
	}
}
