package cli

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strconv"
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
		{name: "missing subcommand", args: nil, want: "usage: ovumcy users <list|delete|create|set-email>"},
		{name: "unknown subcommand", args: []string{"export"}, want: "usage: ovumcy users <list|delete|create|set-email>"},
		{name: "list with extra arg", args: []string{"list", "extra"}, want: "usage: ovumcy users list"},
		{name: "delete without a handle", args: []string{"delete"}, want: usersDeleteUsage},
		{name: "set-email without arguments", args: []string{"set-email"}, want: usersSetEmailUsage},
		{name: "set-email without an address", args: []string{"set-email", "--id", "7"}, want: usersSetEmailUsage},
		{name: "create without email", args: []string{"create"}, want: "usage: ovumcy users create <email> [--show-recovery-code] [--skip-if-exists]"},
		{name: "create with unknown flag", args: []string{"create", "owner@example.com", "--oops"}, want: "usage: ovumcy users create <email> [--show-recovery-code] [--skip-if-exists]"},
	}

	for _, testCase := range tests {

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
		wantID       uint
		wantSkip     bool
		wantErrorMsg string
	}{
		{name: "email only", args: []string{"owner@example.com"}, wantEmail: "owner@example.com"},
		{name: "email and yes", args: []string{"owner@example.com", "--yes"}, wantEmail: "owner@example.com", wantSkip: true},
		{name: "yes before email", args: []string{"--yes", "owner@example.com"}, wantEmail: "owner@example.com", wantSkip: true},
		{name: "id form", args: []string{"--id", "7"}, wantID: 7},
		{name: "blank argument between flags", args: []string{"", "--id", "7", ""}, wantID: 7},
		{name: "id form with equals and yes", args: []string{"--id=7", "--yes"}, wantID: 7, wantSkip: true},
		{name: "missing handle", args: []string{"--yes"}, wantErrorMsg: usersDeleteUsage},
		{name: "multiple emails", args: []string{"one@example.com", "two@example.com"}, wantErrorMsg: usersDeleteUsage},
		{name: "email and id together", args: []string{"owner@example.com", "--id", "7"}, wantErrorMsg: usersDeleteUsage},
		{name: "two ids", args: []string{"--id", "7", "--id", "8"}, wantErrorMsg: usersDeleteUsage},
		{name: "id without value", args: []string{"--id"}, wantErrorMsg: usersDeleteUsage},
		{name: "unknown flag", args: []string{"owner@example.com", "--force"}, wantErrorMsg: usersDeleteUsage},
		{name: "id zero", args: []string{"--id", "0"}, wantErrorMsg: `invalid account id "0" (see ovumcy users list)`},
		{name: "id not a number", args: []string{"--id", "seven"}, wantErrorMsg: `invalid account id "seven" (see ovumcy users list)`},
		{name: "id negative", args: []string{"--id", "-1"}, wantErrorMsg: `invalid account id "-1" (see ovumcy users list)`},
	}

	for _, testCase := range tests {

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			opts, err := parseUsersDeleteArgs(testCase.args)
			if testCase.wantErrorMsg != "" {
				if err == nil || err.Error() != testCase.wantErrorMsg {
					t.Fatalf("expected error %q, got %v", testCase.wantErrorMsg, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseUsersDeleteArgs returned error: %v", err)
			}
			if opts.email != testCase.wantEmail || opts.userID != testCase.wantID || opts.skipConfirm != testCase.wantSkip {
				t.Fatalf("unexpected parsed args: email=%q id=%d skip=%t", opts.email, opts.userID, opts.skipConfirm)
			}
		})
	}
}

func TestParseUsersSetEmailArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		args         []string
		wantID       uint
		wantEmail    string
		wantErrorMsg string
	}{
		{name: "id then email", args: []string{"--id", "7", "owner@example.com"}, wantID: 7, wantEmail: "owner@example.com"},
		{name: "blank argument between flags", args: []string{"", "--id", "7", "", "owner@example.com"}, wantID: 7, wantEmail: "owner@example.com"},
		{name: "equals form", args: []string{"--id=7", "owner@example.com"}, wantID: 7, wantEmail: "owner@example.com"},
		{name: "email then id", args: []string{"owner@example.com", "--id", "7"}, wantID: 7, wantEmail: "owner@example.com"},
		{name: "missing id", args: []string{"owner@example.com"}, wantErrorMsg: usersSetEmailUsage},
		{name: "missing email", args: []string{"--id", "7"}, wantErrorMsg: usersSetEmailUsage},
		{name: "two emails", args: []string{"--id", "7", "a@example.com", "b@example.com"}, wantErrorMsg: usersSetEmailUsage},
		{name: "two ids", args: []string{"--id", "7", "--id=8", "a@example.com"}, wantErrorMsg: usersSetEmailUsage},
		{name: "unknown flag", args: []string{"--id", "7", "a@example.com", "--yes"}, wantErrorMsg: usersSetEmailUsage},
		{name: "id not a number", args: []string{"--id", "x", "a@example.com"}, wantErrorMsg: `invalid account id "x" (see ovumcy users list)`},
	}

	for _, testCase := range tests {

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			opts, err := parseUsersSetEmailArgs(testCase.args)
			if testCase.wantErrorMsg != "" {
				if err == nil || err.Error() != testCase.wantErrorMsg {
					t.Fatalf("expected error %q, got %v", testCase.wantErrorMsg, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseUsersSetEmailArgs returned error: %v", err)
			}
			if opts.userID != testCase.wantID || opts.email != testCase.wantEmail {
				t.Fatalf("unexpected parsed args: id=%d email=%q", opts.userID, opts.email)
			}
		})
	}
}

// TestMapUsersSetEmailError pins the operator-facing wording of every refusal
// the repair can produce. Two of them the CLI's own parser makes unreachable —
// an absent id and an empty address — and they are mapped anyway: the service
// is the authority on its own preconditions, and a mapper that answers only
// the errors today's parser lets through turns a later parser change into an
// unreadable "set email: operator user ..." line.
func TestMapUsersSetEmailError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "missing id", err: services.ErrOperatorUserIDRequired, want: "an account id is required (see ovumcy users list)"},
		{name: "unknown id", err: services.ErrOperatorUserNotFound, want: "no account carries this id (see ovumcy users list)"},
		{name: "empty email", err: services.ErrOperatorUserEmailRequired, want: "email is required"},
		{name: "decorated email", err: services.ErrOperatorUserEmailInvalid, want: "invalid email address: pass the bare address, with no display name or angle brackets"},
		{name: "address taken", err: services.ErrOperatorUserEmailExists, want: "another account already answers to this email address"},
		{name: "row moved", err: services.ErrOperatorUserChangedUnderRepair, want: "this account's email changed while the repair ran — re-read ovumcy users list and retry"},
		{name: "storage failure", err: errors.New("db down"), want: "set email: db down"},
	}

	for _, testCase := range tests {

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := mapUsersSetEmailError(testCase.err); got == nil || got.Error() != testCase.want {
				t.Fatalf("expected %q, got %v", testCase.want, got)
			}
		})
	}
}

// TestRunUsersSetEmailParsesItsOwnArgumentsAndDefaultsItsWriter covers the two
// arms runUsersCommand hides: it validates the same arguments before opening
// the database, and it always supplies a writer.
func TestRunUsersSetEmailParsesItsOwnArgumentsAndDefaultsItsWriter(t *testing.T) {
	t.Parallel()

	// The parse runs before the service is touched, so a nil service here is
	// an assertion in itself: reaching past it would panic.
	if err := runUsersSetEmail(nil, []string{"--id", "7"}, &bytes.Buffer{}); err == nil || err.Error() != usersSetEmailUsage {
		t.Fatalf("expected the usage error, got %v", err)
	}

	databasePath := createCLIUsersDatabase(t)
	user := createCLIUsersUser(t, databasePath, "owner@example.com", "Owner", models.RoleOwner, true, time.Now().UTC())
	service := operatorUserServiceForCLITest(t, databasePath)

	// A nil writer means "the process's stdout", which is what the exported
	// entry point passes; the write itself is proven by the row it moved.
	if err := runUsersSetEmail(service, []string{"--id", strconv.FormatUint(uint64(user.ID), 10), "renamed@example.com"}, nil); err != nil {
		t.Fatalf("runUsersSetEmail with a nil writer returned error: %v", err)
	}
	if got := loadCLIUsersRow(t, databasePath, user.ID).Email; got != "renamed@example.com" {
		t.Fatalf("expected the repair to land, got %q", got)
	}
}

func operatorUserServiceForCLITest(t *testing.T, databasePath string) *services.OperatorUserService {
	t.Helper()

	database, err := db.OpenDatabase(db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := database.DB()
	if err != nil {
		t.Fatalf("open sql db: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	repositories := buildRepositories(database)
	return services.NewOperatorUserService(repositories.Users, services.NewAuthService(repositories.Users))
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
