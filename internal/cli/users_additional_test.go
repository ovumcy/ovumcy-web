package cli

import (
	"bytes"
	"io"
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
		{name: "missing subcommand", args: nil, want: "usage: ovumcy users <list|delete>"},
		{name: "unknown subcommand", args: []string{"export"}, want: "usage: ovumcy users <list|delete>"},
		{name: "list with extra arg", args: []string{"list", "extra"}, want: "usage: ovumcy users list"},
		{name: "delete without email", args: []string{"delete"}, want: "usage: ovumcy users delete <email> [--yes]"},
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

	return services.NewOperatorUserService(db.NewRepositories(database).Users)
}

func nowUTC() time.Time {
	return time.Now().UTC()
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
