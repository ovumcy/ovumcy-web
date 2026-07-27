package cli

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/internal/db"
)

// TestRunResetPasswordCommandValidatesBeforeReadingStdin covers the exported
// entry point, which is otherwise reached only from main. It also pins the
// ordering that makes the piped form usable in a script: the email is rejected
// before anything reads stdin, so a typo does not leave the command blocked on
// input that will never be used.
func TestRunResetPasswordCommandValidatesBeforeReadingStdin(t *testing.T) {
	if err := RunResetPasswordCommand(db.Config{}, "   "); err == nil || err.Error() != "email is required" {
		t.Fatalf("expected blank email error, got %v", err)
	}
}

// TestResetPasswordReaderReadsPipedStdin pins the non-interactive path that
// makes the documented recovery step runnable against the shell-free runtime
// image: `docker exec -i ... reset-password <email>` with the password on
// stdin. Before it existed the command answered "secure password prompt
// requires an interactive terminal" and recovering several accounts after a
// SECRET_KEY rotation could not be scripted at all.
func TestResetPasswordReaderReadsPipedStdin(t *testing.T) {
	t.Parallel()

	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	t.Cleanup(func() { _ = read.Close() })

	go func() {
		defer func() { _ = write.Close() }()
		_, _ = io.WriteString(write, "  StrongPass1  \n")
	}()

	password, err := resetPasswordReader(read)()
	if err != nil {
		t.Fatalf("resetPasswordReader on piped stdin: %v", err)
	}
	if string(password) != "StrongPass1" {
		t.Fatalf("password = %q, want %q (surrounding whitespace trimmed to match web auth)", password, "StrongPass1")
	}
}

// TestResetPasswordReaderRejectsMissingStdin covers the typed-nil guard: a nil
// *os.File satisfies io.Reader, so without the explicit check it would reach
// bufio instead of returning an error.
func TestResetPasswordReaderRejectsMissingStdin(t *testing.T) {
	t.Parallel()

	if _, err := resetPasswordReader(nil)(); err == nil {
		t.Fatal("expected an error when stdin is missing")
	}
}

func TestRunResetPasswordCommandRejectsBlankEmail(t *testing.T) {
	t.Parallel()

	err := runResetPasswordCommand(db.Config{}, "   ", nil, io.Discard)
	if err == nil || err.Error() != "email is required" {
		t.Fatalf("expected blank email error, got %v", err)
	}
}

func TestRunResetPasswordCommandRejectsInvalidEmail(t *testing.T) {
	t.Parallel()

	err := runResetPasswordCommand(db.Config{}, "not-an-email", nil, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "invalid email address") {
		t.Fatalf("expected invalid email error, got %v", err)
	}
}

func TestRunResetPasswordCommandRequiresPasswordPrompt(t *testing.T) {
	t.Parallel()

	databasePath := createCLIResetDatabase(t)
	createCLIResetUser(t, databasePath, "cli-reset-nil-prompt@example.com", "StrongPass1")

	err := runResetPasswordCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		"cli-reset-nil-prompt@example.com",
		nil,
		io.Discard,
	)
	if err == nil || err.Error() != "password prompt is required" {
		t.Fatalf("expected nil prompt error, got %v", err)
	}
}

func TestRunResetPasswordCommandRejectsEmptyPromptedPassword(t *testing.T) {
	t.Parallel()

	databasePath := createCLIResetDatabase(t)
	createCLIResetUser(t, databasePath, "cli-reset-empty-password@example.com", "StrongPass1")

	err := runResetPasswordCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		"cli-reset-empty-password@example.com",
		func() ([]byte, error) { return []byte{}, nil },
		io.Discard,
	)
	if err == nil || err.Error() != "password is required" {
		t.Fatalf("expected empty password error, got %v", err)
	}
}

func TestRunResetPasswordCommandRejectsWeakPassword(t *testing.T) {
	t.Parallel()

	databasePath := createCLIResetDatabase(t)
	createCLIResetUser(t, databasePath, "cli-reset-weak-password@example.com", "StrongPass1")

	err := runResetPasswordCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		"cli-reset-weak-password@example.com",
		func() ([]byte, error) { return []byte("weakpass"), nil },
		io.Discard,
	)
	if err == nil || err.Error() != "password does not meet strength requirements" {
		t.Fatalf("expected weak password error, got %v", err)
	}
}

func TestRunResetPasswordCommandReportsMissingUser(t *testing.T) {
	t.Parallel()

	databasePath := createCLIResetDatabase(t)

	err := runResetPasswordCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		"missing-reset-user@example.com",
		func() ([]byte, error) { return []byte("StrongPass2"), nil },
		io.Discard,
	)
	if err == nil || !strings.Contains(err.Error(), "user missing-reset-user@example.com not found") {
		t.Fatalf("expected missing user error, got %v", err)
	}
}

func TestRunResetPasswordCommandWrapsPromptReadFailure(t *testing.T) {
	t.Parallel()

	databasePath := createCLIResetDatabase(t)
	createCLIResetUser(t, databasePath, "cli-reset-read-failure@example.com", "StrongPass1")

	err := runResetPasswordCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: databasePath},
		"cli-reset-read-failure@example.com",
		func() ([]byte, error) { return nil, errors.New("terminal unavailable") },
		io.Discard,
	)
	if err == nil || !strings.Contains(err.Error(), "read new password") {
		t.Fatalf("expected wrapped prompt error, got %v", err)
	}
}

// TestRunResetPasswordCommandReportsDatabaseInitFailure covers the operator
// UX when the configured database cannot be opened (e.g. a bad path/config):
// the command must surface a wrapped "database init failed" error rather than
// panic or leak a raw driver error. A directory path is an unopenable SQLite
// target on every platform.
func TestRunResetPasswordCommandReportsDatabaseInitFailure(t *testing.T) {
	err := runResetPasswordCommand(
		db.Config{Driver: db.DriverSQLite, SQLitePath: t.TempDir()},
		"owner@example.com",
		func() ([]byte, error) { return []byte("StrongPass1"), nil },
		io.Discard,
	)
	if err == nil {
		t.Fatal("expected an error when the database cannot be opened")
	}
	if !strings.Contains(err.Error(), "database init failed") {
		t.Fatalf("expected a wrapped database-init error, got %v", err)
	}
}
